package nodenamerewriter

import (
	"testing"
)

func TestRewriteSequentialZones(t *testing.T) {
	r, err := New(`^(\w+)-.*$`, "zone{zone}-worker-{index}")
	if err != nil {
		t.Fatal(err)
	}
	r.zoneReplace = "zone{zone}"
	r.region = "eu-west-1"

	tests := []struct {
		host     string
		wantName string
		wantZone string
	}{
		{"zoneA-host-1", "zone1-worker-1", "zone1"},
		{"zoneA-host-2", "zone1-worker-2", "zone1"},
		{"zoneB-host-1", "zone2-worker-1", "zone2"},
		{"zoneC-host-1", "zone3-worker-1", "zone3"},
		{"zoneB-host-3", "zone2-worker-2", "zone2"},
	}

	for _, tt := range tests {
		name := r.Rewrite(tt.host)
		if name != tt.wantName {
			t.Errorf("Rewrite(%q) = %q, want %q", tt.host, name, tt.wantName)
		}
		zone := r.Zone(tt.host)
		if zone != tt.wantZone {
			t.Errorf("Zone(%q) = %q, want %q", tt.host, zone, tt.wantZone)
		}
	}

	if r.Region() != "eu-west-1" {
		t.Errorf("Region() = %q, want eu-west-1", r.Region())
	}
}

func TestRewriteIdempotent(t *testing.T) {
	r, _ := New(`^(\w+)-.*$`, "zone{zone}-worker-{index}")

	a := r.Rewrite("host-node-1")
	b := r.Rewrite("host-node-1")
	if a != b {
		t.Errorf("expected idempotent, got %q and %q", a, b)
	}
}

func TestRewriteNoMatch(t *testing.T) {
	r, _ := New(`^(\w+)-(\w+)-(\d+)$`, "node-${1}-${3}")

	name := r.Rewrite("singleword")
	if name != "singleword" {
		t.Errorf("expected passthrough for non-matching name, got %q", name)
	}
}

func TestRewriteDisabled(t *testing.T) {
	r := &Rewriter{}

	if r.Enabled() {
		t.Error("expected disabled")
	}
	if name := r.Rewrite("anything"); name != "anything" {
		t.Errorf("expected passthrough, got %q", name)
	}
}

func TestRewriteRegexCaptureGroups(t *testing.T) {
	r, _ := New(`^(\w+)-(\w+)-(\d+)$`, "node-${3}")

	name := r.Rewrite("zoneA-host-42")
	if name != "node-42" {
		t.Errorf("expected node-42, got %q", name)
	}
}

func TestHostNameReverseLookup(t *testing.T) {
	r, _ := New(`^(\w+)-.*$`, "zone{zone}-worker-{index}")

	r.Rewrite("host-node-1")
	host, ok := r.HostName("zone1-worker-1")
	if !ok || host != "host-node-1" {
		t.Errorf("expected host-node-1, got %q (ok=%v)", host, ok)
	}

	_, ok = r.HostName("unknown")
	if ok {
		t.Error("expected not found for unknown virtual name")
	}
}
