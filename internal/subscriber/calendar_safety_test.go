package subscriber

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func reservationRows(t *testing.T, s *Store) string {
	t.Helper()
	var result string
	if err := s.sql.QueryRow(`SELECT coalesce(group_concat(entry,'|'),'') FROM (SELECT lease_id||':'||limit_id||':'||period_start||':'||amount AS entry FROM subscriber_reservations ORDER BY lease_id,limit_id,period_start)`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCalendarRenewalAuditFailureRollsBackEveryPeriod(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, svc, a, grant, start := crossingFixture(t, c.period, c.edge)
			if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: start}); err != nil {
				t.Fatal(err)
			}
			now := start.Add(5 * time.Second)
			if err := s.ObserveRouter(t.Context(), observation(svc, a, grant, start, now), svc.Clients); err != nil {
				t.Fatal(err)
			}
			before := reservationRows(t, s)
			snapshot, err := s.Snapshot(t.Context(), a.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_calendar_rollback BEFORE INSERT ON subscriber_audit WHEN NEW.action='session.renew.requested' BEGIN SELECT RAISE(ABORT,'synthetic calendar audit failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err = s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now); err == nil {
				t.Fatal("audit failure did not abort renewal")
			}
			rows, err := s.Operations(t.Context(), false)
			if err != nil || len(rows) != 0 {
				t.Fatal("partial operation committed", rows, err)
			}
			after, err := s.Snapshot(t.Context(), a.ID, now)
			if err != nil || after.Version != snapshot.Version || reservationRows(t, s) != before {
				t.Fatal("calendar reservation or measured balance partially committed", after, err)
			}
			if _, err = s.sql.Exec(`DROP TRIGGER synthetic_calendar_rollback`); err != nil {
				t.Fatal(err)
			}
			op, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
			if err != nil || !op.TargetDeadline.After(c.edge) {
				t.Fatal("positive continuation did not cross reset", op, err)
			}
		})
	}
}

func TestCalendarConcurrentRenewalAndOtherNetworkAdmissionShareBudget(t *testing.T) {
	for _, c := range resetCases(t) {
		t.Run(c.name, func(t *testing.T) {
			s, svc, a, grant, start := crossingFixture(t, c.period, c.edge, func(p *Plan) {
				p.MaxSessions = 2
				p.Networks = []string{"C", "D"}
				p.Policies[0].Limits[0].Amount = 100
			})
			if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: start}); err != nil {
				t.Fatal(err)
			}
			now := start.Add(5 * time.Second)
			if err := s.ObserveRouter(t.Context(), observation(svc, a, grant, start, now), svc.Clients); err != nil {
				t.Fatal(err)
			}
			begin := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			var renewal Operation
			var second Admission
			var renewErr, admitErr error
			go func() {
				defer wg.Done()
				<-begin
				renewal, renewErr = s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
			}()
			go func() {
				defer wg.Done()
				<-begin
				second, admitErr = s.Reserve(t.Context(), a.ID, "lab", "D", "device-two", "calendar-other-network", now)
			}()
			close(begin)
			wg.Wait()
			if renewErr != nil || admitErr != nil || !renewal.TargetDeadline.After(c.edge) || second.LeaseID == grant.LeaseID {
				t.Fatal("independent sessions did not receive bounded grants", renewal, second, renewErr, admitErr)
			}
			if grant.Decision.Timeout != 50 || second.Decision.Timeout > 50 {
				t.Fatal("each device received the entire shared future time balance", grant.Decision.Timeout, second.Decision.Timeout)
			}
			snapshot, err := s.Snapshot(t.Context(), a.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			for _, policy := range snapshot.Subscription.Plan.Policies {
				for _, limit := range policy.Limits {
					rows, err := s.sql.Query(`SELECT r.period_start,sum(r.amount)+coalesce(b.used,0) FROM subscriber_reservations r LEFT JOIN subscriber_buckets b ON b.subscription_id=? AND b.limit_id=r.limit_id AND b.period_start=r.period_start WHERE r.limit_id=? GROUP BY r.period_start`, snapshot.Subscription.ID, limit.ID)
					if err != nil {
						t.Fatal(err)
					}
					count := 0
					for rows.Next() {
						var period int64
						var exposure Quantity
						if err = rows.Scan(&period, &exposure); err != nil {
							rows.Close()
							t.Fatal(err)
						}
						count++
						if exposure > limit.Amount {
							rows.Close()
							t.Fatalf("quota granted twice: limit=%s period=%d exposure=%d amount=%d", limit.ID, period, exposure, limit.Amount)
						}
					}
					err = rows.Err()
					rows.Close()
					if err != nil || count < 2 {
						t.Fatal("missing old/future allocation census", limit.ID, count, err)
					}
				}
			}
			if snapshot.LifetimeSeconds != 5 || snapshot.LifetimeDownload != 5000 {
				t.Fatal("reservation race changed confirmed use", snapshot)
			}
		})
	}
}

