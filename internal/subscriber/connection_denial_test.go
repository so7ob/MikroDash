package subscriber

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"layeh.com/radius"
)

func TestTicketConnectionRetainsAuthoritativePolicyDenialAndRetry(t *testing.T) {
	for _, tc := range []struct{ name, reason string }{
		{"exhausted", "exhausted:time"},
		{"unknown", "unknown:time"},
		{"network", "network-not-entitled"},
		{"session-limit", "session-limit"},
		{"duplicate-device", "device-already-connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectionTestFixture(t)
			now := time.Now().Truncate(time.Second)
			snap, err := f.store.Snapshot(t.Context(), f.account.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			p := snap.Subscription.Plan
			p.Version++
			switch tc.name {
			case "exhausted":
				p.Policies[0].Limits[0].Amount = 0
			case "unknown":
				p.Policies[0].Limits[0].State = "unknown"
			case "network":
				p.Networks = []string{"D"}
			}
			if err = f.store.SavePlan(t.Context(), "synthetic-admin", "denial fixture", p); err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.Assign(t.Context(), "synthetic-admin", f.account.ID, p.ID, p.Version, "denial fixture", now); err != nil {
				t.Fatal(err)
			}
			if tc.name == "session-limit" || tc.name == "duplicate-device" {
				devices := []string{f.input.Device}
				if tc.name == "session-limit" {
					devices = []string{"02:00:00:00:00:70", "02:00:00:00:00:71"}
				}
				for _, device := range devices {
					if _, err = f.store.Reserve(t.Context(), f.account.ID, "lab", "C", device, "synthetic-blocking-"+device, now); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := f.store.Snapshot(t.Context(), f.account.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			packet := access(f.token)
			for range 2 {
				if reply := exchange(t, packet, f.address); reply.Code != radius.CodeAccessReject {
					t.Fatal("blocked ticket was accepted", reply.Code)
				}
				status := f.status(t, time.Now())
				if status.Stage != "rejected" || !slices.Contains(status.Reasons, tc.reason) {
					t.Fatal("owned flow lost the authoritative admission reason", status)
				}
			}
			after, err := f.store.Snapshot(t.Context(), f.account.ID, now)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected request changed balances, expiry, subscription or reservations", err)
			}
			var audits int
			if err = f.store.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='authentication.ticket.rejected' AND target=?`, Digest(f.token)).Scan(&audits); err != nil || audits != 1 {
				t.Fatal("duplicate/missing denial audit", audits, err)
			}
			if tc.name == "exhausted" {
				p.Version++
				p.Policies[0].Limits[0].Amount = 3600
				if err = f.store.SavePlan(t.Context(), "synthetic-admin", "restore entitlement fixture", p); err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.Assign(t.Context(), "synthetic-admin", f.account.ID, p.ID, p.Version, "restore entitlement fixture", now); err != nil {
					t.Fatal(err)
				}
				if reply := exchange(t, packet, f.address); reply.Code != radius.CodeAccessReject {
					t.Fatal("a rejected ticket became reusable after renewal", reply.Code)
				}
				newToken, err := f.store.IssueTicket(t.Context(), f.input, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if reply := exchange(t, access(newToken), f.address); reply.Code != radius.CodeAccessAccept {
					t.Fatal("new entitled attempt did not recover", reply.Code)
				}
			}
		})
	}
}
