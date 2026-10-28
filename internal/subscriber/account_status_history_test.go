package subscriber

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStatusHistoryPagesExactIDsAndIsolatesAccounts(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	at := time.Now()
	before, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	// IDs deliberately exceed JavaScript's exact integer range; timestamps tie.
	const base int64 = 9007199254740993
	for i := int64(0); i < 53; i++ {
		action := "account.suspended"
		if i%2 == 0 {
			action = "account.active"
		}
		if _, err = s.sql.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,?,?,?,?)`, base+i, "synthetic-admin", action, a.ID, fmt.Sprintf("reason %d", i), 100); err != nil {
			t.Fatal(err)
		}
	}
	for i, entry := range []struct{ target, action string }{{"other", "account.active"}, {a.ID, "account.name.edit"}, {a.ID, "credential.issue"}} {
		if _, err = s.sql.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,?,?,?,?)`, base+100+int64(i), "synthetic-admin", entry.action, entry.target, "private unrelated record", 100); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.StatusHistory(t.Context(), a.ID, 0)
	if err != nil || len(first.Events) != 50 || first.NextBefore == nil || *first.NextBefore != Quantity(base+3) || first.Events[0].ID != Quantity(base+52) {
		t.Fatal(first, err)
	}
	raw, _ := json.Marshal(first)
	if !strings.Contains(string(raw), `"id":"9007199254741045"`) || !strings.Contains(string(raw), `"nextBefore":"9007199254740996"`) || strings.Contains(string(raw), "private unrelated") {
		t.Fatal(string(raw))
	}
	// An intervening new change must not repeat or displace older page entries.
	if err = s.SetStatus(t.Context(), "synthetic-admin", a.ID, "active", "intervening change"); err != nil {
		t.Fatal(err)
	}
	second, err := s.StatusHistory(t.Context(), a.ID, *first.NextBefore)
	if err != nil || len(second.Events) != 3 || second.NextBefore != nil || second.Events[0].ID != Quantity(base+2) || second.Events[2].ID != Quantity(base) {
		t.Fatal(second, err)
	}
	repeated, err := s.StatusHistory(t.Context(), a.ID, *first.NextBefore)
	if err != nil || !reflect.DeepEqual(second, repeated) {
		t.Fatal("repeated page changed", err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("history changed balances", err)
	}
}

func TestStatusHistoryEmptyValidationAndBatchEvidence(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	empty, err := s.StatusHistory(t.Context(), a.ID, 0)
	if err != nil || empty.Events == nil || len(empty.Events) != 0 || empty.NextBefore != nil {
		t.Fatal(empty, err)
	}
	for _, q := range []struct {
		id     string
		before Quantity
	}{{"", 0}, {"missing", 0}, {a.ID, -1}} {
		if _, err = s.StatusHistory(t.Context(), q.id, q.before); err == nil {
			t.Fatal("invalid history request accepted", q)
		}
	}
	revision := a.StatusRevision
	batch, err := s.SetStatusBatch(t.Context(), "synthetic-admin", "suspended", "سبب synthetic", []AccountStatusExpectation{{AccountID: a.ID, ExpectedStatus: a.Status, ExpectedRevision: &revision}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.StatusHistory(t.Context(), a.ID, 0)
	if err != nil || len(result.Events) != 1 || result.Events[0].Action != "account.status.batch" || result.Events[0].Actor != "synthetic-admin" {
		t.Fatal(result, err)
	}
	var reason map[string]string
	if err = json.Unmarshal([]byte(result.Events[0].Reason), &reason); err != nil || reason["batchId"] != batch.BatchID || reason["before"] != "active" || reason["after"] != "suspended" || reason["reason"] != "سبب synthetic" {
		t.Fatal(reason, err)
	}
}
