package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/audit"
	"mikrodash/internal/db"
	"mikrodash/internal/hub"
	"mikrodash/internal/rbac"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/store"
)

func jobFile(t *testing.T, s *Server, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.store.Dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func awaitDNSJob(t *testing.T, done <-chan DNSBulkJob) DNSBulkJob {
	t.Helper()
	select {
	case j := <-done:
		return j
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
		return DNSBulkJob{}
	}
}

// Each case changes live policy after the first command, while there are still
// allowance tokens. The second command must check again inside InWriteQueue.
func TestDNSJobRechecksBeforeEveryWrite(t *testing.T) {
	for _, kind := range []string{"revoked", "replaced-user", "bulk-off", "fleet-off", "list-off", "shutdown", "cancelled", "router-disabled"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, _ := bulkServer(t, 100, nil)
			a := s.dnsActor(&Session{Username: "someone"}, store.FeatureDNSBulk)
			if kind == "fleet-off" {
				a.feature = store.FeatureDNSFleetSync
			}
			if kind == "list-off" {
				a = dnsJobActor{system: true, username: "system", feature: store.FeatureDNSLists}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.ctx = ctx
			first, proceed := make(chan struct{}), make(chan struct{})
			writes := 0
			sn := session.NewForTestWithExec(hub.New(), "r-A", func(c routeros.Cmd) ([]routeros.Reply, error) {
				if strings.HasSuffix(c.Path, "/print") {
					return nil, nil
				}
				writes++
				if writes == 1 {
					close(first)
					<-proceed
				}
				return nil, nil
			})
			done := make(chan DNSBulkJob, 1)
			steps, _ := planDNSBulk(dnsBulkIn{Add: []map[string]any{{"name": "a.example", "type": "FWD"}, {"name": "b.example", "type": "FWD"}}}, nil)
			_, code := s.beginDNSBulk(sn, func() {}, a, dnsBulkIn{RouterID: "r-A"}, steps, audit.New(auditSinkOf(s), audit.System(), nowMillis), true, func(j DNSBulkJob) { done <- j })
			if code != "" {
				t.Fatal(code)
			}
			select {
			case <-first:
			case <-time.After(3 * time.Second):
				t.Fatal("no first write")
			}
			switch kind {
			case "revoked":
				if _, err := s.auditDB.DeleteGrantsForPrincipal("user", "u-1"); err != nil {
					t.Fatal(err)
				}
			case "replaced-user":
				jobFile(t, s, "users.json", `[{"id":"u-other","username":"someone"}]`)
			case "bulk-off", "fleet-off", "list-off":
				jobFile(t, s, "settings.json", `{}`)
			case "shutdown":
				s.features.Stop()
			case "cancelled":
				cancel()
			case "router-disabled":
				jobFile(t, s, "routers.json", `[{"id":"r-A","host":"198.51.100.1","disabled":true}]`)
			}
			close(proceed)
			j := awaitDNSJob(t, done)
			if writes != 1 || j.State != "stopped" || j.Failed != 1 || j.Added != 1 || j.Unconfirmed != 1 || len(j.Errors) != 1 {
				t.Fatalf("writes=%d job=%+v", writes, j)
			}
			rows, err := s.auditDB.QueryAuditEvents(db.Query{RouterIDs: []string{"r-A"}})
			if err != nil || len(rows.Rows) != 1 {
				t.Fatalf("missing job audit: %+v %v", rows, err)
			}
		})
	}
}

func TestDNSJobChecksInsideAnAlreadyOccupiedWriteQueue(t *testing.T) {
	s, sn, sent, _ := bulkServer(t, 100, nil)
	held, release, unlocked := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { _ = sn.InWriteQueue(func() error { close(held); <-release; return nil }); close(unlocked) }()
	<-held
	done := make(chan DNSBulkJob, 1)
	a := s.dnsActor(&Session{Username: "someone"}, store.FeatureDNSBulk)
	steps, _ := planDNSBulk(dnsBulkIn{Add: []map[string]any{{"name": "a.example", "type": "FWD"}}}, nil)
	_, code := s.beginDNSBulk(sn, func() {}, a, dnsBulkIn{RouterID: "r-A"}, steps, audit.New(auditSinkOf(s), audit.System(), nowMillis), true, func(j DNSBulkJob) { done <- j })
	if code != "" {
		t.Fatal(code)
	}
	jobFile(t, s, "settings.json", `{}`)
	close(release)
	<-unlocked
	j := awaitDNSJob(t, done)
	if len(*sent) != 0 || j.State != "stopped" {
		t.Fatalf("queued job wrote after disabling: %+v %v", j, *sent)
	}
}

