package subscriber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2866"
)

func reviewedStatus(account Account) AccountStatusExpectation {
	revision := account.StatusRevision
	return AccountStatusExpectation{AccountID: account.ID, ExpectedStatus: account.Status, ExpectedRevision: &revision}
}

func statusBatchFixture(t *testing.T) (*Store, Account, Account, []AccountStatusExpectation) {
	t.Helper()
	store, _, first, _ := protocolFixture(t)
	second, _, err := store.CreateAccount(t.Context(), "synthetic-admin", "synthetic-ثاني", "Synthetic Unicode: مشترك", "synthetic-password", "Synthetic batch fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	return store, first, second, []AccountStatusExpectation{reviewedStatus(first), reviewedStatus(second)}
}

func statusAuditCount(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='account.status.batch'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestStatusBatchPreservesUsageIdentityCredentialsAndCorrelatedAudit(t *testing.T) {
	store, first, second, items := statusBatchFixture(t)
	at := time.Now()
	before, err := store.Snapshot(t.Context(), first.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Reviewed synthetic Unicode: سبب", items)
	if err != nil || result.BatchID == "" || len(result.Changed) != 2 || result.Skipped != 0 {
		t.Fatal(result, err)
	}
	for _, account := range []Account{first, second} {
		current, err := store.Authenticate(t.Context(), account.Username, "synthetic-password")
		if err != nil || current.ID != account.ID || current.Username != account.Username || current.Name != account.Name || current.Status != "suspended" {
			t.Fatal("status change damaged portal credentials or Unicode identity", current, err)
		}
		var raw string
		if err = store.sql.QueryRow(`SELECT reason FROM subscriber_audit WHERE action='account.status.batch' AND target=? AND actor='synthetic-admin'`, account.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var audit map[string]string
		if err = json.Unmarshal([]byte(raw), &audit); err != nil || audit["batchId"] != result.BatchID || audit["before"] != "active" || audit["after"] != "suspended" || audit["reason"] != "Reviewed synthetic Unicode: سبب" {
			t.Fatal("audit did not match committed selection", audit, err)
		}
	}
	after, err := store.Snapshot(t.Context(), first.ID, at)
	if err != nil || !reflect.DeepEqual(before.Subscription, after.Subscription) || !reflect.DeepEqual(before.Sessions, after.Sessions) || before.LifetimeSeconds != after.LifetimeSeconds || before.LifetimeDownload != after.LifetimeDownload || before.LifetimeUpload != after.LifetimeUpload || after.Decision.Allowed {
		t.Fatal("batch changed entitlement/usage or still allowed admission", err)
	}
	if _, err = store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Retry stale response", items); !errors.Is(err, ErrConflict) || statusAuditCount(t, store) != 2 {
		t.Fatal("stale replay repeated the batch", err)
	}
	for i := range items {
		current, err := store.AccountByID(t.Context(), items[i].AccountID)
		if err != nil {
			t.Fatal(err)
		}
		items[i] = reviewedStatus(current)
	}
	noop, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Reviewed no-op", items)
	if err != nil || len(noop.Changed) != 0 || noop.Skipped != 2 || statusAuditCount(t, store) != 2 {
		t.Fatal("unchanged statuses created a second mutation", noop, err)
	}
	if _, err = store.SetStatusBatch(t.Context(), "synthetic-admin", "active", "Reviewed reactivation", items); err != nil {
		t.Fatal(err)
	}
	active, err := store.Snapshot(t.Context(), first.ID, at)
	if err != nil || !active.Decision.Allowed || !reflect.DeepEqual(active.Subscription, before.Subscription) || active.LifetimeSeconds != before.LifetimeSeconds {
		t.Fatal("reactivation changed the subscription or reset credit", err)
	}
}

func TestStatusBatchRollsBackEarlierUpdatesOnConflictOrAuditFailure(t *testing.T) {
	for _, cause := range []string{"missing", "stale", "audit"} {
		t.Run(cause, func(t *testing.T) {
			store, first, second, items := statusBatchFixture(t)
			switch cause {
			case "missing":
				items[1].AccountID = "synthetic-missing-account"
			case "stale":
				items[1].ExpectedStatus = "suspended"
			case "audit":
				if _, err := store.sql.Exec(`CREATE TRIGGER synthetic_batch_audit_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='account.status.batch' AND (SELECT count(*) FROM subscriber_audit WHERE action='account.status.batch')>0 BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if result, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Synthetic transaction fault", items); err == nil || len(result.Changed) != 0 || result.BatchID != "" {
				t.Fatal("partial batch reported success", result, err)
			}
			for _, account := range []Account{first, second} {
				current, err := store.AccountByID(t.Context(), account.ID)
				if err != nil || current != account {
					t.Fatal("failed batch left a partial account update", current, err)
				}
			}
			if statusAuditCount(t, store) != 0 {
				t.Fatal("failed transaction retained partial audit")
			}
		})
	}
}

func TestStatusBatchValidatesSelectionBeforeWriting(t *testing.T) {
	store, first, _, items := statusBatchFixture(t)
	for _, tc := range []struct {
		actor, status, reason string
		items                 []AccountStatusExpectation
	}{
		{" ", "suspended", "Reason", items},
		{"synthetic-admin", "suspended", " ", items},
		{"synthetic-admin", "suspended", strings.Repeat("x", 2049), items},
		{"synthetic-admin", "suspended", string([]byte{255}), items},
		{"synthetic-admin", "deleted", "Reason", items},
		{"synthetic-admin", "suspended", "Reason", nil},
		{"synthetic-admin", "suspended", "Reason", make([]AccountStatusExpectation, 51)},
		{"synthetic-admin", "suspended", "Reason", []AccountStatusExpectation{{AccountID: first.ID, ExpectedStatus: ""}}},
		{"synthetic-admin", "suspended", "Reason", []AccountStatusExpectation{reviewedStatus(first), reviewedStatus(first)}},
	} {
		if _, err := store.SetStatusBatch(t.Context(), tc.actor, tc.status, tc.reason, tc.items); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	current, err := store.AccountByID(t.Context(), first.ID)
	if err != nil || current != first || statusAuditCount(t, store) != 0 {
		t.Fatal("invalid selection changed an account", err)
	}
}

func TestConcurrentOverlappingStatusBatchesCommitOnlyOneReview(t *testing.T) {
	store, _, _, items := statusBatchFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Concurrent reviewed selection", items)
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	committed, conflicts := 0, 0
	for err := range results {
		if err == nil {
			committed++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if committed != 1 || conflicts != 1 || statusAuditCount(t, store) != 2 {
		t.Fatal("overlapping reviews were both applied", committed, conflicts)
	}
}

func TestStatusBatchSuspensionFeedsGuardedWorkerWithoutBrowsers(t *testing.T) {
	store, service, account, grant, start := liveFixture(t)
	if _, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Reviewed background suspension", []AccountStatusExpectation{reviewedStatus(account)}); err != nil {
		t.Fatal(err)
	}
	due, err := store.Due(t.Context(), start)
	if err != nil || len(due) != 1 || due[0].Reason != "account-suspended" {
		t.Fatal("batch was not consumed before local delegation expiry", due, err)
	}
	packets := disconnectNAS(t, service, radius.CodeDisconnectACK)
	service.Observe = func(context.Context, string) (RouterObservation, error) {
		return observation(service, account, grant, start, time.Now()), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); service.Run(ctx, true, func(err error) { t.Log(err) }) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case packet := <-packets:
		if packet.Code != radius.CodeDisconnectRequest || rfc2866.AcctSessionID_GetString(packet) != "session" {
			t.Fatal("batch targeted another session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch suspension did not reach guarded background Disconnect")
	}
	cancel()
	<-done
	remaining, err := store.Due(t.Context(), time.Now())
	if err != nil || len(remaining) != 1 || remaining[0].LeaseID != grant.LeaseID {
		t.Fatal("Disconnect ACK silently released unresolved accounting", remaining, err)
	}
}

func TestStatusBatchAcceptsFiftyReviewedAccountsAndPreservesVoucher(t *testing.T) {
	store, _, first, _ := protocolFixture(t)
	items := []AccountStatusExpectation{reviewedStatus(first)}
	var voucher Account
	var code string
	for i := 1; i < MaxAccountStatusBatch; i++ {
		length := 0
		if i == 1 {
			length = 24
		}
		account, token, err := store.CreateAccount(t.Context(), "synthetic-admin", fmt.Sprintf("synthetic-batch-%d", i), "Synthetic boundary fixture", "synthetic-password", "Synthetic batch boundary", length)
		if err != nil {
			t.Fatal(err)
		}
		if length != 0 {
			voucher, code = account, token
		}
		items = append(items, reviewedStatus(account))
	}
	result, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Reviewed maximum-sized batch", items)
	if err != nil || len(result.Changed) != MaxAccountStatusBatch || statusAuditCount(t, store) != MaxAccountStatusBatch {
		t.Fatal("valid maximum-sized batch failed", err)
	}
	current, err := store.Authenticate(t.Context(), "", code)
	if err != nil || code == "" || current.ID != voucher.ID || current.Username != voucher.Username || current.Status != "suspended" {
		t.Fatal("batch changed the one-code voucher identity", err)
	}
}

func TestStatusBatchRejectsChangedThenRestoredStatus(t *testing.T) {
	store, first, second, items := statusBatchFixture(t)
	for _, status := range []string{"suspended", "active"} {
		if err := store.SetStatus(t.Context(), "synthetic-other-admin", second.ID, status, "Synthetic concurrent reviewed change"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Outdated pre-ABA selection", items); !errors.Is(err, ErrConflict) {
		t.Fatal("textually restored status reused a stale review", err)
	}
	current, err := store.AccountByID(t.Context(), first.ID)
	if err != nil || current != first || statusAuditCount(t, store) != 0 {
		t.Fatal("ABA conflict did not roll back the first account", err)
	}
	changed, err := store.AccountByID(t.Context(), second.ID)
	if err != nil || changed.Status != "active" || changed.StatusRevision == *items[1].ExpectedRevision {
		t.Fatal("status history did not advance review identity", err)
	}
}

func TestStatusRevisionJSONPreservesLargeAuditIdentity(t *testing.T) {
	store, _, account, _ := protocolFixture(t)
	const large = 9007199254740993
	if _, err := store.sql.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,'account.active',?,?,?)`, large, "synthetic-admin", account.ID, "Synthetic large status revision", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	current, err := store.AccountByID(t.Context(), account.ID)
	if err != nil || current.StatusRevision != large {
		t.Fatal("large status revision lost precision", err)
	}
	raw, err := json.Marshal(current)
	if err != nil || !strings.Contains(string(raw), `"statusRevision":"9007199254740993"`) {
		t.Fatal("revision was not a decimal JSON string", err)
	}
	result, err := store.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "Reviewed large revision", []AccountStatusExpectation{reviewedStatus(current)})
	if err != nil || len(result.Changed) != 1 || result.Changed[0].StatusRevision != large+1 {
		t.Fatal("committed status did not retain exact audit identity", err)
	}
}
