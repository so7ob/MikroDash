package subscriberlab

import (
	"maps"
	"testing"

	"mikrodash/internal/routeros"
)

func TestTimeoutProofRejectsMalformedAndRelativeReadback(t *testing.T) {
	for _, tc := range []struct {
		uptime, left string
		valid        bool
	}{
		{"9s", "1m21s", true},
		{"10s", "1m19s", true}, // one-second non-atomic readback tolerance
		{"9s", "1m30s", false}, // a remaining-mode NAS cannot prove total mode
		{"9s", "1m19s", false},
		{"garbage", "1m30s", false},
		{"", "1m30s", false},
		{"0s", "", false},
		{"-1s", "1m31s", false},
		{"0.5s", "89.5s", false},
	} {
		err := verifyTotalTimeout(routeros.Reply{"uptime": tc.uptime, "session-time-left": tc.left}, 90)
		if (err == nil) != tc.valid {
			t.Fatalf("%q + %q proof=%v: %v", tc.uptime, tc.left, tc.valid, err)
		}
	}
}

func TestByteProofAcceptsMonotonicSnapshotSkewAndRefusesReset(t *testing.T) {
	before := routeros.Reply{".id": "*synthetic", "bytes-in": "42107", "bytes-out": "1054227"}
	after := routeros.Reply{".id": "*synthetic", "bytes-in": "42107", "bytes-out": "1054267", "limit-bytes-in": "107643", "limit-bytes-out": "1119763"}
	if err := verifyByteReadback(before, after, 1119763, 107643); err != nil {
		t.Fatal("legitimate in-flight acknowledgement invalidated cumulative counters", err)
	}
	for _, mutation := range []struct{ key, value string }{
		{".id", "*replacement"}, {".id", ""},
		{"bytes-in", "0"}, {"bytes-out", "0"},
		{"bytes-in", "unknown"}, {"bytes-out", ""},
		{"limit-bytes-in", "107644"}, {"limit-bytes-out", "1119764"},
	} {
		bad := maps.Clone(after)
		bad[mutation.key] = mutation.value
		if err := verifyByteReadback(before, bad, 1119763, 107643); err == nil {
			t.Fatalf("unproven byte semantics accepted: %s=%s", mutation.key, mutation.value)
		}
	}
}
