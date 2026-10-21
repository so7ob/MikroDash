package subscriber

import (
	"testing"
	"time"
)

func resetCases(t *testing.T) []struct {
	name   string
	period Period
	edge   time.Time
} {
	t.Helper()
	parse := func(raw string) time.Time {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	return []struct {
		name   string
		period Period
		edge   time.Time
	}{
		{"hourly-minute-25", Period{Kind: "hourly", Zone: "Asia/Aden", Anchor: parse("2024-01-01T00:25:00+03:00")}, parse("2024-03-01T12:25:00+03:00")},
		{"daily-midnight", Period{Kind: "daily", Zone: "Asia/Aden", Anchor: parse("2024-01-01T00:00:00+03:00")}, parse("2024-03-01T00:00:00+03:00")},
		{"weekly-sunday", Period{Kind: "weekly", Zone: "UTC", Anchor: parse("2024-01-07T00:00:00Z")}, parse("2024-01-14T00:00:00Z")},
		{"monthly-leap", Period{Kind: "monthly", Zone: "UTC", Anchor: parse("2024-01-31T00:00:00Z")}, parse("2024-02-29T00:00:00Z")},
		{"monthly-short", Period{Kind: "monthly", Zone: "UTC", Anchor: parse("2024-01-31T00:00:00Z")}, parse("2025-02-28T00:00:00Z")},
	}
}

func crossingFixture(t *testing.T, period Period, edge time.Time, configure ...func(*Plan)) (*Store, *RadiusService, Account, Admission, time.Time) {
	t.Helper()
	s, svc, account, _ := protocolFixture(t)
	plans, err := s.Plans(t.Context())
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	p := plans[0]
	p.Version++
	p.MaxSessions = 1
	p.Policies[0].Limits[0].Period = period
	p.Policies[0].Limits = append(p.Policies[0].Limits, Limit{ID: "download", Kind: "download", State: "limited", Amount: 64 << 20, Period: period, Action: "disconnect"})
	for _, change := range configure {
		change(&p)
	}
	start := edge.Add(-20 * time.Second)
	if err = s.SavePlan(t.Context(), "synthetic-admin", "calendar delegation fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", account.ID, p.ID, p.Version, "calendar delegation fixture", start); err != nil {
		t.Fatal(err)
	}
	grant, err := s.Reserve(t.Context(), account.ID, "lab", "C", "device", "calendar-admission", start)
	if err != nil {
		t.Fatal(err)
	}
	svc.Clients[0].Capabilities = recordedCapabilities()
	return s, svc, account, grant, start
}

func TestAdmissionCrossesQuotaResetWithIndependentReservations(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, _, account, grant, start := crossingFixture(t, c.period, c.edge)
			if grant.Decision.Timeout != 60 {
				t.Fatalf("eligible initial delegation ends at quota reset: got %d seconds, want 60", grant.Decision.Timeout)
			}
			old, end, err := c.period.Bounds(start)
			if err != nil || !end.Equal(c.edge) {
				t.Fatal("invalid independent fixture edge", old, end, err)
			}
			for _, expected := range []struct {
				limit  string
				period time.Time
				amount Quantity
			}{{"time", old, 25}, {"time", c.edge, 60}, {"download", old, grant.Download}, {"download", c.edge, grant.Download}} {
				var amount Quantity
				if err = s.sql.QueryRow(`SELECT amount FROM subscriber_reservations WHERE lease_id=? AND limit_id=? AND period_start=?`, grant.LeaseID, expected.limit, expected.period.Unix()).Scan(&amount); err != nil || amount != expected.amount {
					t.Fatal("missing or incorrect independent period reservation", expected, amount, err)
				}
			}
			view, err := s.Snapshot(t.Context(), account.ID, start)
			if err != nil || view.LifetimeSeconds != 0 || view.LifetimeDownload != 0 || view.PendingAccounting != 0 {
				t.Fatal("reserving future entitlement changed confirmed usage", view, err)
			}
		})
	}
}

func TestRenewalCrossesQuotaResetWithoutRegrantingConsumedTime(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, svc, account, grant, start := crossingFixture(t, c.period, c.edge)
			if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: start}); err != nil {
				t.Fatal(err)
			}
			now := start.Add(5 * time.Second)
			if err := s.ObserveRouter(t.Context(), observation(svc, account, grant, start, now), svc.Clients); err != nil {
				t.Fatal(err)
			}
			op, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
			if err != nil {
				t.Fatal(err)
			}
			if !op.TargetDeadline.Equal(now.Add(60*time.Second)) || !op.TargetDeadline.After(c.edge) {
				t.Fatal("eligible renewal still expires at quota reset", op.TargetDeadline, c.edge)
			}
			view, err := s.Snapshot(t.Context(), account.ID, now)
			if err != nil || view.LifetimeSeconds != 5 || view.LifetimeDownload != 5000 {
				t.Fatal("renewal reset or duplicated confirmed consumption", view, err)
			}
		})
	}
}
