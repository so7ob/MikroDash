package subscriber_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"mikrodash/internal/db"
	. "mikrodash/internal/subscriber"
)

var ctx = context.Background()

func at(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}
func plan() Plan {
	return Plan{ID: "lab", Version: 1, Name: "Synthetic test plan", ValidSeconds: 86400 * 60, Start: "first-login", MaxSessions: 2, Networks: []string{"C", "D"}, Down: 8000000, Up: 2000000, Speeds: []Speed{{ID: "economy", Name: "Economy", Down: 2000000, Up: 1000000}, {ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []Policy{{ID: "all", Limits: []Limit{
		{ID: "time", Kind: "seconds", State: "limited", Amount: 3600, Period: Period{Kind: "daily", Zone: "Asia/Aden", Anchor: at("2026-01-01T00:00:00+03:00")}, Action: "disconnect"},
		{ID: "download", Kind: "download", State: "limited", Amount: 2 << 30, Period: Period{Kind: "daily", Zone: "Asia/Aden", Anchor: at("2026-01-01T00:00:00+03:00")}, Action: "disconnect"},
		{ID: "upload", Kind: "upload", State: "limited", Amount: 512 << 20, Period: Period{Kind: "daily", Zone: "Asia/Aden", Anchor: at("2026-01-01T00:00:00+03:00")}, Action: "disconnect"},
		{ID: "total", Kind: "total", State: "limited", Amount: 10 << 30, Period: Period{Kind: "monthly", Zone: "Asia/Aden", Anchor: at("2026-01-01T00:00:00+03:00")}, Action: "disconnect"},
	}}}}
}
func setup(t *testing.T, p Plan) (*Store, Account) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := d.Subscribers()
	a, _, err := s.CreateAccount(ctx, "test-admin", "synthetic", "اختبار Unicode", "synthetic-password", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SavePlan(ctx, "test-admin", "test", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(ctx, "test-admin", a.ID, p.ID, p.Version, "test", at("2026-10-01T10:00:00+03:00")); err != nil {
		t.Fatal(err)
	}
	return s, a
}
func snapshot(t *testing.T, s *Store, a Account, now time.Time) Snapshot {
	t.Helper()
	v, err := s.Snapshot(ctx, a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func remain(v Snapshot, id string) Quantity {
	for _, l := range v.Decision.Limits {
		if l.Limit.ID == id {
			return l.Remaining
		}
	}
	panic(id)
}
func reserve(t *testing.T, s *Store, a Account, device string, now time.Time) Admission {
	t.Helper()
	v, err := s.Reserve(ctx, a.ID, "router-lab", "C", device, ID(), now)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func event(a Admission, device, status string, seconds, down, up Quantity, now time.Time) Accounting {
	return Accounting{LeaseID: a.LeaseID, RouterID: "router-lab", Network: "C", Device: device, SessionID: a.LeaseID + "-nas", Status: status, Seconds: seconds, Download: down, Upload: up, At: now}
}
func TestTimeSurvivesSessionsAndSpeed(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	before := snapshot(t, s, a, now)
	if remain(before, "time") != 3600 || before.LifetimeSeconds != 0 {
		t.Fatal(before)
	}
	for i, n := range []Quantity{109, 51} {
		v := reserve(t, s, a, "device", now)
		e := event(v, "device", "stop", n, 0, 0, now.Add(time.Duration(n)*time.Second))
		if err := s.Account(ctx, e); err != nil {
			t.Fatal(err)
		}
		now = e.At.Add(time.Second)
		if i == 0 && remain(snapshot(t, s, a, now), "time") != 3491 {
			t.Fatal("first debit")
		}
	}
	v := snapshot(t, s, a, now)
	if remain(v, "time") != 3440 || v.LifetimeSeconds != 160 {
		t.Fatal(v)
	}
	id := v.Subscription.ID
	expires := v.Subscription.ExpiresAt
	for i, id := range []string{"fast", "economy", "fast"} {
		if err := s.ChooseSpeed(ctx, a.ID, id, now.Add(time.Duration(i*11)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	v = snapshot(t, s, a, now)
	if v.Subscription.ID != id || !v.Subscription.ExpiresAt.Equal(expires) || remain(v, "time") != 3440 || v.Subscription.SpeedState != "pending-reconnect" {
		t.Fatal("speed changed entitlement")
	}
}
func TestTwentyMinutesReconnectLeavesForty(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	v := reserve(t, s, a, "device", now)
	if err := s.Account(ctx, event(v, "device", "stop", 1200, 0, 0, now.Add(20*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if remain(snapshot(t, s, a, now.Add(20*time.Minute)), "time") != 2400 {
		t.Fatal("reset")
	}
	next := reserve(t, s, a, "device", now.Add(21*time.Minute))
	if next.Decision.Timeout > 60 {
		t.Fatal("unbounded lease")
	}
}
func TestDuplicateLateStopDoesNotCloseNewSession(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	old := reserve(t, s, a, "device", now)
	stop := event(old, "device", "stop", 20, 1<<32, 50, now.Add(20*time.Second))
	if err := s.Account(ctx, stop); err != nil {
		t.Fatal(err)
	}
	// Use a new daily bucket after exhausting the download cap.
	now = now.Add(24 * time.Hour)
	next := reserve(t, s, a, "device", now)
	for i := 0; i < 3; i++ {
		if err := s.Account(ctx, stop); err != nil {
			t.Fatal(err)
		}
	}
	earlier := stop
	earlier.Status = "interim"
	earlier.Seconds = 19
	earlier.Download = 100
	if err := s.Account(ctx, earlier); err != nil {
		t.Fatal(err)
	}
	v := snapshot(t, s, a, now)
	if v.LifetimeSeconds != 20 || v.LifetimeDownload != 1<<32 {
		t.Fatal(v)
	}
	found := false
	for _, l := range v.Sessions {
		if l.ID == next.LeaseID {
			found = l.State != "stopped"
		}
	}
	if !found {
		t.Fatal("late stop closed new lease")
	}
}
func TestAtomicSharedAdmission(t *testing.T) {
	p := plan()
	p.Policies[0].Limits[0].Amount = 80
	s, a := setup(t, p)
	now := at("2026-10-01T10:00:00+03:00")
	var wg sync.WaitGroup
	accepted := make(chan Admission, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Reserve(ctx, a.ID, "router-lab", "C", ID(), ID(), now)
			if err == nil {
				accepted <- v
			}
		}()
	}
	wg.Wait()
	close(accepted)
	sum := int64(0)
	n := 0
	for v := range accepted {
		n++
		sum += v.Decision.Timeout
	}
	if n != 2 || sum != 80 {
		t.Fatalf("accepted=%d seconds=%d", n, sum)
	}
	v := snapshot(t, s, a, now)
	if remain(v, "time") != 0 {
		t.Fatal(v)
	}
}
func TestBoundaryTimeAndBytesRemainAuditable(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-31T23:59:50+03:00")
	v := reserve(t, s, a, "device", now)
	if v.Decision.Timeout != 60 {
		t.Fatal(v)
	}
	for _, expected := range []struct {
		start string
		value Quantity
	}{{"2026-10-31T00:00:00+03:00", 15}, {"2026-11-01T00:00:00+03:00", 60}} {
		view := snapshot(t, s, a, at(expected.start))
		found := false
		for _, limit := range view.Decision.Limits {
			if limit.Limit.ID == "time" {
				found = true
				if limit.Reserved != expected.value {
					t.Fatal("initial grant lacks conservative period coverage", expected, limit)
				}
			}
		}
		if !found {
			t.Fatal("time limit missing from public snapshot")
		}
	}
	if err := s.Account(ctx, event(v, "device", "stop", 20, 100, 0, now.Add(20*time.Second))); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, s, a, now.Add(20*time.Second))
	if remain(after, "time") != 3590 || after.LifetimeSeconds != 20 || after.LifetimeDownload != 100 {
		t.Fatal(after)
	}
	for _, l := range after.Decision.Limits {
		if l.Limit.ID == "download" && (l.Used != 0 || l.Provisional != 100 || l.Reserved < 100 || l.Quality != "reconciling") {
			t.Fatal(l)
		}
	}
}
func TestOneSecondSkewAndStartNeverMakesBalanceUnknown(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	v := reserve(t, s, a, "device", now)
	if err := s.Account(ctx, event(v, "device", "start", 0, 0, 0, now)); err != nil {
		t.Fatal(err)
	}
	for _, n := range []Quantity{10, 9, 11} {
		if err := s.Account(ctx, event(v, "device", "interim", n, n*10, 0, now.Add(time.Duration(n)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	got := snapshot(t, s, a, now.Add(11*time.Second))
	if got.LifetimeSeconds != 11 || got.LifetimeDownload != 110 || got.Decision.Limits[0].Quality == "unknown" {
		t.Fatal(got)
	}
}
func TestExpiredAndSuspendedStillAuthenticateForPortal(t *testing.T) {
	s, a := setup(t, plan())
	if err := s.SetStatus(ctx, "admin", a.ID, "suspended", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, a.Username, "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, a.ID, "router-lab", "C", "device", ID(), time.Now()); err == nil {
		t.Fatal("suspended admission")
	}
	if err := s.ChooseSpeed(ctx, a.ID, "unentitled", time.Now()); err == nil {
		t.Fatal("unentitled speed")
	}
}
func TestAccountingIdentityAndOverflowFailClosed(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	v := reserve(t, s, a, "device", now)
	e := event(v, "forged", "stop", 1, 0, 0, now)
	if err := s.Account(ctx, e); err == nil {
		t.Fatal("forged device")
	}
	e.Device = "device"
	e.Download = 1 << 62
	e.Upload = 1 << 62
	if err := s.Account(ctx, e); err == nil {
		t.Fatal("overflow")
	}
	if snapshot(t, s, a, now).LifetimeSeconds != 0 {
		t.Fatal("partial commit")
	}
}
func TestPeriodsAndWindows(t *testing.T) {
	p := Period{Kind: "hourly", Zone: "Asia/Aden", Anchor: at("2026-01-01T00:25:00+03:00")}
	start, end, err := p.Bounds(at("2026-10-01T10:24:59+03:00"))
	if err != nil || start.Hour() != 9 || start.Minute() != 25 || end.Hour() != 10 {
		t.Fatal(start, end, err)
	}
	w := Window{Days: []int{1}, From: 22 * 3600, Until: 2 * 3600, Zone: "Asia/Aden"}
	if !w.Allows(at("2026-10-06T01:00:00+03:00")) || w.Allows(at("2026-10-07T01:00:00+03:00")) {
		t.Fatal("overnight weekday")
	}
}
func TestQuantityJSONAndUnicode(t *testing.T) {
	for _, raw := range []string{`"4294967296"`, `"9007199254740993"`, `"9223372036854775807"`} {
		var q Quantity
		if err := json.Unmarshal([]byte(raw), &q); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(q)
		if string(got) != raw {
			t.Fatal(string(got))
		}
	}
	for _, raw := range []string{`42`, `"-1"`, `"9223372036854775808"`, `"1.5"`} {
		var q Quantity
		if json.Unmarshal([]byte(raw), &q) == nil {
			t.Fatal(raw)
		}
	}
	s, a := setup(t, plan())
	got, err := s.Authenticate(ctx, a.Username, "synthetic-password")
	if err != nil || got.Name != "اختبار Unicode" {
		t.Fatal(got, err)
	}
}
func FuzzPeriodContainsTimestamp(f *testing.F) {
	f.Add(int64(1790845200), uint8(0))
	f.Fuzz(func(t *testing.T, unix int64, k uint8) {
		if unix < 946684800 || unix > 4102444800 {
			return
		}
		p := Period{Kind: []string{"hourly", "daily", "weekly", "monthly"}[k%4], Zone: "Asia/Aden", Anchor: at("2000-01-01T00:25:00+03:00")}
		now := time.Unix(unix, 0)
		start, end, err := p.Bounds(now)
		if err != nil || start.After(now) || !end.After(now) {
			t.Fatal(start, end, err)
		}
	})
}

func TestRetransmissionAfterStopOrRevocationIsDenied(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	v, err := s.Reserve(ctx, a.ID, "router-lab", "C", "device", "same-request", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Account(ctx, event(v, "device", "stop", 1, 0, 0, now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(ctx, a.ID, "router-lab", "C", "device", "same-request", now.Add(2*time.Second)); err == nil {
		t.Fatal("replayed a completed authorization")
	}
}
func FuzzAccountingMonotoneReplay(f *testing.F) {
	f.Add(uint16(109), uint16(51))
	f.Fuzz(func(t *testing.T, x, y uint16) {
		s, a := setup(t, plan())
		now := at("2026-10-01T10:00:00+03:00")
		v := reserve(t, s, a, "device", now)
		for _, n := range []Quantity{Quantity(x), Quantity(y), Quantity(x), Quantity(y)} {
			e := event(v, "device", "interim", n, n*123, 0, now.Add(time.Duration(n)*time.Second))
			if err := s.Account(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		got := snapshot(t, s, a, now)
		want := Quantity(max(x, y))
		if got.LifetimeSeconds != want || got.LifetimeDownload != want*123 {
			t.Fatal("non-monotone accounting")
		}
	})
}

func TestSnapshotReflectsAdmissionStateAndActualObservation(t *testing.T) {
	p := plan()
	p.MaxSessions = 1
	s, a := setup(t, p)
	now := at("2026-10-01T10:00:00+03:00")
	initial := snapshot(t, s, a, now)
	if !initial.Decision.Allowed {
		t.Fatal(initial.Decision)
	}
	lease := reserve(t, s, a, "device", now)
	waiting := snapshot(t, s, a, now.Add(time.Second))
	if waiting.Decision.Allowed || waiting.Quality != "reconciling" {
		t.Fatal(waiting)
	}
	if err := s.Account(ctx, event(lease, "device", "start", 0, 0, 0, now)); err != nil {
		t.Fatal(err)
	}
	fresh := snapshot(t, s, a, now.Add(time.Second))
	if !fresh.ObservedAt.Equal(now) || !fresh.LastSyncedAt.Equal(now) {
		t.Fatal("snapshot fabricated a new observation")
	}
	stale := snapshot(t, s, a, now.Add(66*time.Second))
	if stale.Quality != "stale" || fresh.Version == stale.Version {
		t.Fatal("freshness change missing from version")
	}
	if err := s.Account(ctx, event(lease, "device", "stop", 20, 0, 0, now.Add(20*time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, "test-admin", a.ID, "suspended", "test"); err != nil {
		t.Fatal(err)
	}
	suspended := snapshot(t, s, a, now.Add(21*time.Second))
	if suspended.Decision.Allowed {
		t.Fatal("simulator allowed suspended account")
	}
	expired := snapshot(t, s, a, now.Add(61*24*time.Hour))
	if expired.Subscription.Status != "expired" {
		t.Fatal("expired subscription rendered active")
	}
}

func TestRestartRetainsLedgerReservationsAndDeduplication(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := d.Subscribers()
	a, _, err := s.CreateAccount(ctx, "test-admin", "restart-fixture", "Synthetic restart fixture", "synthetic-password", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	p := plan()
	if err = s.SavePlan(ctx, "test-admin", "test", p); err != nil {
		t.Fatal(err)
	}
	now := at("2026-10-01T10:00:00+03:00")
	if _, err = s.Assign(ctx, "test-admin", a.ID, p.ID, p.Version, "test", now); err != nil {
		t.Fatal(err)
	}
	lease := reserve(t, s, a, "device", now)
	reading := event(lease, "device", "interim", 20, 100, 200, now.Add(20*time.Second))
	if err = s.Account(ctx, reading); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s, a, now.Add(20*time.Second))
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s = d.Subscribers()
	if err = s.Account(ctx, reading); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, s, a, now.Add(20*time.Second))
	if before.Version != after.Version || after.LifetimeSeconds != 20 || after.LifetimeDownload != 100 || after.LifetimeUpload != 200 {
		t.Fatal("restart or retransmission changed durable accounting")
	}
	// Losing Stop or rebooting the NAS does not free a live delegation's slot.
	if _, err = s.Reserve(ctx, a.ID, "router-lab", "C", "device", ID(), now.Add(70*time.Second)); err == nil {
		t.Fatal("missing Stop freed an unreconciled reservation")
	}
	reading.Status = "stop"
	if err = s.Account(ctx, reading); err != nil {
		t.Fatal(err)
	}
	final := snapshot(t, s, a, now.Add(71*time.Second))
	if remain(final, "time") != 3580 {
		t.Fatal("late Stop lost remaining time")
	}
	reserve(t, s, a, "device", now.Add(72*time.Second))
}

func TestLateByteCorrectionAcrossMidnightCannotEscapePeriodCharge(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T23:59:50+03:00")
	lease := reserve(t, s, a, "device", now)
	if err := s.Account(ctx, event(lease, "device", "interim", 20, 100, 0, now.Add(20*time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := s.Account(ctx, event(lease, "device", "interim", 5, 200, 0, now.Add(5*time.Second))); err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{now, now.Add(20 * time.Second)} {
		v := snapshot(t, s, a, when)
		for _, limit := range v.Decision.Limits {
			if limit.Limit.ID == "download" && (limit.Used != 0 || limit.Provisional != 200 || limit.Reserved < 200 || limit.Quality != "reconciling") {
				t.Fatal("late correction escaped provisional protection", limit)
			}
		}
		if v.LifetimeDownload != 200 || v.LifetimeSeconds != 20 {
			t.Fatal("correction duplicated lifetime usage")
		}
	}
}

func TestAllLimitsRemainIndependentWhenDownloadIsExhausted(t *testing.T) {
	s, a := setup(t, plan())
	now := at("2026-10-01T10:00:00+03:00")
	lease := reserve(t, s, a, "device", now)
	e := event(lease, "device", "interim", 20, 2<<30, 32, now.Add(20*time.Second))
	if err := s.Account(ctx, e); err != nil {
		t.Fatal(err)
	}
	v := snapshot(t, s, a, now.Add(20*time.Second))
	if v.Decision.Allowed || !v.Decision.Disconnect || len(v.Decision.Limits) != 4 {
		t.Fatal(v.Decision)
	}
	due, err := s.Due(ctx, now.Add(20*time.Second))
	if err != nil || len(due) != 1 || due[0].Reason != "policy-disconnect" {
		t.Fatal("exhaustion did not reach background enforcement", due, err)
	}
	e.Status = "stop"
	if err = s.Account(ctx, e); err != nil {
		t.Fatal(err)
	}
	v = snapshot(t, s, a, now.Add(21*time.Second))
	if remain(v, "time") != 3580 || remain(v, "download") != 0 || remain(v, "upload") != (512<<20)-32 || remain(v, "total") != (8<<30)-32 {
		t.Fatal("one limit changed another limit", v.Decision)
	}
	if _, err = s.Reserve(ctx, a.ID, "router-lab", "C", "new-device", ID(), now.Add(22*time.Second)); err == nil {
		t.Fatal("exhausted daily download admitted a new connection")
	}
	next := snapshot(t, s, a, now.Add(24*time.Hour))
	if remain(next, "time") != 3600 || remain(next, "download") != 2<<30 || remain(next, "total") != (8<<30)-32 {
		t.Fatal("daily renewal reset monthly usage")
	}
}

func TestDifferentRoutersAndNetworksShareOneEntitlement(t *testing.T) {
	p := plan()
	p.Policies[0].Limits[0].Amount = 80
	s, a := setup(t, p)
	now := at("2026-10-01T10:00:00+03:00")
	first, err := s.Reserve(ctx, a.ID, "router-one", "C", "device-one", ID(), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Reserve(ctx, a.ID, "router-two", "D", "device-two", ID(), now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision.Timeout != 40 || second.Decision.Timeout != 40 {
		t.Fatal("routers received duplicated time budgets")
	}
	if _, err = s.Reserve(ctx, a.ID, "router-three", "C", "device-three", ID(), now); err == nil {
		t.Fatal("router boundary bypassed account session limit")
	}
	if remain(snapshot(t, s, a, now), "time") != 0 {
		t.Fatal("reserved time remains spendable")
	}
}
