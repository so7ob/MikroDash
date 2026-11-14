package subscriber

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"
)

func credentialFixture(t *testing.T) (*Store, Account) {
	t.Helper()
	h, err := sql.Open("sqlite", protocolFixtureURI(filepath.Join(t.TempDir(), "credentials.db")))
	if err != nil {
		t.Fatal(err)
	}
	h.SetMaxOpenConns(1)
	t.Cleanup(func() { h.Close() })
	if _, err = h.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	s := NewStore(h)
	a, _, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-credential", "Synthetic اسم", "synthetic-old-password", "synthetic fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	p := Plan{ID: "synthetic-plan", Version: 1, Name: "Synthetic quota", ValidSeconds: 86400, Start: "assigned", MaxSessions: 2, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []Speed{{ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []Policy{{ID: "all", Limits: []Limit{{ID: "seconds", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: Period{Kind: "none", Zone: "UTC", Anchor: time.Now().Add(-time.Hour)}}}}}}
	if err = s.SavePlan(t.Context(), "synthetic-admin", "synthetic fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", a.ID, p.ID, 1, "synthetic fixture", time.Now()); err != nil {
		t.Fatal(err)
	}
	return s, a
}
func credentialTicket(t *testing.T, s *Store, a Account) string {
	t.Helper()
	token, err := s.IssueTicket(t.Context(), TicketRequest{AccountID: a.ID, CredentialRevision: a.CredentialRevision, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:99", ClientAddress: "192.0.2.99"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func ticketBinding(a Account) TicketBinding {
	return TicketBinding{RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:99", Username: a.Username}
}

func TestCredentialResetPreservesEntitlementAndRevokesOldProofs(t *testing.T) {
	s, a := credentialFixture(t)
	now := time.Now()
	token := credentialTicket(t, s, a)
	proof, err := s.Authenticate(t.Context(), a.Username, "synthetic-old-password")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.ReserveAuthenticated(t.Context(), proof, "synthetic-router", "C", "02:00:00:00:00:98", "synthetic-before-reset", now)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-new-password", "Synthetic reviewed rotation", 0)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != a.ID || updated.Username != a.Username || updated.Name != a.Name || updated.Status != a.Status || updated.CredentialRevision != 1 {
		t.Fatal("reset changed account identity or failed to advance revision")
	}
	if _, err = s.Authenticate(t.Context(), a.Username, "synthetic-old-password"); !errors.Is(err, ErrDenied) {
		t.Fatal("old credential remained valid")
	}
	current, err := s.Authenticate(t.Context(), a.Username, "synthetic-new-password")
	if err != nil || current.CredentialRevision != 1 {
		t.Fatal("new credential did not authenticate", err)
	}
	if err = s.ValidateCredentialRevision(t.Context(), a.ID, 0); !errors.Is(err, ErrDenied) {
		t.Fatal("old portal proof remained valid")
	}
	for _, request := range []string{"synthetic-before-reset", "synthetic-new-after-reset"} {
		if _, err = s.ReserveAuthenticated(t.Context(), proof, "synthetic-router", "C", "02:00:00:00:00:98", request, now); !errors.Is(err, ErrDenied) {
			t.Fatal("stale authentication admitted or replayed", err)
		}
	}
	if _, err = s.RedeemTicket(t.Context(), token, ticketBinding(a), "synthetic-ticket", now); !errors.Is(err, ErrDenied) {
		t.Fatal("old ticket redeemed", err)
	}
	if _, err = s.IssueTicket(t.Context(), TicketRequest{AccountID: a.ID, CredentialRevision: 0, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:99", ClientAddress: "192.0.2.99"}, now); !errors.Is(err, ErrDenied) {
		t.Fatal("in-flight stale portal request issued ticket")
	}
	fresh := credentialTicket(t, s, current)
	if _, err = s.RedeemTicket(t.Context(), fresh, ticketBinding(a), "synthetic-fresh-ticket", now); err != nil {
		t.Fatal("current ticket rejected", err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rotation changed commercial entitlement, usage, network history or held exposure", err)
	}
	if after.Sessions[0].ID != grant.LeaseID {
		t.Fatal("existing lease lost")
	}
	var auditReason string
	if err = s.sql.QueryRow(`SELECT reason FROM subscriber_audit WHERE action='account.credential.reset' AND target=?`, a.ID).Scan(&auditReason); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Reason string
		Before string `json:"beforeRevision"`
		After  string `json:"afterRevision"`
	}
	if json.Unmarshal([]byte(auditReason), &event) != nil || event.Before != "0" || event.After != "1" {
		t.Fatal("reset audit lost exact revision transition")
	}
	if strings.Contains(auditReason, "synthetic-new-password") || strings.Contains(auditReason, "synthetic-old-password") {
		t.Fatal("secret leaked into audit")
	}
	payload, _ := json.Marshal(updated)
	for _, field := range []string{"password", "salt", "hash", "voucher"} {
		if strings.Contains(string(payload), field) {
			t.Fatal("credential material returned in account metadata")
		}
	}
}

func TestCredentialResetConcurrentReviewsAndExactRevision(t *testing.T) {
	s, a := credentialFixture(t)
	const initial Quantity = 9007199254740993
	if _, err := s.sql.Exec(`INSERT INTO subscriber_credential_revisions VALUES(?,?)`, a.ID, initial); err != nil {
		t.Fatal(err)
	}
	meta, err := s.AccountByID(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(meta)
	if !strings.Contains(string(encoded), `"credentialRevision":"9007199254740993"`) {
		t.Fatal("metadata rounded credential revision")
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, []string{"synthetic-winner-one", "synthetic-winner-two"}[i], "Synthetic concurrent reset", initial)
		}(i)
	}
	close(start)
	wg.Wait()
	winners, conflicts := 0, 0
	for i, err := range results {
		if err == nil {
			winners++
			if _, authErr := s.Authenticate(t.Context(), a.Username, []string{"synthetic-winner-one", "synthetic-winner-two"}[i]); authErr != nil {
				t.Fatal(authErr)
			}
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("same reviewed epoch committed more than once", winners, conflicts)
	}
	current, err := s.AccountByID(t.Context(), a.ID)
	if err != nil || current.CredentialRevision != initial+1 {
		t.Fatal("concurrent reset lost exact revision", err)
	}
	var count int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='account.credential.reset'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("concurrent reset audit was not atomic", count, err)
	}
	if _, err = s.sql.Exec(`UPDATE subscriber_credential_revisions SET revision=? WHERE account_id=?`, int64(math.MaxInt64), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-overflow", "Synthetic overflow refusal", Quantity(math.MaxInt64)); err == nil {
		t.Fatal("credential epoch wrapped")
	}
}

func TestCredentialResetAuditAndStorageFailuresRollBackEverything(t *testing.T) {
	for _, stage := range []string{"audit", "storage"} {
		t.Run(stage, func(t *testing.T) {
			s, a := credentialFixture(t)
			token := credentialTicket(t, s, a)
			trigger := `CREATE TRIGGER synthetic_reset_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='account.credential.reset' BEGIN SELECT RAISE(FAIL,'synthetic audit unavailable'); END;`
			if stage == "storage" {
				trigger = `CREATE TRIGGER synthetic_reset_failure BEFORE UPDATE ON subscriber_accounts WHEN NEW.password_hash!=OLD.password_hash BEGIN SELECT RAISE(FAIL,'synthetic storage unavailable'); END;`
			}
			if _, err := s.sql.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-uncommitted-password", "Synthetic rollback test", 0); err == nil {
				t.Fatal("failure committed")
			}
			current, err := s.AccountByID(t.Context(), a.ID)
			if err != nil || current.CredentialRevision != 0 {
				t.Fatal("failure changed epoch", err)
			}
			if _, err = s.Authenticate(t.Context(), a.Username, "synthetic-old-password"); err != nil {
				t.Fatal("failure replaced the old credential", err)
			}
			if _, err = s.Authenticate(t.Context(), a.Username, "synthetic-uncommitted-password"); !errors.Is(err, ErrDenied) {
				t.Fatal("failed secret became valid")
			}
			if _, err = s.RedeemTicket(t.Context(), token, ticketBinding(a), "synthetic-retained-ticket", time.Now()); err != nil {
				t.Fatal("rollback lost the outstanding ticket", err)
			}
		})
	}
}

func TestCredentialResetVoucherScopeAndValidation(t *testing.T) {
	s, a := credentialFixture(t)
	voucher, code, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-voucher", "Synthetic قسيمة", "", "Synthetic voucher", 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", voucher.ID, "synthetic-voucher-replacement", "Synthetic explicit voucher revocation", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(t.Context(), "", code); !errors.Is(err, ErrDenied) {
		t.Fatal("voucher survived password rotation")
	}
	if _, err = s.Authenticate(t.Context(), voucher.Username, "synthetic-voucher-replacement"); err != nil {
		t.Fatal("rotated voucher account cannot use username/password", err)
	}
	if _, err = s.Authenticate(t.Context(), a.Username, "synthetic-old-password"); err != nil {
		t.Fatal("rotation touched another account", err)
	}
	for _, in := range []struct{ actor, password, reason string }{{"", "synthetic-valid-password", "Synthetic reason"}, {"synthetic-admin", "short", "Synthetic reason"}, {"synthetic-admin", "synthetic-valid-password", ""}, {"synthetic-admin", "synthetic-valid-password", strings.Repeat("x", 2049)}, {"synthetic-admin", "synthetic-valid-password", "synthetic-valid-password"}, {"synthetic-admin", "synthetic\npassword", "Synthetic reason"}} {
		if _, err = s.ResetCredential(t.Context(), in.actor, a.ID, in.password, in.reason, 0); err == nil {
			t.Fatal("invalid reset accepted")
		}
	}
}

func TestCredentialProofInFlightBeforeResetCannotReserveAfterReset(t *testing.T) {
	s, a := credentialFixture(t)
	proof, err := s.Authenticate(t.Context(), a.Username, "synthetic-old-password")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-release
		_, err := s.ReserveAuthenticated(t.Context(), proof, "synthetic-router", "C", "02:00:00:00:00:99", "synthetic-delayed-admission", time.Now())
		done <- err
	}()
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-race-new-password", "Synthetic reset while authentication is in flight", 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-done; !errors.Is(err, ErrDenied) {
		t.Fatal("delayed old proof admitted", err)
	}
	var count int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_leases`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale admission reserved entitlement", count, err)
	}
}

func TestRADIUSCredentialRotationRejectsOldAndTicketReplay(t *testing.T) {
	s, _, a, address := protocolFixture(t)
	token, err := s.IssueTicket(t.Context(), TicketRequest{AccountID: a.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", ClientAddress: "192.0.2.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-radius-new-password", "Synthetic RADIUS credential rotation", 0); err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"synthetic-password", token} {
		if reply := exchange(t, access(password), address); reply.Code != radius.CodeAccessReject {
			t.Fatal("old RADIUS credential or ticket accepted", reply.Code)
		}
	}
	packet := access("synthetic-radius-new-password")
	first := exchange(t, packet, address)
	if first.Code != radius.CodeAccessAccept {
		t.Fatal("new RADIUS credential rejected", first.Code)
	}
	repeated := exchange(t, packet, address)
	if repeated.Code != radius.CodeAccessAccept {
		t.Fatal("current authenticated replay lost")
	}
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-radius-next-password", "Synthetic second RADIUS rotation", 1); err != nil {
		t.Fatal(err)
	}
	if reply := exchange(t, packet, address); reply.Code != radius.CodeAccessReject {
		t.Fatal("old password replay accepted after second reset")
	}
}

func TestSessionRegistryRevokesOnlyEarlierCredentialEpochs(t *testing.T) {
	var registry SessionRegistry
	old, _ := registry.New("synthetic-account", 0)
	current, _ := registry.New("synthetic-account", 1)
	foreign, _ := registry.New("synthetic-other", 0)
	registry.RevokeBefore("synthetic-account", 1)
	if _, ok := registry.Get(old); ok {
		t.Fatal("old epoch retained")
	}
	for _, token := range []string{current, foreign} {
		if _, ok := registry.Get(token); !ok {
			t.Fatal("revocation removed a current or foreign session")
		}
	}
}
