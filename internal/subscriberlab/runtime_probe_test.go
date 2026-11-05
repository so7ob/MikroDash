package subscriberlab

import (
	"maps"
	"testing"

	"mikrodash/internal/routeros"
)

func TestRuntimePayloadRequiresExactOwnedClient(t *testing.T) {
	row := routeros.Reply{".id": "*synthetic", "user": "chr-lab", "server": "hs-lab", "mac-address": "02:00:00:00:54:02", "address": "198.51.100.2"}
	if _, err := ownedRuntimeClient([]routeros.Reply{row}); err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][]routeros.Reply{nil, {}, {row, row}} {
		if _, err := ownedRuntimeClient(rows); err == nil {
			t.Fatal("missing or ambiguous runtime client accepted")
		}
	}
	for key := range row {
		bad := maps.Clone(row)
		bad[key] = "unexpected"
		if key == ".id" {
			bad[key] = ""
		}
		if _, err := ownedRuntimeClient([]routeros.Reply{bad}); err == nil {
			t.Fatalf("different client accepted through %s", key)
		}
	}
}

func TestSelectionProbeRequiresSoleExpectedPrimary(t *testing.T) {
	row := routeros.Reply{".id": "*synthetic", "comment": "md-lab-radius", "disabled": "false", "authentication-port": "12812", "accounting-port": "12813", "address": "10.0.2.2", "service": "hotspot"}
	if id, err := selectionPrimary([]routeros.Reply{row}); err != nil || id != row[".id"] {
		t.Fatal("owned primary refused", id, err)
	}
	for _, rows := range [][]routeros.Reply{nil, {row, row}} {
		if _, err := selectionPrimary(rows); err == nil {
			t.Fatal("missing or competing primary accepted")
		}
	}
	for key := range row {
		bad := maps.Clone(row)
		bad[key] = "unexpected"
		if key == ".id" {
			bad[key] = ""
		}
		if _, err := selectionPrimary([]routeros.Reply{bad}); err == nil {
			t.Fatalf("unexpected primary field accepted: %s", key)
		}
	}
}
