// Package patches implements sync.toHost.*.patches / sync.fromHost.*.patches (translate patches), which upstream
// ships only in vCluster Pro. It plugs into pro.ApplyPatchesHostObject / pro.ApplyPatchesVirtualObject from init().
package patches

import (
	"fmt"
	"strconv"
	"strings"
)

type segmentKind int

const (
	segField segmentKind = iota
	segIndex
	segWildcard
	segFilter
)

type segment struct {
	kind  segmentKind
	field string
	index int

	// filter: [?(@.<filterPath> <op> <value>)]; op is "" when only existence is tested
	filterPath  []string
	filterOp    string
	filterValue any
}

// match is one location a path resolved to. parent is the map or slice holding the value; for a map the key is
// used, for a slice the index. value is nil when the final field does not exist yet in an existing parent map.
type match struct {
	parent any
	key    string
	index  int
	value  any
}

func (m match) set(v any) {
	switch p := m.parent.(type) {
	case map[string]any:
		p[m.key] = v
	case []any:
		p[m.index] = v
	}
}

func (m match) remove() {
	if p, ok := m.parent.(map[string]any); ok {
		delete(p, m.key)
	}
}

// parsePath accepts the JSONPath subset used by vCluster patches: dotted fields, quoted fields in brackets
// (["a/b"]), numeric indexes ([0]), wildcards ([*]) and filters ([?(@.name=='x')]). A leading "$" or "." is optional.
func parsePath(path string) ([]segment, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	var segments []segment
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := matchingBracket(path, i)
			if end < 0 {
				return nil, fmt.Errorf("unclosed bracket in path %q", path)
			}
			seg, err := parseBracket(path[i+1 : end])
			if err != nil {
				return nil, fmt.Errorf("path %q: %w", path, err)
			}
			segments = append(segments, seg)
			i = end + 1
		default:
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			segments = append(segments, segment{kind: segField, field: path[start:i]})
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("empty path %q", path)
	}
	return segments, nil
}

func matchingBracket(s string, open int) int {
	depth := 0
	quote := byte(0)
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseBracket(inner string) (segment, error) {
	inner = strings.TrimSpace(inner)
	switch {
	case inner == "*":
		return segment{kind: segWildcard}, nil
	case strings.HasPrefix(inner, "?(") && strings.HasSuffix(inner, ")"):
		return parseFilter(strings.TrimSpace(inner[2 : len(inner)-1]))
	case len(inner) >= 2 && (inner[0] == '\'' || inner[0] == '"') && inner[len(inner)-1] == inner[0]:
		return segment{kind: segField, field: inner[1 : len(inner)-1]}, nil
	}
	n, err := strconv.Atoi(inner)
	if err != nil {
		return segment{}, fmt.Errorf("unsupported bracket expression [%s]", inner)
	}
	return segment{kind: segIndex, index: n}, nil
}

func parseFilter(expr string) (segment, error) {
	if !strings.HasPrefix(expr, "@") {
		return segment{}, fmt.Errorf("filter %q must start with @", expr)
	}
	seg := segment{kind: segFilter}
	rest := expr[1:]
	for _, op := range []string{"==", "!="} {
		if idx := strings.Index(rest, op); idx >= 0 {
			seg.filterOp = op
			seg.filterValue = parseLiteral(strings.TrimSpace(rest[idx+len(op):]))
			rest = strings.TrimSpace(rest[:idx])
			break
		}
	}
	rest = strings.TrimPrefix(rest, ".")
	if rest == "" {
		return segment{}, fmt.Errorf("filter %q has no field", expr)
	}
	seg.filterPath = strings.Split(rest, ".")
	return seg, nil
}

func parseLiteral(s string) any {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// find resolves the segments against root. A trailing field that does not exist in an existing map yields a match
// with a nil value so a patch can add the field; every other miss yields no match.
func find(root any, segments []segment) []match {
	return findFrom(match{value: root}, segments)
}

func findFrom(current match, segments []segment) []match {
	if len(segments) == 0 {
		return []match{current}
	}
	seg, rest := segments[0], segments[1:]
	switch seg.kind {
	case segField:
		m, ok := current.value.(map[string]any)
		if !ok {
			return nil
		}
		v, exists := m[seg.field]
		if !exists && len(rest) > 0 {
			return nil
		}
		return findFrom(match{parent: m, key: seg.field, value: v}, rest)
	case segIndex:
		s, ok := current.value.([]any)
		if !ok || seg.index < 0 || seg.index >= len(s) {
			return nil
		}
		return findFrom(match{parent: s, index: seg.index, value: s[seg.index]}, rest)
	case segWildcard:
		var out []match
		switch c := current.value.(type) {
		case []any:
			for i := range c {
				out = append(out, findFrom(match{parent: c, index: i, value: c[i]}, rest)...)
			}
		case map[string]any:
			for k := range c {
				out = append(out, findFrom(match{parent: c, key: k, value: c[k]}, rest)...)
			}
		}
		return out
	case segFilter:
		s, ok := current.value.([]any)
		if !ok {
			return nil
		}
		var out []match
		for i := range s {
			if filterMatches(s[i], seg) {
				out = append(out, findFrom(match{parent: s, index: i, value: s[i]}, rest)...)
			}
		}
		return out
	}
	return nil
}

func filterMatches(item any, seg segment) bool {
	v, ok := lookup(item, seg.filterPath)
	if seg.filterOp == "" {
		return ok && v != nil
	}
	equal := ok && literalEqual(v, seg.filterValue)
	if seg.filterOp == "!=" {
		return !equal
	}
	return equal
}

func literalEqual(a, b any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// lookup follows a plain dotted field path inside item.
func lookup(item any, fields []string) (any, bool) {
	cur := item
	for _, f := range fields {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[f]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// relative resolves a relative path (e.g. "name" or "secretRef.name") from a matched value, or returns the match
// itself when the relative path is empty.
func relative(m match, relPath string) (match, bool) {
	if relPath == "" {
		return m, true
	}
	segments, err := parsePath(relPath)
	if err != nil {
		return match{}, false
	}
	matches := findFrom(m, segments)
	if len(matches) != 1 || matches[0].value == nil {
		return match{}, false
	}
	return matches[0], true
}
