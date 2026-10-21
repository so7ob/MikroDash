package subscriber

import (
	"testing"
	"time"
)

// The accounting ingress accepts a NAS start from five seconds before reserve
// through sixty seconds after it. Every accepted start must have quota coverage,
// including a one-second delay over a reset; exact relative timeout is not an
// absolute wall-clock cutoff.
func TestAdmissionReservationsCoverAcceptedStartTiming(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, _, _, grant, start := crossingFixture(t, c.period, c.edge)
			for delay := int64(-5); delay <= 60; delay++ {
				required := map[int64]int64{}
				for second := int64(0); second < grant.Decision.Timeout; second++ {
					point := start.Add(time.Duration(delay+second) * time.Second)
					period, _, err := c.period.Bounds(point)
					if err != nil {
						t.Fatal(err)
					}
					required[period.Unix()]++
				}
				for period, seconds := range required {
					var reserved int64
					err := s.sql.QueryRow(`SELECT amount FROM subscriber_reservations WHERE lease_id=? AND limit_id='time' AND period_start=?`, grant.LeaseID, period).Scan(&reserved)
					if err != nil || reserved < seconds {
						t.Fatalf("accepted NAS start is under-reserved: delay=%d period=%d reserved=%d required=%d err=%v", delay, period, reserved, seconds, err)
					}
				}
			}
		})
	}
}

func TestByteReservationIncludesMeasurementAtResetEndpoint(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, _, account, _, start := crossingFixture(t, c.period, c.edge)
			view, err := s.Snapshot(t.Context(), account.ID, start)
			if err != nil {
				t.Fatal(err)
			}
			for _, initial := range []bool{true, false} {
				point := c.edge.Add(-60 * time.Second)
				if initial {
					point = point.Add(-60 * time.Second)
				}
				var grant Admission
				var reservations []periodReservation
				if initial {
					grant, reservations, _, err = planAdmission(view.Subscription.Plan, view.Subscription.SpeedID, point, c.edge.Add(time.Hour), map[string]Usage{}, 1)
				} else {
					grant, reservations, _, err = planDelegation(view.Subscription.Plan, view.Subscription.SpeedID, point, point, c.edge.Add(time.Hour), map[string]Usage{}, 1)
				}
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, reservation := range reservations {
					if reservation.LimitID == "download" && reservation.Start.Equal(c.edge) && reservation.Amount >= grant.Download {
						found = true
					}
				}
				if !found {
					t.Fatalf("boundary snapshot byte uncertainty was unreserved: initial=%v grant=%d", initial, grant.Download)
				}
			}
		})
	}
}

func TestInitialRelativeTimeoutCannotOutliveExpiryOrWindow(t *testing.T) {
	c := resetCases(t)[0]
	s, _, account, _, at := crossingFixture(t, c.period, c.edge)
	view, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int64{30, 60, 61, 90, 120, 180} {
		for _, window := range []bool{false, true} {
			plan := view.Subscription.Plan
			until := at.Add(time.Duration(seconds) * time.Second)
			expires := until
			if window {
				expires = at.Add(time.Hour)
				plan.Policies = append([]Policy{}, plan.Policies...)
				cutoff := until.UTC()
				plan.Policies[0].Windows = []Window{{Days: []int{0, 1, 2, 3, 4, 5, 6}, From: 0, Until: cutoff.Hour()*3600 + cutoff.Minute()*60 + cutoff.Second(), Zone: "UTC"}}
			}
			grant, _, end, err := planAdmission(plan, plan.Speeds[0].ID, at, expires, map[string]Usage{}, 1)
			if seconds <= 60 {
				if err == nil {
					t.Fatalf("unknown NAS start admitted past cutoff: seconds=%d window=%v", seconds, window)
				}
				continue
			}
			if err != nil || grant.Decision.Timeout != min(int64(60), seconds-60) || end.After(until) {
				t.Fatalf("relative timeout exceeded latest-start cutoff: seconds=%d window=%v timeout=%d end=%s err=%v", seconds, window, grant.Decision.Timeout, end, err)
			}
		}
	}
}

func TestInitialGrantStopsBeforeUnavailableFutureByteEndpoint(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, _, account, _, at := crossingFixture(t, c.period, c.edge)
			view, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil {
				t.Fatal(err)
			}
			plan := view.Subscription.Plan
			at = c.edge.Add(-90 * time.Second)
			for _, quality := range []string{"fresh", "unknown"} {
				usage := map[string]Usage{Key("download", c.edge): {Used: 64 << 20, Quality: quality}}
				grant, reservations, end, err := planAdmission(plan, plan.Speeds[0].ID, at, c.edge.Add(time.Hour), usage, 1)
				if err != nil || grant.Decision.Timeout != 29 || !end.Equal(c.edge.Add(-time.Second)) {
					t.Fatal("grant can reach unavailable byte endpoint", quality, grant, end, err)
				}
				for _, r := range reservations {
					if !r.Start.Before(c.edge) {
						t.Fatal("unreachable future period retained an allocation", r)
					}
				}
			}
		})
	}
}
