package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxAccountStatusBatch = 50

const AccountStatusIndexes = `CREATE INDEX IF NOT EXISTS subscriber_status_revision ON subscriber_audit(target,id DESC) WHERE action IN ('account.active','account.suspended','account.status.batch');`

const accountReviewColumns = `id,username,name,status,(SELECT COALESCE(MAX(id),0) FROM subscriber_audit WHERE target=subscriber_accounts.id AND action IN ('account.active','account.suspended','account.status.batch'))`

type AccountStatusExpectation struct {
	AccountID        string    `json:"accountId"`
	ExpectedStatus   string    `json:"expectedStatus"`
	ExpectedRevision *Quantity `json:"expectedRevision"`
}

type AccountStatusBatch struct {
	BatchID string    `json:"batchId"`
	Changed []Account `json:"changed"`
	Skipped int       `json:"skipped"`
}

// SetStatusBatch fences every reviewed status and commits every changed account
// with its audit record. A stale, missing or unauditable row rolls back the batch.
// Subscriptions, credentials, reservations and measured usage are never rewritten.
func (s *Store) SetStatusBatch(ctx context.Context, actor, status, reason string, items []AccountStatusExpectation) (AccountStatusBatch, error) {
	var out AccountStatusBatch
	validStatus := func(value string) bool { return value == "active" || value == "suspended" }
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || !utf8.ValidString(reason) || len(reason) > 2048 {
		return out, errors.New("actor and a change reason of at most 2048 UTF-8 bytes are required")
	}
	if !validStatus(status) || len(items) == 0 || len(items) > MaxAccountStatusBatch {
		return out, errors.New("choose a valid status and 1 to 50 reviewed subscribers")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.AccountID == "" || !validStatus(item.ExpectedStatus) || item.ExpectedRevision == nil || seen[item.AccountID] {
			return out, errors.New("every subscriber needs a unique account ID, reviewed status and revision")
		}
		seen[item.AccountID] = true
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out = AccountStatusBatch{BatchID: ID(), Changed: []Account{}}
	at := time.Now()
	for _, item := range items {
		var account Account
		err = tx.QueryRowContext(ctx, `SELECT `+accountReviewColumns+` FROM subscriber_accounts WHERE id=?`, item.AccountID).Scan(&account.ID, &account.Username, &account.Name, &account.Status, &account.StatusRevision)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (account.Status != item.ExpectedStatus || account.StatusRevision != *item.ExpectedRevision) {
			return AccountStatusBatch{}, ErrConflict
		}
		if err != nil {
			return AccountStatusBatch{}, err
		}
		if account.Status == status {
			out.Skipped++
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE subscriber_accounts SET status=? WHERE id=? AND status=?`, status, item.AccountID, item.ExpectedStatus)
		if err != nil {
			return AccountStatusBatch{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return AccountStatusBatch{}, err
		}
		if count != 1 {
			return AccountStatusBatch{}, ErrConflict
		}
		history, err := json.Marshal(struct {
			BatchID string `json:"batchId"`
			Reason  string `json:"reason"`
			Before  string `json:"before"`
			After   string `json:"after"`
		}{out.BatchID, reason, account.Status, status})
		if err != nil {
			return AccountStatusBatch{}, err
		}
		if err = audit(tx, actor, "account.status.batch", account.ID, string(history), at); err != nil {
			return AccountStatusBatch{}, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT last_insert_rowid()`).Scan(&account.StatusRevision); err != nil {
			return AccountStatusBatch{}, err
		}
		account.Status = status
		out.Changed = append(out.Changed, account)
	}
	if err = tx.Commit(); err != nil {
		return AccountStatusBatch{}, err
	}
	return out, nil
}
