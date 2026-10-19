package subscriber_test

import (
	. "mikrodash/internal/subscriber"
	"testing"
	"time"
)

func TestByteGapSettlementIsExplicitConservingAndIdempotent(t *testing.T) {
	s, a := setup(t, plan())
	start := at("2026-10-31T23:59:50+03:00")
	grant := reserve(t, s, a, "device", start)
	if err := s.Account(ctx, event(grant, "device", "stop", 20, 100, 0, start.Add(20*time.Second))); err != nil {
		t.Fatal(err)
	}
	gaps, err := s.ByteGaps(ctx, a.ID)
	if err != nil || len(gaps) != 2 {
		t.Fatal(gaps, err)
	}
	before := snapshot(t, s, a, start.Add(20*time.Second))
	if before.LifetimeDownload != 100 {
		t.Fatal(before)
	}
	for _, gap := range gaps {
		if len(gap.Periods) != 2 || gap.Amount != 100 || !gap.ResolvedAt.IsZero() {
			t.Fatal(gap)
		}
		wrong := []GapAllocation{{PeriodStart: gap.Periods[0], Amount: 99}}
		if err = s.ResolveByteGap(ctx, "admin", gap.ID, "approved fixture distribution", "synthetic test evidence", wrong, start.Add(time.Minute)); err == nil {
			t.Fatal("partial allocation accepted")
		}
		allocation := []GapAllocation{{PeriodStart: gap.Periods[0], Amount: 40}, {PeriodStart: gap.Periods[1], Amount: 60}}
		if err = s.ResolveByteGap(ctx, "", gap.ID, "approved fixture distribution", "synthetic test evidence", allocation, start.Add(time.Minute)); err == nil {
			t.Fatal("anonymous approval accepted")
		}
		for range 2 {
			if err = s.ResolveByteGap(ctx, "admin", gap.ID, "approved fixture distribution", "synthetic test evidence", allocation, start.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	old := snapshot(t, s, a, start)
	current := snapshot(t, s, a, start.Add(20*time.Second))
	for _, v := range old.Decision.Limits {
		if (v.Limit.Kind == "download" || v.Limit.Kind == "total") && v.Used != 40 {
			t.Fatal("old period settlement", v)
		}
	}
	for _, v := range current.Decision.Limits {
		if (v.Limit.Kind == "download" || v.Limit.Kind == "total") && (v.Used != 60 || v.Provisional != 0) {
			t.Fatal("new period settlement", v)
		}
	}
	if current.LifetimeDownload != 100 || current.LifetimeSeconds != 20 || current.Subscription.ID != before.Subscription.ID {
		t.Fatal("settlement changed lifetime or subscription", current)
	}
}
func TestRemainingSaturatesInsteadOfWrappingAtInt64Boundary(t *testing.T) {
	p := plan()
	p.Policies[0].Limits[0].Amount = 9223372036854775807
	now := at("2026-10-01T10:00:00+03:00")
	start, _, err := p.Policies[0].Limits[0].Period.Bounds(now)
	if err != nil {
		t.Fatal(err)
	}
	d := Evaluate(p, "economy", now, now.Add(time.Hour), map[string]Usage{Key("time", start): {Used: 9223372036854775807, Reserved: 9223372036854775807}})
	if d.Allowed || d.Limits[0].Remaining != 0 {
		t.Fatal("signed overflow invented credit", d)
	}
}
