package subscriber

import (
	"database/sql"
	"encoding/json"
	"strconv"
)

// A late partial packet cannot undo a committed final RADIUS reading. Require
// an actual complete Stop receipt, rather than an API high-water snapshot or a
// locally expired delegation. Contradictory counters still require review.
func coveredByFinalReceipt(tx *sql.Tx, r AccountingReceipt) (bool, error) {
	rows, err := tx.Query(`SELECT payload FROM subscriber_accounting_receipts WHERE lease_id=? AND state='applied' AND json_extract(payload,'$.status')='stop'`, r.LeaseID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var final AccountingReceipt
		if err = rows.Scan(&raw); err != nil {
			return false, err
		}
		if err = json.Unmarshal([]byte(raw), &final); err != nil {
			return false, err
		}
		e, err := final.reading()
		if err != nil {
			continue
		}
		if r.Seconds != "" {
			n, err := strconv.ParseUint(r.Seconds, 10, 64)
			if err != nil || n > uint64(e.Seconds) {
				continue
			}
		}
		if readingCoversReceipt(e, r) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// A complete API absence closes a seat, but cannot supply final counters.
// Do not rely on quota holds alone: unlimited plans can have no holds, and
// measured router byte-cap overruns mean a local grant is not an exact debit.
// A trusted Stop or reasoned final review records a stop event atomically.
const missingFinalCondition = `EXISTS(SELECT 1 FROM subscriber_reconciliations finality WHERE finality.lease_id=l.id AND finality.actor='service:subscriber')
 AND NOT EXISTS(SELECT 1 FROM subscriber_events final_event WHERE final_event.lease_id=l.id AND final_event.status='stop')`

func pendingAccounting(tx *sql.Tx, account string) (int, error) {
	var count int
	// Preserve the existing receipt count. An absent session adds one issue only
	// when no pending receipt already represents that same unresolved finality.
	err := tx.QueryRow(`SELECT count(*) FROM (
 SELECT r.id FROM subscriber_accounting_receipts r JOIN subscriber_leases l ON l.id=r.lease_id JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE sub.account_id=? AND r.state='pending'
 UNION ALL
 SELECT l.id FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE sub.account_id=? AND l.state='stopped' AND `+missingFinalCondition+`
 AND NOT EXISTS(SELECT 1 FROM subscriber_accounting_receipts r WHERE r.lease_id=l.id AND r.state='pending')
)`, account, account).Scan(&count)
	return count, err
}

func pendingForLease(tx *sql.Tx, lease string) ([]AccountingReceipt, error) {
	rows, err := tx.Query(`SELECT payload FROM subscriber_accounting_receipts WHERE lease_id=? AND state='pending'`, lease)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountingReceipt{}
	for rows.Next() {
		var raw string
		var r AccountingReceipt
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func readingCoversReceipt(e Accounting, r AccountingReceipt) bool {
	if r.SessionID != "" && e.SessionID != "" && r.SessionID != e.SessionID {
		return false
	}
	floor := func(raw string, actual Quantity, tolerance uint64) bool {
		if raw == "" {
			return true
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || actual < 0 {
			return false
		}
		return n <= uint64(actual) || n-uint64(actual) <= tolerance
	}
	// Keep the last trusted second when snapshots round on opposite edges. A
	// one-second time-only discrepancy is not an unknown byte balance.
	return floor(r.Seconds, e.Seconds, 1) && floor(r.Download, e.Download, 0) && floor(r.Upload, e.Upload, 0)
}

func coversPendingFinal(tx *sql.Tx, e Accounting) (bool, error) {
	pending, err := pendingForLease(tx, e.LeaseID)
	if err != nil {
		return false, err
	}
	for _, r := range pending {
		if !readingCoversReceipt(e, r) {
			return false, nil
		}
	}
	return true, nil
}

func resolveReceiptCoverage(tx *sql.Tx, e Accounting) error {
	if e.Status == "start" {
		return nil
	}
	pending, err := pendingForLease(tx, e.LeaseID)
	if err != nil {
		return err
	}
	for _, r := range pending {
		if !readingCoversReceipt(e, r) {
			continue
		}
		if r.Status == "stop" && e.Status != "stop" {
			continue
		}
		if e.Source == "api" && e.At.Before(r.At) {
			continue
		}
		if _, err = tx.Exec(`UPDATE subscriber_accounting_receipts SET state='covered',resolved_at=? WHERE id=? AND state='pending'`, e.At.Unix(), r.ID); err != nil {
			return err
		}
		if err = audit(tx, "service:subscriber", "accounting.receipt.covered", r.ID, "covered by reliable "+e.Source+" cumulative reading", e.At); err != nil {
			return err
		}
	}
	return nil
}

func preserveReceiptSeconds(tx *sql.Tx, e Accounting) (Accounting, error) {
	pending, err := pendingForLease(tx, e.LeaseID)
	if err != nil {
		return e, err
	}
	for _, r := range pending {
		if !readingCoversReceipt(e, r) {
			continue
		}
		if raw, err := strconv.ParseInt(r.Seconds, 10, 64); err == nil && raw >= 0 && Quantity(raw) > e.Seconds {
			e.Seconds = Quantity(raw)
		}
	}
	return e, nil
}
