package patches

import (
	"testing"

	"gotest.tools/assert"
)

func sampleObject() map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"name":      "demo",
			"namespace": "team-a",
			"labels":    map[string]any{"app": "demo"},
		},
		"spec": map[string]any{
			"dnsNames": []any{"www.example.com", "example.com"},
			"containers": []any{
				map[string]any{"name": "app", "env": []any{map[string]any{"name": "FOO", "value": "1"}, map[string]any{"name": "BAR", "value": "2"}}},
				map[string]any{"name": "sidecar"},
			},
		},
	}
}

func values(ms []match) []any {
	var out []any
	for _, m := range ms {
		out = append(out, m.value)
	}
	return out
}

func TestFind(t *testing.T) {
	obj := sampleObject()
	cases := []struct {
		path string
		want []any
	}{
		{"metadata.name", []any{"demo"}},
		{"$.metadata.name", []any{"demo"}},
		{`metadata.labels["app"]`, []any{"demo"}},
		{"spec.dnsNames[*]", []any{"www.example.com", "example.com"}},
		{"spec.dnsNames[1]", []any{"example.com"}},
		{"spec.containers[?(@.name=='app')].env[?(@.name == \"FOO\")].value", []any{"1"}},
		{"spec.containers[?(@.name!='app')].name", []any{"sidecar"}},
		{"spec.containers[?(@.env)].name", []any{"app"}},
		{"spec.missing.deeper", nil},
		{"spec.dnsNames[5]", nil},
	}
	for _, c := range cases {
		segments, err := parsePath(c.path)
		assert.NilError(t, err, c.path)
		assert.DeepEqual(t, values(find(obj, segments)), c.want)
	}
}

func TestFindMissingLeafIsAddable(t *testing.T) {
	obj := sampleObject()
	segments, err := parsePath("metadata.annotations")
	assert.NilError(t, err)
	matches := find(obj, segments)
	assert.Equal(t, len(matches), 1)
	assert.Assert(t, matches[0].value == nil)
	matches[0].set(map[string]any{"k": "v"})
	assert.DeepEqual(t, obj["metadata"].(map[string]any)["annotations"], map[string]any{"k": "v"})

	matches[0].remove()
	_, exists := obj["metadata"].(map[string]any)["annotations"]
	assert.Assert(t, !exists)
}

func TestParsePathErrors(t *testing.T) {
	for _, p := range []string{"", "spec[", "spec[?(name=='x')]", "spec[foo]"} {
		_, err := parsePath(p)
		assert.Assert(t, err != nil, p)
	}
}
