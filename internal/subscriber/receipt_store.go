package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func insertReceipt(tx *sql.Tx, r AccountingReceipt, owned bool) (bool, error) {
	var lease any
	if owned {
		lease = r.LeaseID
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	var resolved int64
	if !r.ResolvedAt.IsZero() {
		resolved = r.ResolvedAt.Unix()
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO subscriber_accounting_receipts(id,lease_id,router_id,state,received_at,resolved_at,payload) VALUES(?,?,?,?,?,?,?)`, r.ID, lease, r.RouterID, r.State, r.ReceivedAt.Unix(), resolved, string(raw))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) retainFailedReceipt(ctx context.Context, r AccountingReceipt) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r.State = "pending"
	r.Reason = "apply-failed"
	added, err := insertReceipt(tx, r, true)
	if err != nil {
		return err
	}
	if added {
		if err = audit(tx, "service:subscriber", "accounting.receipt.pending", r.ID, "reliable reading could not be applied; original receipt retained", r.ReceivedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReceiveAccounting commits the receipt and any debit together. A complete
// reading that fails to apply is retained separately and remains unacknowledged
// for retry; it never turns a failed ledger update into a successful settlement.
func (s *Store) ReceiveAccounting(ctx context.Context, r AccountingReceipt) (bool, error) {
	if r.RouterID == "" || r.ID == "" || r.ReceivedAt.IsZero() {
		return false, ErrDenied
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var previous, original string
	err = tx.QueryRow(`SELECT state,payload FROM subscriber_accounting_receipts WHERE id=?`, r.ID).Scan(&previous, &original)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil && (previous == "applied" || previous == "covered" || previous == "rejected") {
		return previous != "rejected", tx.Commit()
	}
	if err == nil && previous == "pending" {
		var saved AccountingReceipt
		if err = json.Unmarshal([]byte(original), &saved); err != nil {
			return false, err
		}
		r.At = saved.At
		r.ReceivedAt = saved.ReceivedAt
	}
	if r.Status == "nas-on" || r.Status == "nas-off" {
		r.State = "applied"
		r.Reason = "authenticated router accounting marker"
		added, e := insertReceipt(tx, r, false)
		if e != nil {
			return false, e
		}
		if added {
			if e = audit(tx, "service:subscriber", "accounting."+r.Status, r.RouterID, "authenticated NAS marker; balances and sessions retained", r.ReceivedAt); e != nil {
				return false, e
			}
		}
		return true, tx.Commit()
	}
	var router, network, device, session string
	identityErr := tx.QueryRow(`SELECT router_id,network,device,session_id FROM subscriber_leases WHERE id=?`, r.LeaseID).Scan(&router, &network, &device, &session)
	owned := identityErr == nil && r.Network != "" && r.Device != "" && r.SessionID != "" && router == r.RouterID && network == r.Network && device == r.Device && (session == "" || session == r.SessionID)
	if !owned || (r.Status != "start" && r.Status != "interim" && r.Status != "stop") {
		r.State = "rejected"
		r.Reason = "accounting identity does not match an owned delegation"
		if _, err = insertReceipt(tx, r, false); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	e, readingErr := r.reading()
	r.State = "pending"
	r.Reason = ""
	if readingErr != nil {
		r.Reason = "statistics-incomplete"
	}
	if readingErr == nil && e.Status == "stop" {
		covered, err := coversPendingFinal(tx, e)
		if err != nil {
			return false, err
		}
		if !covered {
			readingErr = errors.New("earlier incomplete counters are not covered")
			r.Reason = "prior-reading-not-covered"
		}
	}
	if readingErr != nil {
		covered, err := coveredByFinalReceipt(tx, r)
		if err != nil {
			return false, err
		}
		if covered {
			r.State = "covered"
			r.Reason = "covered-by-committed-final-stop"
			r.ResolvedAt = r.ReceivedAt
			added, err := insertReceipt(tx, r, true)
			if err != nil {
				return false, err
			}
			if !added {
				if _, err = tx.Exec(`UPDATE subscriber_accounting_receipts SET state='covered',resolved_at=? WHERE id=? AND state='pending'`, r.ResolvedAt.Unix(), r.ID); err != nil {
					return false, err
				}
			}
			if err = audit(tx, "service:subscriber", "accounting.receipt.covered", r.ID, "late partial evidence covered by a committed complete Stop", r.ReceivedAt); err != nil {
				return false, err
			}
			return true, tx.Commit()
		}
	}
	added, err := insertReceipt(tx, r, true)
	if err != nil {
		return false, err
	}
	if readingErr != nil {
		if added {
			if err = audit(tx, "service:subscriber", "accounting.receipt.pending", r.ID, r.Reason+"; last reliable usage and delegations retained", r.ReceivedAt); err != nil {
				return false, err
			}
		}
		return true, tx.Commit()
	}
	e, err = preserveReceiptSeconds(tx, e)
	if err != nil {
		return false, err
	}
	if err = s.accountTx(tx, e); err != nil {
		_ = tx.Rollback()
		if retained := s.retainFailedReceipt(ctx, r); retained != nil {
			return false, retained
		}
		return false, err
	}
	if err = resolveReceiptCoverage(tx, e); err != nil {
		return false, err
	}
	if _, err = tx.Exec(`UPDATE subscriber_accounting_receipts SET state='applied',resolved_at=? WHERE id=?`, r.ReceivedAt.Unix(), r.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) AccountingReceipts(ctx context.Context, account string) ([]AccountingReceipt, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT r.payload,r.state,r.resolved_at FROM subscriber_accounting_receipts r JOIN subscriber_leases l ON l.id=r.lease_id JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE sub.account_id=? ORDER BY (r.state='pending') DESC,r.received_at DESC,r.id LIMIT 1000`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountingReceipt{}
	for rows.Next() {
		var r AccountingReceipt
		var raw string
		var resolved int64
		if err = rows.Scan(&raw, &r.State, &resolved); err != nil {
			return nil, err
		}
		state := r.State
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		r.State = state
		if resolved > 0 {
			r.ResolvedAt = time.Unix(resolved, 0)
		}
		if r.Fields == nil {
			r.Fields = map[string]string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
