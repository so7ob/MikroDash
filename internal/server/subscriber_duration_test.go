package server

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"testing"
	"time"

	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
)

func durationObserverFixture(t *testing.T, active routeros.Reply) (*Server, subscriber.Account) {
	t.Helper()
	s, account, _, _ := portalFixture(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := hub.New()
	scripted := session.NewForTestWithExec(h, "synthetic-router", func(cmd routeros.Cmd) ([]routeros.Reply, error) {
		switch cmd.Path {
		case "/system/resource/print":
			return []routeros.Reply{{"version": "synthetic-lab"}}, nil
		case "/ip/hotspot/active/print":
			return []routeros.Reply{active}, nil
		case "/queue/simple/print":
			return []routeros.Reply{}, nil
		default:
			t.Errorf("unexpected observer command %s", cmd.Path)
			return nil, nil
		}
	})
	s.sessions = session.NewManagerForTest(st, h, scripted)
	s.subscribers.cfg.Clients = []subscriber.NAS{{RouterID: "synthetic-router", Network: "C", CalledStationID: "hs-lab", Capabilities: subscriber.Capabilities{Version: "synthetic-lab", Evidence: "synthetic duration regression", CounterMode: "input-upload"}}}
	return s, account
}
func durationActive(uptime, left string) routeros.Reply {
	return routeros.Reply{".id": "*1", "server": "hs-lab", "user": "synthetic-account", "mac-address": "02:00:00:00:00:62", "address": "198.51.100.62", "uptime": uptime, "session-time-left": left, "bytes-in": "20", "bytes-out": "100", "limit-bytes-in": "1000", "limit-bytes-out": "1000"}
}
func TestSubscriberObserverRejectsMalformedUptimeBeforeCompleteSnapshot(t *testing.T) {
	for _, raw := range []string{"garbage", "1s-junk", "1m2m", "-1s", "01:99:00", "9223372036854775808s", "3000000000000h"} {
		t.Run(raw, func(t *testing.T) {
			s, _ := durationObserverFixture(t, durationActive(raw, "20s"))
			view, err := s.subscriberObserveRouter(t.Context(), "synthetic-router")
			if err == nil || view.Complete {
				t.Fatal("invalid uptime became a complete trusted snapshot", raw, view, err)
			}
		})
	}
}
func TestSubscriberObserverMalformedTimeoutIsUnknownWithoutLosingCounters(t *testing.T) {
	for _, raw := range []string{"", "garbage", "60s-junk", "1m2m", "-1s", "00:70:00", "9223372036854775808s"} {
		t.Run(raw, func(t *testing.T) {
			s, _ := durationObserverFixture(t, durationActive("20s", raw))
			view, err := s.subscriberObserveRouter(t.Context(), "synthetic-router")
			if err != nil || !view.Complete || len(view.Sessions) != 1 {
				t.Fatal("valid presence/counters lost", view, err)
			}
			row := view.Sessions[0]
			if row.TimeoutKnown || !row.CountersKnown || row.Seconds != 20 || row.Upload != 20 || row.Download != 100 {
				t.Fatal("unknown timeout became zero or erased reliable counters", raw, row)
			}
		})
	}
}
func TestSubscriberObserverKeepsLastTrustedLedgerAfterMalformedUptime(t *testing.T) {
	row := durationActive("70s", "0s")
	s, account := durationObserverFixture(t, row)
	ledger := s.subscribers.radius.Store
	start := time.Now().Add(-70 * time.Second).Truncate(time.Second)
	grant, err := ledger.Reserve(t.Context(), account.ID, "synthetic-router", "C", row["mac-address"], "synthetic-duration-lease", start)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.Account(t.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: row["mac-address"], SessionID: "synthetic-session", Status: "start", At: start}); err != nil {
		t.Fatal(err)
	}
	good, err := s.subscriberObserveRouter(t.Context(), "synthetic-router")
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.ObserveRouter(t.Context(), good, s.subscribers.cfg.Clients); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	before, err := ledger.Snapshot(t.Context(), account.ID, at)
	if err != nil || len(before.Sessions) != 1 || before.Sessions[0].State != "active" || before.LifetimeSeconds != 70 {
		t.Fatal("trusted fixture not reached", before, err)
	}
	row["uptime"] = "garbage"
	broken, readErr := s.subscriberObserveRouter(t.Context(), "synthetic-router")
	if readErr == nil {
		if err = ledger.ObserveRouter(t.Context(), broken, s.subscribers.cfg.Clients); err != nil {
			t.Fatal(err)
		}
	}
	after, err := ledger.Snapshot(t.Context(), account.ID, at)
	if readErr == nil || broken.Complete || err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("malformed uptime fabricated absence or changed trusted ledger", readErr, after, err)
	}
	if err = ledger.ObserveRouter(t.Context(), broken, s.subscribers.cfg.Clients); err == nil {
		t.Fatal("incomplete rejected observation accepted by ledger")
	}
}
func TestSubscriberObserverExplicitZeroDurationRemainsKnown(t *testing.T) {
	s, _ := durationObserverFixture(t, durationActive("0s", "0s"))
	view, err := s.subscriberObserveRouter(t.Context(), "synthetic-router")
	if err != nil || len(view.Sessions) != 1 || !view.Sessions[0].TimeoutKnown || view.Sessions[0].Seconds != 0 || view.Sessions[0].TimeLeft != 0 {
		t.Fatal("explicit zero became unknown", view, err)
	}
}

