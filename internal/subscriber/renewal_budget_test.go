package subscriber

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRenewalCannotRegrantObservationLag(t *testing.T) {
	for _, lag := range []int64{0, 1, 5} {
		t.Run(time.Duration(lag*time.Second.Nanoseconds()).String(), func(t *testing.T) {
			s, svc, a, grant, start := liveFixture(t, func(p *Plan) { p.MaxSessions = 1; p.Policies[0].Limits[0].Amount = 60 })
			now := start.Add(45 * time.Second)
			sample := observation(svc, a, grant, start, now)
			sample.Sessions[0].Seconds -= Quantity(lag)
			sample.Sessions[0].Download = sample.Sessions[0].Seconds * 1000
			if err := s.ObserveRouter(t.Context(), sample, svc.Clients); err != nil {
				t.Fatal(err)
			}
			op, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
			if err != nil {
				t.Fatal(err)
			}
			total := op.TargetDeadline.Unix() - op.Request.StartedAt.Unix()
			if total != 60 {
				t.Fatalf("observation lag granted extra time: lag=%d total=%d want=60", lag, total)
			}
			view, err := s.Snapshot(t.Context(), a.ID, now)
			if err != nil || view.LifetimeSeconds != sample.Sessions[0].Seconds {
				t.Fatal("renewal altered consumption", view, err)
			}
		})
	}
}

func TestRenewalReadbackUsesNASUptimeAndCannotVerifyAnExcessCap(t *testing.T) {
	start := time.Unix(100000, 0)
	o := Operation{Kind: "renew", CreatedAt: start.Add(45 * time.Second), UpdatedAt: start.Add(45 * time.Second), TargetDeadline: start.Add(60 * time.Second), Request: OperationRequest{StartedAt: start, DownloadCeiling: 100, UploadCeiling: 50}}
	row := ObservedSession{Seconds: 45, TimeLeft: 15, TimeoutKnown: true, LimitsKnown: true, DownloadLimit: 100, UploadLimit: 50}
	if !operationVerified(o, row, start.Add(50*time.Second)) {
		t.Fatal("API completion lag invalidated consistent NAS uptime/time-left")
	}
	row.TimeLeft = 16
	if operationVerified(o, row, start.Add(50*time.Second)) {
		t.Fatal("verified more cumulative time than reserved")
	}
	row.TimeLeft = 14
	if !operationVerified(o, row, start.Add(50*time.Second)) {
		t.Fatal("safe smaller timeout was refused")
	}
	row.Seconds = 9223372036854775807
	row.TimeLeft = 1
	if operationVerified(o, row, start.Add(50*time.Second)) {
		t.Fatal("overflow verified a timeout")
	}
}

func TestLegacyRenewalRetirementIsAtomicAndRetainsIssuedExposure(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t, func(p *Plan) { p.MaxSessions = 2; p.Policies[0].Limits[0].Amount = 120 })
	svc.AllowCoA = false // Local retirement must also work after network permission is revoked.
	now := start.Add(45 * time.Second)
	sample := observation(svc, a, grant, start, now)
	sample.Sessions[0].Seconds = 40
	sample.Sessions[0].Download = 40000
	if err := s.ObserveRouter(t.Context(), sample, svc.Clients); err != nil {
		t.Fatal(err)
	}
	o, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
	if err != nil {
		t.Fatal(err)
	}
	o.Request.TimeBasis = ""
	o.TargetDeadline = o.TargetDeadline.Add(5 * time.Second)
	raw, err := json.Marshal(o.Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`UPDATE subscriber_operations SET request=?,target_deadline=?,state='applying',attempts=1 WHERE id=?`, string(raw), o.TargetDeadline.Unix(), o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(t.Context(), a.ID, "lab", "C", "other", "legacy-blocked", now); !errors.Is(err, ErrDenied) {
		t.Fatal("legacy uncertainty admitted a new device", err)
	}
	if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_retirement_fail BEFORE INSERT ON subscriber_audit WHEN NEW.action='session.renew.legacy-retired' BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessOperations(t.Context(), now); err == nil {
		t.Fatal("retirement audit failure did not abort")
	}
	pending, err := s.Operations(t.Context(), true)
	if err != nil || len(pending) != 1 {
		t.Fatal("retirement partially committed", pending, err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || before.Decision.Limits[0].Reserved != 40 {
		t.Fatal(before, err)
	}
	if _, err = s.sql.Exec(`DROP TRIGGER synthetic_retirement_fail`); err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessOperations(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	ops, err := s.Operations(t.Context(), false)
	if err != nil || len(ops) != 1 || ops[0].State != "failed" {
		t.Fatal("legacy intent not fenced", ops, err)
	}
	view, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || view.LifetimeSeconds != 40 || view.Decision.Limits[0].Reserved != 45 || view.Decision.Limits[0].Remaining != 35 {
		t.Fatal("issued exposure was released or charged", view, err)
	}
	other, err := s.Reserve(t.Context(), a.ID, "lab", "C", "other", "after-legacy-retirement", now)
	if err != nil || other.Decision.Timeout != 35 {
		t.Fatal("remaining shared credit was not conserved", other, err)
	}
}

func FuzzRenewalConservesTimeBudget(f *testing.F) {
	f.Add(uint8(40), uint8(0))
	f.Add(uint8(40), uint8(1))
	f.Add(uint8(40), uint8(5))
	f.Fuzz(func(t *testing.T, usedByte, lagByte uint8) {
		used := int64(usedByte % 59)
		lag := int64(lagByte % 6)
		if used+lag >= 60 {
			return
		}
		s, svc, a, grant, start := liveFixture(t, func(p *Plan) { p.MaxSessions = 1; p.Policies[0].Limits[0].Amount = 60 })
		now := start.Add(time.Duration(used+lag) * time.Second)
		sample := observation(svc, a, grant, start, now)
		sample.Sessions[0].Seconds = Quantity(used)
		sample.Sessions[0].Download = Quantity(used * 1000)
		if err := s.ObserveRouter(t.Context(), sample, svc.Clients); err != nil {
			t.Fatal(err)
		}
		op, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
		if err != nil {
			t.Fatal(err)
		}
		total := op.TargetDeadline.Unix() - op.Request.StartedAt.Unix()
		if total > 60 || op.Request.GrantedSeconds > Quantity(60-used) {
			t.Fatal("renewal exceeded shared credit", used, lag, op)
		}
		view, err := s.Snapshot(t.Context(), a.ID, now)
		if err != nil || view.LifetimeSeconds != Quantity(used) {
			t.Fatal("renewal changed consumption", view, err)
		}
	})
}