// This checks accepted relative start times against independent interval
// intersections, rather than requiring the planner to choose one exact grant.
func FuzzCalendarAdmissionPeriodConservation(f *testing.F) {
	f.Add(uint8(0), uint8(100), uint8(0), uint8(0), uint8(1))
	f.Add(uint8(3), uint8(119), uint8(99), uint8(2), uint8(2))
	f.Add(uint8(4), uint8(90), uint8(25), uint8(50), uint8(4))
	f.Fuzz(func(t *testing.T, kind, offset, oldUsed, futureUsed, slotsByte uint8) {
		cases := resetCases(t)
		c := cases[int(kind)%len(cases)]
		at := c.edge.Add(time.Duration(int(offset)-120) * time.Second)
		slots := Quantity(slotsByte%4 + 1)
		p := Plan{ID: "synthetic-calendar", Name: "Synthetic calendar conservation", Version: 1, ValidSeconds: 86400, Start: "first-login", MaxSessions: 4, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []Speed{{ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []Policy{{ID: "all", Limits: []Limit{{ID: "time", Kind: "seconds", State: "limited", Amount: 100, Period: c.period, Action: "disconnect"}, {ID: "bytes", Kind: "download", State: "limited", Amount: 1 << 20, Period: c.period, Action: "disconnect"}}}}}
		old, _, err := c.period.Bounds(c.edge.Add(-time.Second))
		if err != nil {
			t.Fatal(err)
		}
		usage := map[string]Usage{Key("time", old): {Used: Quantity(oldUsed % 101)}, Key("time", c.edge): {Used: Quantity(futureUsed % 101)}, Key("bytes", old): {Reserved: Quantity(oldUsed%100) * 10000}, Key("bytes", c.edge): {Provisional: Quantity(futureUsed%100) * 10000}}
		grant, reservations, end, err := planAdmission(p, "fast", at, at.Add(time.Hour), usage, slots)
		if err != nil {
			if !errors.Is(err, ErrDenied) {
				t.Fatal(err)
			}
			return
		}
		if grant.Decision.Timeout < 1 || grant.Decision.Timeout > 60 || !end.Equal(at.Add(time.Duration(60+grant.Decision.Timeout)*time.Second)) {
			t.Fatal("invalid bounded delegation", grant, end)
		}
		allocated := map[string]Quantity{}
		for _, r := range reservations {
			key := Key(r.LimitID, r.Start)
			if _, exists := allocated[key]; exists {
				t.Fatal("duplicate period", key)
			}
			allocated[key] = r.Amount
			limit := p.Policies[0].Limits[0]
			if r.LimitID == "bytes" {
				limit = p.Policies[0].Limits[1]
			}
			u := usage[key]
			remaining := max(Quantity(0), limit.Amount-u.Used-max(u.Reserved, u.Provisional)) / slots
			if r.Amount > remaining || r.Amount < 0 {
				t.Fatal("reservation exceeds spendable shared quota", key, r.Amount, remaining)
			}
		}
		for delay := int64(-5); delay <= 60; delay++ {
			from := at.Add(time.Duration(delay) * time.Second)
			until := from.Add(time.Duration(grant.Decision.Timeout) * time.Second)
			for cursor := from; cursor.Before(until); {
				period, next, err := c.period.Bounds(cursor)
				if err != nil {
					t.Fatal(err)
				}
				edge := min(next.Unix(), until.Unix())
				required := Quantity(edge - cursor.Unix())
				if allocated[Key("time", period)] < required || allocated[Key("bytes", period)] < grant.Download {
					t.Fatal("accepted start lacks period coverage", delay, period, required, allocated, grant)
				}
				cursor = time.Unix(edge, 0)
			}
			endpoint, _, err := c.period.Bounds(until)
			if err != nil {
				t.Fatal(err)
			}
			if allocated[Key("bytes", endpoint)] < grant.Download {
				t.Fatal(fmt.Sprintf("unreserved byte endpoint: delay=%d endpoint=%s", delay, endpoint))
			}
		}
	})
}