func TestSubscriberDurationSpellingsAndBounds(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		seconds subscriber.Quantity
		known   bool
	}{
		{"0", 0, true}, {"0s", 0, true}, {"0ms", 0, true}, {"90", 90, true},
		{"01:02:03", 3723, true}, {"1:2:3", 3723, true}, {"1d01:00:00", 90000, true},
		{"1w2d3h4m5s6ms", 788645, true}, {"1w2d03:04:05.987654321", 788645, true},
		{"24s930ms", 24, true}, {"1000ms", 1, true}, {"1s2000ms", 3, true},
		{"3650d", 315360000, true}, {"3650d1s", 0, false}, {"315360001", 0, false},
		{"", 0, false}, {"infinity", 0, false}, {" 1s", 0, false}, {"1s ", 0, false},
		{"1h2h", 0, false}, {"1m1h", 0, false}, {"1.5m", 0, false}, {"1s+junk", 0, false},
		{"00:60:00", 0, false}, {"00:00:60", 0, false}, {"1:001:1", 0, false},
		{"1w2d03:04:05.1234567890", 0, false}, {"18446744073709551616ms", 0, false},
		{"9223372036854775808s", 0, false}, {"4294967296s", 0, false},
	} {
		got, known := subscriberDuration(tc.raw)
		if got != tc.seconds || known != tc.known {
			t.Errorf("%q: got %d/%v, want %d/%v", tc.raw, got, known, tc.seconds, tc.known)
		}
	}
}

func FuzzSubscriberDurationCheckedArithmetic(f *testing.F) {
	f.Add(uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0))
	f.Add(uint64(1), uint64(2), uint64(3), uint64(4), uint64(5), uint64(6))
	f.Add(^uint64(0), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0))
	f.Fuzz(func(t *testing.T, weeks, days, hours, minutes, seconds, milliseconds uint64) {
		raw := fmt.Sprintf("%dw%dd%dh%dm%ds%dms", weeks, days, hours, minutes, seconds, milliseconds)
		expected := new(big.Int)
		for i, n := range []uint64{weeks, days, hours, minutes, seconds, milliseconds / 1000} {
			term := new(big.Int).SetUint64(n)
			term.Mul(term, new(big.Int).SetUint64([]uint64{604800, 86400, 3600, 60, 1, 1}[i]))
			expected.Add(expected, term)
		}
		got, known := subscriberDuration(raw)
		wantKnown := expected.Cmp(big.NewInt(315360000)) <= 0
		if known != wantKnown || (known && int64(got) != expected.Int64()) {
			t.Fatal("duration arithmetic wrapped or lost precision", raw, got, known, expected)
		}
		if known {
			roundtrip, ok := subscriberDuration(strconv.FormatInt(int64(got), 10))
			if !ok || roundtrip != got {
				t.Fatal("known integral seconds failed decimal representation")
			}
		}
	})
}
