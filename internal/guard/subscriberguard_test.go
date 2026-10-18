package guard

import "testing"

func TestSubscriberServiceGrantIsNarrow(t *testing.T) {
	g := SubscriberAuthority{Identity: "service:subscriber", Mode: "pilot", RouterID: "synthetic-router", Network: "C", Disconnect: true}
	if !CheckSubscriberDisconnect(g, "synthetic-router", "C", "lease", "session") {
		t.Fatal("valid service grant")
	}
	for _, mode := range []string{"disabled", "shadow", ""} {
		v := g
		v.Mode = mode
		if CheckSubscriberDisconnect(v, "synthetic-router", "C", "lease", "session") {
			t.Fatal(mode)
		}
	}
	for _, target := range [][4]string{{"other", "C", "lease", "session"}, {"synthetic-router", "B", "lease", "session"}, {"synthetic-router", "C", "", "session"}, {"synthetic-router", "C", "lease", ""}} {
		if CheckSubscriberDisconnect(g, target[0], target[1], target[2], target[3]) {
			t.Fatal(target)
		}
	}
	g.Disconnect = false
	if CheckSubscriberDisconnect(g, "synthetic-router", "C", "lease", "session") {
		t.Fatal("missing write grant")
	}
}
