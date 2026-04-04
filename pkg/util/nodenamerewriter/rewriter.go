package nodenamerewriter

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Rewriter translates host node names to virtual node names using a regex
// match/replace pattern. The replacement template supports standard regex
// capture group references (${1}, ${2}) plus two special sequential placeholders:
//
//   - {zone}  sequential number for the zone (grouped by capture group 1)
//   - {index} sequential number within the zone
//
// When no match/replace is configured, names pass through unchanged.
type Rewriter struct {
	mu          sync.Mutex
	pattern     *regexp.Regexp
	replace     string
	zoneReplace string
	region      string
	hostToVirt  map[string]string
	virtToHost  map[string]string
	hostToZone  map[string]string
	zoneMap     map[string]int // capture group 1 value -> zone number
	zoneCounts  map[int]int    // zone number -> worker count
	nextZone    int
}

// DefaultFromEnv creates a Rewriter from environment variables:
//
//	VCLUSTER_NODE_NAME_MATCH   - regex pattern (required for rewriting)
//	VCLUSTER_NODE_NAME_REPLACE - replacement template for node name
//	VCLUSTER_NODE_ZONE_REPLACE - replacement template for zone label (e.g. "zone{zone}")
//	VCLUSTER_NODE_REGION       - region label value
//
// Returns a no-op Rewriter if VCLUSTER_NODE_NAME_MATCH is not set.
func DefaultFromEnv() *Rewriter {
	match := os.Getenv("VCLUSTER_NODE_NAME_MATCH")
	replace := os.Getenv("VCLUSTER_NODE_NAME_REPLACE")
	if match == "" {
		return &Rewriter{}
	}
	r, err := New(match, replace)
	if err != nil {
		return &Rewriter{}
	}
	r.zoneReplace = os.Getenv("VCLUSTER_NODE_ZONE_REPLACE")
	r.region = os.Getenv("VCLUSTER_NODE_REGION")
	return r
}

func New(pattern, replace string) (*Rewriter, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid node name pattern %q: %w", pattern, err)
	}
	return &Rewriter{
		pattern:    re,
		replace:    replace,
		hostToVirt: make(map[string]string),
		virtToHost: make(map[string]string),
		hostToZone: make(map[string]string),
		zoneMap:    make(map[string]int),
		zoneCounts: make(map[int]int),
		nextZone:   1,
	}, nil
}

// Enabled returns true if a rewrite pattern is configured.
func (r *Rewriter) Enabled() bool {
	return r != nil && r.pattern != nil
}

// Rewrite translates a host node name to its virtual name.
func (r *Rewriter) Rewrite(hostName string) string {
	if !r.Enabled() {
		return hostName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if virt, ok := r.hostToVirt[hostName]; ok {
		return virt
	}

	match := r.pattern.FindStringSubmatch(hostName)
	if match == nil {
		return hostName
	}

	zoneKey, zoneNum, indexNum := r.resolveCounters(match)
	virt := r.applyTemplate(hostName, r.replace, match, zoneNum, indexNum)

	// Compute zone label
	zoneLabel := zoneKey
	if r.zoneReplace != "" {
		zoneLabel = r.applyTemplate(hostName, r.zoneReplace, match, zoneNum, indexNum)
	}

	r.hostToVirt[hostName] = virt
	r.virtToHost[virt] = hostName
	r.hostToZone[hostName] = zoneLabel
	return virt
}

// Zone returns the rewritten zone label for a host node name.
// Must be called after Rewrite.
func (r *Rewriter) Zone(hostName string) string {
	if !r.Enabled() {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hostToZone[hostName]
}

// Region returns the configured region label value.
func (r *Rewriter) Region() string {
	if r == nil {
		return ""
	}
	return r.region
}

// HostName returns the real host name for a virtual name, if known.
func (r *Rewriter) HostName(virtualName string) (string, bool) {
	if !r.Enabled() {
		return virtualName, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	host, ok := r.virtToHost[virtualName]
	return host, ok
}

func (r *Rewriter) resolveCounters(match []string) (string, int, int) {
	zoneKey := ""
	if len(match) > 1 {
		zoneKey = match[1]
	}

	zoneNum, ok := r.zoneMap[zoneKey]
	if !ok {
		zoneNum = r.nextZone
		r.zoneMap[zoneKey] = zoneNum
		r.nextZone++
	}

	r.zoneCounts[zoneNum]++
	return zoneKey, zoneNum, r.zoneCounts[zoneNum]
}

func (r *Rewriter) applyTemplate(hostName, tmpl string, match []string, zoneNum, indexNum int) string {
	result := r.pattern.ReplaceAllString(hostName, tmpl)
	result = strings.ReplaceAll(result, "{zone}", fmt.Sprintf("%d", zoneNum))
	result = strings.ReplaceAll(result, "{index}", fmt.Sprintf("%d", indexNum))
	return result
}