func TestDNSJobStopsWhileWaitingForAllowance(t *testing.T) {
	s, sn, sent, _ := bulkServer(t, 1, nil)
	s.jobLimit = newRateLimiter(1, time.Hour)
	a := s.dnsActor(&Session{Username: "someone"}, store.FeatureDNSBulk)
	s.jobLimit.take(writeLimitKey(a.username, "r-A"))
	done := make(chan DNSBulkJob, 1)
	steps, _ := planDNSBulk(dnsBulkIn{Add: []map[string]any{{"name": "a.example", "type": "FWD"}}}, nil)
	job, code := s.beginDNSBulk(sn, func() {}, a, dnsBulkIn{RouterID: "r-A"}, steps, audit.New(auditSinkOf(s), audit.System(), nowMillis), false, func(j DNSBulkJob) { done <- j })
	if code != "" {
		t.Fatal(code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.dnsBulk.snapshot(job).WaitingUntil == 0 {
		if time.Now().After(deadline) {
			t.Fatal("not waiting")
		}
		time.Sleep(time.Millisecond)
	}
	jobFile(t, s, "settings.json", `{}`)
	j := awaitDNSJob(t, done)
	if len(*sent) != 0 || j.State != "stopped" {
		t.Fatalf("waiting job survived: %+v %v", j, *sent)
	}
}

func TestDNSJobSystemAuthorityIsExplicit(t *testing.T) {
	for _, a := range []dnsJobActor{{username: "system", feature: store.FeatureDNSLists}, {username: "system", feature: store.FeatureDNSBulk, system: true}} {
		s, sn, sent, _ := bulkServer(t, 100, nil)
		done := make(chan DNSBulkJob, 1)
		steps, _ := planDNSBulk(dnsBulkIn{Add: []map[string]any{{"name": "a.example", "type": "FWD"}}}, nil)
		_, code := s.beginDNSBulk(sn, func() {}, a, dnsBulkIn{RouterID: "r-A"}, steps, audit.New(auditSinkOf(s), audit.System(), nowMillis), true, func(j DNSBulkJob) { done <- j })
		if code != "" {
			t.Fatal(code)
		}
		if j := awaitDNSJob(t, done); j.State != "stopped" || len(*sent) != 0 {
			t.Fatalf("implicit system authority: %+v", j)
		}
	}
}

func TestDNSBulkEndpointCapturesStableIdentityAndOwner(t *testing.T) {
	s, _, _, _ := bulkServer(t, 100, nil)
	writes := 0
	sn := session.NewForTestWithExec(hub.New(), "r-A", func(c routeros.Cmd) ([]routeros.Reply, error) {
		if strings.HasSuffix(c.Path, "/add") {
			writes++
		}
		return []routeros.Reply{{".id": "*1", "name": "a.example", "type": "FWD"}}, nil
	})
	s.sessions = session.NewManagerForTest(s.store, hub.New(), sn)
	job, code, errs := s.startDNSBulk(context.Background(), &Session{Username: "someone"}, dnsBulkIn{RouterID: "r-A", Add: []map[string]any{{"name": "a.example", "type": "FWD"}}}, audit.New(auditSinkOf(s), audit.System(), nowMillis))
	if code != "" {
		t.Fatalf("%s %v", code, errs)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.dnsBulk.snapshot(job).State == "running" {
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if j := s.dnsBulk.snapshot(job); j.actor.userID != "u-1" || writes != 1 || j.Unconfirmed != 0 {
		t.Fatalf("wrong endpoint job: %+v writes %d", j, writes)
	}
	if w := gateCall(s, s.dnsBulkGet, "/api/dns/bulk?id="+job.ID, true); w.Code != 200 {
		t.Fatalf("owner cannot read job: %d", w.Code)
	}
	jobFile(t, s, "settings.json", `{}`)
	if w := gateCall(s, s.dnsBulkGet, "/api/dns/bulk?id="+job.ID, true); w.Code != 200 {
		t.Fatalf("owner cannot read job after switch-off: %d", w.Code)
	}
	jobFile(t, s, "users.json", `[{"id":"u-other","username":"someone"}]`)
	if w := gateCall(s, s.dnsBulkGet, "/api/dns/bulk?id="+job.ID, true); w.Code != 404 {
		t.Fatalf("new account inherited job: %d", w.Code)
	}
}

// Drive the real purge -> runListJob -> bulk -> readback -> pending-storage
// path, not just a predicate on a hand-made job result.
func TestDNSListPurgeRetainsUnconfirmedRemovals(t *testing.T) {
	for _, mode := range []string{"still-present", "read-failed", "confirmed"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _ := bulkServer(t, 100, nil)
			// Use the additive schema's list table in the synthetic fixture.
			listStore := gateServerWith(t, `{"dnsListsEnabled":true}`, listsDDL, `[{"id":"r-A","host":"198.51.100.1","label":"Synthetic"}]`)
			s.auditDB = listStore.auditDB
			id, err := s.auditDB.SaveDNSList(db.DNSList{Name: "synthetic", AddressList: "vpn", Dropped: []string{"r-A"}})
			if err != nil {
				t.Fatal(err)
			}
			l, _ := s.dnsListByID(id)
			removed := false
			sn := session.NewForTestWithExec(hub.New(), "r-A", func(c routeros.Cmd) ([]routeros.Reply, error) {
				if strings.HasSuffix(c.Path, "/remove") {
					removed = true
					return nil, nil
				}
				if removed && mode == "read-failed" {
					return nil, errors.New("synthetic read failure")
				}
				if removed && mode == "confirmed" {
					return nil, nil
				}
				return []routeros.Reply{{".id": "*1", "name": "gone.example", "type": "FWD", "comment": listTag(l)}}, nil
			})
			s.sessions = session.NewManagerForTest(s.store, hub.New(), sn)
			msg, _ := s.settleDropped(context.Background(), l, dnsJobActor{system: true, username: "system", feature: store.FeatureDNSLists}, audit.New(auditSinkOf(s), audit.System(), nowMillis))
			got, ok := s.dnsListByID(id)
			if !removed || !ok {
				t.Fatal("purge did not exercise removal")
			}
			if mode == "confirmed" {
				if len(got.Dropped) != 0 || msg != "" {
					t.Fatalf("confirmed purge stayed pending: %+v %s", got, msg)
				}
			} else {
				if len(got.Dropped) != 1 || got.Dropped[0] != "r-A" || !strings.Contains(msg, "1 unconfirmed") {
					t.Fatalf("unconfirmed purge forgotten: %+v %s", got, msg)
				}
				still, lines := s.purgeRouters(context.Background(), l, []string{"r-A"}, dnsJobActor{system: true, username: "system", feature: store.FeatureDNSLists}, audit.New(auditSinkOf(s), audit.System(), nowMillis))
				s.finishForget(l, still, lines)
				if _, exists := s.dnsListByID(id); !exists {
					t.Fatal("list forgotten before confirmed removal")
				}
			}
		})
	}
}

// A save reaches the actual HTTP handler while the old cleanup is waiting on
// RouterOS. Its reattached router and changed definition must survive both
// reconciliation and the completion of an older Delete-with-entries request.
func TestDNSListSaveDuringRemovalSurvivesCompletion(t *testing.T) {
	s, _, _, _ := bulkServer(t, 100, nil)
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s.auditDB = database
	s.rbac = rbac.New(database, func() []rbac.Router { return []rbac.Router{{ID: "r-A"}} })
	if err := database.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "u-1", RoleID: "administrator", ScopeType: "global"}); err != nil {
		t.Fatal(err)
	}
	id, err := database.SaveDNSList(db.DNSList{Name: "synthetic", Domains: "old.example", AddressList: "vpn", RouterIDs: []string{}, Dropped: []string{"r-A"}})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := s.dnsListByID(id)
	deleting, resume := make(chan struct{}), make(chan struct{})
	removed := false
	sn := session.NewForTestWithExec(hub.New(), "r-A", func(c routeros.Cmd) ([]routeros.Reply, error) {
		if strings.HasSuffix(c.Path, "/remove") {
			close(deleting)
			<-resume
			removed = true
			return nil, nil
		}
		if removed {
			return nil, nil
		}
		return []routeros.Reply{{".id": "*1", "name": "old.example", "type": "FWD", "comment": listTag(l)}}, nil
	})
	s.sessions = session.NewManagerForTest(s.store, hub.New(), sn)
	done := make(chan bool, 1)
	go func() {
		_, again := s.settleDropped(context.Background(), l, dnsJobActor{system: true, username: "system", feature: store.FeatureDNSLists}, audit.New(auditSinkOf(s), audit.System(), nowMillis))
		done <- again
	}()
	select {
	case <-deleting:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup did not write")
	}
	updated := l
	updated.Name = "synthetic updated"
	updated.Domains = "new.example"
	updated.RouterIDs = []string{"r-A"}
	body, _ := json.Marshal(updated)
	req := httptest.NewRequest("POST", "/api/dns/lists", strings.NewReader(string(body)))
	req.Header.Set("Cookie", authed)
	w := httptest.NewRecorder()
	s.dnsListSave(w, req)
	close(resume)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	select {
	case again := <-done:
		if !again {
			t.Fatal("reattached router was not scheduled for resync")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup did not finish")
	}
	s.finishForget(l, nil, nil)
	got, ok := s.dnsListByID(id)
	if !ok || got.Name != updated.Name || got.Domains != updated.Domains || len(got.RouterIDs) != 1 || len(got.Dropped) != 0 || !strings.HasPrefix(got.LastResult, "stopped:") {
		t.Fatalf("overlapping save lost: %+v exists=%v", got, ok)
	}
}
