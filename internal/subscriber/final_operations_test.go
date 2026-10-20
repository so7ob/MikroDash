package subscriber

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestMissingFinalAccountingStopsNewAndQueuedRenewalForOtherDevice(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "queued"}[queued], func(t *testing.T) {
			s, svc, account, _, start := liveFixture(t)
			secondAt := start.Add(30 * time.Second)
			second, err := s.Reserve(t.Context(), account.ID, "lab", "C", "device-two", "second-device", secondAt)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Account(t.Context(), Accounting{LeaseID: second.LeaseID, RouterID: "lab", Network: "C", Device: "device-two", SessionID: "session-two", Status: "start", At: secondAt}); err != nil {
				t.Fatal(err)
			}
			observe := func(at time.Time) {
				view := observation(svc, account, second, secondAt, at)
				view.Sessions[0].Device = "device-two"
				if err := s.ObserveRouter(t.Context(), view, svc.Clients); err != nil {
					t.Fatal(err)
				}
			}
			if queued {
				// The first delegation has not expired yet. This operation is
				// legitimately reserved before any missing-final proof exists.
				observe(start.Add(50 * time.Second))
				if _, err = s.PrepareOperation(t.Context(), second.LeaseID, "renew", start.Add(50*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			observe(start.Add(61 * time.Second))
			if !queued {
				if _, err = s.PrepareOperation(t.Context(), second.LeaseID, "renew", start.Add(62*time.Second)); !errors.Is(err, ErrDenied) {
					t.Fatal("another device renewed unknown shared credit", err)
				}
				return
			}
			conn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			svc.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
			if err = svc.ProcessOperations(t.Context(), start.Add(61*time.Second)); err != nil {
				t.Fatal(err)
			}
			operations, err := s.Operations(t.Context(), false)
			if err != nil || len(operations) != 1 || operations[0].State != "failed" || operations[0].Attempts != 0 {
				t.Fatal("queued grant was issued with unknown shared final accounting", operations, err)
			}
		})
	}
}
