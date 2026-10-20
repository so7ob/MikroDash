package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type GapAllocation struct {
	PeriodStart time.Time `json:"periodStart"`
	Amount      Quantity  `json:"amount"`
}
type ByteGap struct {
	ID          string          `json:"id"`
	LeaseID     string          `json:"leaseId"`
	LimitID     string          `json:"limitId"`
	From        time.Time       `json:"from"`
	Until       time.Time       `json:"until"`
	Amount      Quantity        `json:"amount"`
	Periods     []time.Time     `json:"periods"`
	ResolvedAt  time.Time       `json:"resolvedAt"`
	Actor       string          `json:"actor"`
	Reason      string          `json:"reason"`
	Evidence    string          `json:"evidence"`
	Allocations []GapAllocation `json:"allocations"`
}

func (s *Store) ByteGaps(ctx context.Context, account string) ([]ByteGap, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT g.id,g.lease_id,g.limit_id,g.from_at,g.until_at,g.amount,g.resolved_at,g.actor,g.reason,g.evidence,g.allocations FROM subscriber_byte_gaps g JOIN subscriber_leases l ON l.id=g.lease_id JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE sub.account_id=? ORDER BY (g.resolved_at=0) DESC,g.from_at DESC,g.id LIMIT 1000`, account)
	if err != nil {
		return nil, err
	}
	out := []ByteGap{}
	for rows.Next() {
		var g ByteGap
		var from, until, resolved int64
		var raw string
		if err = rows.Scan(&g.ID, &g.LeaseID, &g.LimitID, &from, &until, &g.Amount, &resolved, &g.Actor, &g.Reason, &g.Evidence, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		g.From = time.Unix(from, 0)
		g.Until = time.Unix(until, 0)
		if resolved > 0 {
			g.ResolvedAt = time.Unix(resolved, 0)
		}
		if err = json.Unmarshal([]byte(raw), &g.Allocations); err != nil {
			rows.Close()
			return nil, err
		}
		g.Periods = []time.Time{}
		out = append(out, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		r, e := s.sql.QueryContext(ctx, `SELECT period_start FROM subscriber_gap_periods WHERE gap_id=? ORDER BY period_start`, out[i].ID)
		if e != nil {
			return nil, e
		}
		for r.Next() {
			var v int64
			if e = r.Scan(&v); e != nil {
				r.Close()
				return nil, e
			}
			out[i].Periods = append(out[i].Periods, time.Unix(v, 0))
		}
		e = r.Err()
		r.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// ResolveByteGap records an explicitly approved distribution. It is labelled as
// approved settlement, not a measured per-period traffic sample. The journal,
// prior bounds and lifetime counters are preserved.
func (s *Store) ResolveByteGap(ctx context.Context, actor, id, reason, evidence string, allocations []GapAllocation, at time.Time) error {
	if actor == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(evidence) == "" || len(allocations) == 0 || len(allocations) > 10000 {
		return errors.New("reason, evidence and a complete allocation are required")
	}
	allocations = append([]GapAllocation{}, allocations...)
	for i := range allocations {
		allocations[i].PeriodStart = allocations[i].PeriodStart.UTC()
	}
	sort.Slice(allocations, func(i, j int) bool { return allocations[i].PeriodStart.Before(allocations[j].PeriodStart) })
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lease, limit, sub string
	var amount Quantity
	var resolved int64
	var oldActor, oldReason, oldEvidence, oldAllocations string
	err = tx.QueryRow(`SELECT g.lease_id,g.limit_id,l.subscription_id,g.amount,g.resolved_at,g.actor,g.reason,g.evidence,g.allocations FROM subscriber_byte_gaps g JOIN subscriber_leases l ON l.id=g.lease_id WHERE g.id=?`, id).Scan(&lease, &limit, &sub, &amount, &resolved, &oldActor, &oldReason, &oldEvidence, &oldAllocations)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(allocations)
	if err != nil {
		return err
	}
	if resolved > 0 {
		if oldActor == actor && oldReason == reason && oldEvidence == evidence && oldAllocations == string(raw) {
			return tx.Commit()
		}
		return ErrConflict
	}
	valid := map[int64]bool{}
	rows, err := tx.Query(`SELECT period_start FROM subscriber_gap_periods WHERE gap_id=?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var start int64
		if err = rows.Scan(&start); err != nil {
			rows.Close()
			return err
		}
		valid[start] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	sum := Quantity(0)
	for _, a := range allocations {
		if a.Amount < 0 || a.PeriodStart.Nanosecond() != 0 || !valid[a.PeriodStart.Unix()] || seen[a.PeriodStart.Unix()] {
			return errors.New("allocation must use each eligible period at most once")
		}
		seen[a.PeriodStart.Unix()] = true
		sum, err = add(sum, a.Amount)
		if err != nil {
			return err
		}
	}
	if sum != amount {
		return errors.New("allocated bytes must equal the original session delta")
	}
	// Settlement adds confirmed period usage but must not spend another grant.
	// charge updates only reservations owned by this gap's lease; those grant
	// portions were already consumed when the gap was recorded, so restore them.
	for _, a := range allocations {
		var reserved Quantity
		e := tx.QueryRow(`SELECT amount FROM subscriber_reservations WHERE lease_id=? AND limit_id=? AND period_start=?`, lease, limit, a.PeriodStart.Unix()).Scan(&reserved)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if err = charge(tx, sub, lease, limit, a.PeriodStart, a.Amount, "approved-settlement"); err != nil {
			return err
		}
		if e == nil {
			if _, err = tx.Exec(`UPDATE subscriber_reservations SET amount=? WHERE lease_id=? AND limit_id=? AND period_start=?`, reserved, lease, limit, a.PeriodStart.Unix()); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`UPDATE subscriber_holds SET resolved_at=? WHERE id IN (SELECT hold_id FROM subscriber_gap_periods WHERE gap_id=?) AND resolved_at=0`, at.Unix(), id); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_byte_gaps SET resolved_at=?,actor=?,reason=?,evidence=?,allocations=? WHERE id=?`, at.Unix(), actor, reason, evidence, string(raw), id); err != nil {
		return err
	}
	if err = audit(tx, actor, "accounting.byte-gap.settle", id, reason+"; evidence: "+evidence, at); err != nil {
		return err
	}
	return tx.Commit()
}

type FinalReading struct {
	LeaseID  string   `json:"leaseId"`
	Seconds  Quantity `json:"seconds"`
	Download Quantity `json:"download"`
	Upload   Quantity `json:"upload"`
	Reason   string   `json:"reason"`
	Evidence string   `json:"evidence"`
}

func (s *Store) ReconcileMissingStop(ctx context.Context, actor string, in FinalReading, at time.Time) error {
	if actor == "" || strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.Evidence) == "" || in.Seconds < 0 || in.Seconds > 315360000 || in.Download < 0 || in.Upload < 0 {
		return errors.New("a final cumulative reading, reason and evidence are required")
	}
	if _, err := add(in.Download, in.Upload); err != nil {
		return err
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := Digest(fmt.Sprint("review:", in.LeaseID, ":", in.Seconds, ":", in.Download, ":", in.Upload, ":", actor, ":", in.Reason, ":", in.Evidence))
	var exists int
	if err = tx.QueryRow(`SELECT count(*) FROM subscriber_reconciliations WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return tx.Commit()
	}
	var e Accounting
	var state string
	var started, created int64
	var seconds, down, up Quantity
	err = tx.QueryRow(`SELECT id,router_id,network,device,session_id,state,started_at,created_at,seconds,download,upload FROM subscriber_leases WHERE id=?`, in.LeaseID).Scan(&e.LeaseID, &e.RouterID, &e.Network, &e.Device, &e.SessionID, &state, &started, &created, &seconds, &down, &up)
	if err != nil {
		return err
	}
	if state != "stopped" || in.Seconds < seconds || in.Download < down || in.Upload < up {
		return errors.New("review requires an absent settled session and non-regressing cumulative counters")
	}
	var proof int
	if err = tx.QueryRow(`SELECT (SELECT count(*) FROM subscriber_reconciliations WHERE lease_id=? AND actor='service:subscriber') + (SELECT count(*) FROM subscriber_events WHERE lease_id=? AND status='stop')`, in.LeaseID, in.LeaseID).Scan(&proof); err != nil {
		return err
	}
	if proof == 0 {
		return errors.New("trusted absence or a committed final Stop is required before manual final accounting")
	}
	if started == 0 {
		started = created
	}
	e.Source = "admin"
	e.Status = "stop"
	e.Seconds = in.Seconds
	e.Download = in.Download
	e.Upload = in.Upload
	e.At = time.Unix(started+int64(in.Seconds), 0)
	e, err = preserveReceiptSeconds(tx, e)
	if err != nil {
		return err
	}
	if covered, err := coversPendingFinal(tx, e); err != nil {
		return err
	} else if !covered {
		return errors.New("approved final reading does not cover recorded counters")
	}
	if err = s.accountTx(tx, e); err != nil {
		return err
	}
	if err = resolveReceiptCoverage(tx, e); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO subscriber_reconciliations VALUES(?,?,?,?,?,?,?,?,?)`, id, in.LeaseID, actor, in.Reason, in.Evidence, at.Unix(), in.Seconds, in.Download, in.Upload); err != nil {
		return err
	}
	if err = audit(tx, actor, "accounting.missing-stop.review", in.LeaseID, in.Reason+"; evidence: "+in.Evidence, at); err != nil {
		return err
	}
	return tx.Commit()
}

type LegacyExposure struct {
	ID              string    `json:"id"`
	SubscriptionID  string    `json:"subscriptionId"`
	LimitID         string    `json:"limitId"`
	PeriodStart     time.Time `json:"periodStart"`
	Amount          Quantity  `json:"amount"`
	OriginalQuality string    `json:"originalQuality"`
	ResolvedAt      time.Time `json:"resolvedAt"`
	SettledAmount   Quantity  `json:"settledAmount"`
}

func (s *Store) LegacyExposures(ctx context.Context, account string) ([]LegacyExposure, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT h.id,h.subscription_id,h.limit_id,h.period_start,h.amount,h.original_quality,h.resolved_at,h.settled_amount FROM subscriber_legacy_exposure h JOIN subscriber_subscriptions sub ON sub.id=h.subscription_id WHERE sub.account_id=? ORDER BY (h.resolved_at=0) DESC,period_start DESC,limit_id LIMIT 1000`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegacyExposure{}
	for rows.Next() {
		var v LegacyExposure
		var start, resolved int64
		if err = rows.Scan(&v.ID, &v.SubscriptionID, &v.LimitID, &start, &v.Amount, &v.OriginalQuality, &resolved, &v.SettledAmount); err != nil {
			return nil, err
		}
		v.PeriodStart = time.Unix(start, 0)
		if resolved > 0 {
			v.ResolvedAt = time.Unix(resolved, 0)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ResolveLegacyExposure(ctx context.Context, actor, id, reason, evidence string, amount Quantity, at time.Time) error {
	if actor == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(evidence) == "" || amount < 0 {
		return errors.New("approved amount, actor, reason and evidence are required")
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sub, limit, oldActor, oldReason, oldEvidence string
	var start, resolved int64
	var original, settled Quantity
	err = tx.QueryRow(`SELECT subscription_id,limit_id,period_start,amount,resolved_at,settled_amount,actor,reason,evidence FROM subscriber_legacy_exposure WHERE id=?`, id).Scan(&sub, &limit, &start, &original, &resolved, &settled, &oldActor, &oldReason, &oldEvidence)
	if err != nil {
		return err
	}
	if amount > original {
		return errors.New("approved usage exceeds the original conservative bound")
	}
	if resolved > 0 {
		if oldActor == actor && oldReason == reason && oldEvidence == evidence && settled == amount {
			return tx.Commit()
		}
		return ErrConflict
	}
	var used Quantity
	if err = tx.QueryRow(`SELECT used FROM subscriber_buckets WHERE subscription_id=? AND limit_id=? AND period_start=?`, sub, limit, start).Scan(&used); err != nil {
		return err
	}
	next, err := add(used, amount)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_buckets SET used=?,quality='approved-settlement' WHERE subscription_id=? AND limit_id=? AND period_start=?`, next, sub, limit, start); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_legacy_exposure SET resolved_at=?,actor=?,reason=?,evidence=?,settled_amount=? WHERE id=?`, at.Unix(), actor, reason, evidence, amount, id); err != nil {
		return err
	}
	if err = audit(tx, actor, "accounting.legacy-exposure.settle", id, reason+"; evidence: "+evidence, at); err != nil {
		return err
	}
	return tx.Commit()
}

type SessionReview struct {
	LeaseID          string   `json:"leaseId"`
	Network          string   `json:"network"`
	Device           string   `json:"device"`
	Seconds          Quantity `json:"seconds"`
	Download         Quantity `json:"download"`
	Upload           Quantity `json:"upload"`
	UnresolvedLimits int      `json:"unresolvedLimits"`
}

func (s *Store) SessionReviews(ctx context.Context, account string) ([]SessionReview, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT l.id,l.network,l.device,l.seconds,l.download,l.upload,count(h.id) FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id LEFT JOIN subscriber_holds h ON h.lease_id=l.id AND h.resolved_at=0 AND h.id NOT IN (SELECT hold_id FROM subscriber_gap_periods) WHERE sub.account_id=? AND l.state='stopped' AND (h.id IS NOT NULL OR EXISTS(SELECT 1 FROM subscriber_accounting_receipts r WHERE r.lease_id=l.id AND r.state='pending')) GROUP BY l.id ORDER BY l.created_at LIMIT 1000`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionReview{}
	for rows.Next() {
		var v SessionReview
		if err = rows.Scan(&v.LeaseID, &v.Network, &v.Device, &v.Seconds, &v.Download, &v.Upload, &v.UnresolvedLimits); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
