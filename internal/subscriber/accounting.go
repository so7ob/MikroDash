package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

type Lease struct {
	ID         string    `json:"id"`
	RouterID   string    `json:"routerId"`
	Network    string    `json:"network"`
	Device     string    `json:"device"`
	SessionID  string    `json:"sessionId"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"createdAt"`
	Deadline   time.Time `json:"deadline"`
	ObservedAt time.Time `json:"observedAt"`
	Seconds    Quantity  `json:"seconds"`
	Download   Quantity  `json:"download"`
	Upload     Quantity  `json:"upload"`
}
type Admission struct {
	LeaseID  string   `json:"leaseId"`
	Decision Decision `json:"decision"`
	Download Quantity `json:"download"`
	Upload   Quantity `json:"upload"`
	Total    Quantity `json:"total"`
}

// Reserve serializes admission with accounting using SQLite's immediate
// transaction. A local lease never crosses a hard period or schedule boundary.
// Quotas are shared across all routers, with a sub-budget for each device slot.
func (s *Store) Reserve(ctx context.Context, account, router, network, device, request string, at time.Time) (Admission, error) {
	var a Admission
	if router == "" || network == "" || device == "" || request == "" {
		return a, ErrDenied
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var previous, owner, oldRouter, oldNetwork, oldDevice, oldState, accountStatus string
	var deadline int64
	err = tx.QueryRow(`SELECT l.decision,s.account_id,l.deadline,l.router_id,l.network,l.device,l.state,a.status FROM subscriber_leases l JOIN subscriber_subscriptions s ON s.id=l.subscription_id JOIN subscriber_accounts a ON a.id=s.account_id WHERE l.request_key=?`, request).Scan(&previous, &owner, &deadline, &oldRouter, &oldNetwork, &oldDevice, &oldState, &accountStatus)
	if err == nil {
		if owner != account || at.Unix() >= deadline || oldRouter != router || oldNetwork != network || oldDevice != device || oldState == "stopped" || accountStatus != "active" {
			return a, ErrDenied
		}
		err = json.Unmarshal([]byte(previous), &a)
		return a, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	var status string
	if err = tx.QueryRow(`SELECT status FROM subscriber_accounts WHERE id=?`, account).Scan(&status); err != nil || status != "active" {
		return a, ErrDenied
	}
	sub, err := subscription(tx, account)
	if err != nil {
		return a, err
	}
	if !slices.Contains(sub.Plan.Networks, network) {
		return a, ErrDenied
	}
	var active, duplicate int
	err = tx.QueryRow(`SELECT count(*),coalesce(sum(CASE WHEN device=? THEN 1 ELSE 0 END),0) FROM subscriber_leases WHERE subscription_id=? AND state!='stopped'`, device, sub.ID).Scan(&active, &duplicate)
	if err != nil {
		return a, err
	}
	if active >= sub.Plan.MaxSessions || duplicate > 0 {
		return a, ErrDenied
	}
	u, err := usage(tx, sub.ID)
	if err != nil {
		return a, err
	}
	if sub.ActivatedAt.IsZero() {
		sub.ActivatedAt = at
		sub.ExpiresAt = at.Add(time.Duration(sub.Plan.ValidSeconds) * time.Second)
	}
	a.Decision = Evaluate(sub.Plan, sub.SpeedID, at, sub.ExpiresAt, u)
	if !a.Decision.Allowed {
		return a, ErrDenied
	}
	slots := Quantity(sub.Plan.MaxSessions - active)
	for _, v := range a.Decision.Limits {
		l := v.Limit
		if l.State != "limited" || (l.Action != "deny" && l.Action != "disconnect") {
			continue
		}
		budget := v.Remaining / slots
		if budget <= 0 {
			return a, ErrDenied
		}
		switch l.Kind {
		case "seconds":
			a.Decision.Timeout = min(a.Decision.Timeout, int64(budget))
		case "download":
			a.Download = smallest(a.Download, budget)
		case "upload":
			a.Upload = smallest(a.Upload, budget)
		case "total":
			a.Total = smallest(a.Total, budget)
		}
	}
	// Always delegate bounded traffic as well as bounded time. This caps local
	// exposure on application failure even for an unlimited commercial plan.
	a.Download = smallest(a.Download, Quantity(a.Decision.Timeout)*a.Decision.Down/8)
	a.Upload = smallest(a.Upload, Quantity(a.Decision.Timeout)*a.Decision.Up/8)
	if a.Download <= 0 || a.Upload <= 0 || a.Decision.Timeout <= 0 {
		return a, ErrDenied
	}
	a.LeaseID = ID()
	raw, err := json.Marshal(a)
	if err != nil {
		return a, err
	}
	_, err = tx.Exec(`INSERT INTO subscriber_leases(id,subscription_id,router_id,network,device,request_key,created_at,deadline,decision) VALUES(?,?,?,?,?,?,?,?,?)`, a.LeaseID, sub.ID, router, network, device, request, at.Unix(), at.Unix()+a.Decision.Timeout, string(raw))
	if err != nil {
		return a, err
	}
	for _, v := range a.Decision.Limits {
		l := v.Limit
		if l.State != "limited" || (l.Action != "deny" && l.Action != "disconnect") {
			continue
		}
		budget := v.Remaining / slots
		switch l.Kind {
		case "seconds":
			budget = min(budget, Quantity(a.Decision.Timeout))
		case "download":
			budget = min(budget, a.Download)
		case "upload":
			budget = min(budget, a.Upload)
		case "total":
			total, e := add(a.Download, a.Upload)
			if e != nil {
				return a, e
			}
			budget = min(budget, total)
		}
		if _, err = tx.Exec(`INSERT INTO subscriber_reservations VALUES(?,?,?,?)`, a.LeaseID, l.ID, v.PeriodStart.Unix(), budget); err != nil {
			return a, err
		}
	}
	if _, err = tx.Exec(`UPDATE subscriber_subscriptions SET activated_at=?,expires_at=?,status='active' WHERE id=?`, sub.ActivatedAt.Unix(), sub.ExpiresAt.Unix(), sub.ID); err != nil {
		return a, err
	}
	if err = audit(tx, "service:subscriber", "admission.reserve", a.LeaseID, "bounded network delegation", at); err != nil {
		return a, err
	}
	return a, tx.Commit()
}
func smallest(a, b Quantity) Quantity {
	if a == 0 {
		return b
	}
	return min(a, b)
}

type Accounting struct {
	LeaseID   string
	RouterID  string
	Network   string
	Device    string
	SessionID string
	Status    string
	Seconds   Quantity
	Download  Quantity
	Upload    Quantity
	At        time.Time
}

// Account persists the normalized cumulative reading before the transport may
// acknowledge it. RADIUS is the sole debit source; API observations are never
// debited. Retries and out-of-order samples use independent high-water marks.
func (s *Store) Account(ctx context.Context, e Accounting) error {
	if !slices.Contains([]string{"start", "interim", "stop"}, e.Status) || e.SessionID == "" || e.Seconds < 0 || e.Download < 0 || e.Upload < 0 || e.Seconds > 315360000 {
		return errors.New("invalid accounting event")
	}
	if _, err := add(e.Download, e.Upload); err != nil {
		return err
	}
	key := Digest(fmt.Sprintf("%s:%s:%s:%d:%d:%d", e.LeaseID, e.SessionID, e.Status, e.Seconds, e.Download, e.Upload))
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var subID, router, network, device, session, state, raw string
	var oldSec, oldDown, oldUp Quantity
	var started, created int64
	err = tx.QueryRow(`SELECT l.subscription_id,l.router_id,l.network,l.device,l.session_id,l.state,l.seconds,l.download,l.upload,l.started_at,l.created_at,p.definition FROM subscriber_leases l JOIN subscriber_subscriptions s ON s.id=l.subscription_id JOIN subscriber_plans p ON p.id=s.plan_id AND p.version=s.plan_version WHERE l.id=?`, e.LeaseID).Scan(&subID, &router, &network, &device, &session, &state, &oldSec, &oldDown, &oldUp, &started, &created, &raw)
	if err != nil {
		return err
	}
	if router != e.RouterID || network != e.Network || device != e.Device || (session != "" && session != e.SessionID) {
		return ErrDenied
	}
	var exists int
	if err = tx.QueryRow(`SELECT count(*) FROM subscriber_events WHERE event_key=?`, key).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return tx.Commit()
	}
	var p Plan
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return err
	}
	if started == 0 {
		started = e.At.Unix() - int64(e.Seconds)
		if started < created-5 || started > created+60 {
			return errors.New("accounting start does not match authorization")
		}
	}
	sec := max(oldSec, e.Seconds)
	down := max(oldDown, e.Download)
	up := max(oldUp, e.Upload)
	from := time.Unix(started+int64(oldSec), 0)
	until := time.Unix(started+int64(sec), 0)
	quality := "fresh"
	if e.Seconds < oldSec && (e.Download > oldDown || e.Upload > oldUp) {
		quality = "reconciling"
	}
	for _, po := range p.Policies {
		for _, l := range po.Limits {
			if l.State == "disabled" {
				continue
			}
			var delta Quantity
			switch l.Kind {
			case "seconds":
				delta = sec - oldSec
			case "download":
				delta = down - oldDown
			case "upload":
				delta = up - oldUp
			case "total":
				delta, err = add(down-oldDown, up-oldUp)
				if err != nil {
					return err
				}
			}
			if delta == 0 {
				continue
			}
			if l.Kind == "seconds" {
				for cursor := from; cursor.Before(until); {
					start, end, berr := l.Period.Bounds(cursor)
					if berr != nil {
						return berr
					}
					edge := until
					if end.Before(edge) {
						edge = end
					}
					n := Quantity(edge.Sub(cursor) / time.Second)
					if n <= 0 {
						return errors.New("invalid accounting period")
					}
					if err = charge(tx, subID, e.LeaseID, l.ID, start, n, quality); err != nil {
						return err
					}
					cursor = edge
				}
			} else {
				// The bytes inside an unsampled boundary gap are unknowable. Charge the
				// full delta to every touched period, conservatively, and label it. The
				// lifetime counter remains exact and is never this sum of period buckets.
				endTime := until
				if !endTime.After(from) {
					endTime = e.At
				}
				first, _, berr := l.Period.Bounds(from)
				if berr != nil {
					return berr
				}
				last, _, berr := l.Period.Bounds(endTime)
				if berr != nil {
					return berr
				}
				q := quality
				if !first.Equal(last) {
					q = "boundary-conservative"
					quality = q
				}
				cursor := first
				count := 0
				for !cursor.After(last) {
					count++
					if count > 10000 {
						return errors.New("accounting gap requires reconciliation")
					}
					if err = charge(tx, subID, e.LeaseID, l.ID, cursor, delta, q); err != nil {
						return err
					}
					_, next, berr := l.Period.Bounds(cursor)
					if berr != nil {
						return berr
					}
					cursor = next
				}
			}
		}
	}
	if e.Status == "stop" {
		state = "stopped"
	} else if state != "stopped" {
		state = "active"
	}
	_, err = tx.Exec(`UPDATE subscriber_leases SET session_id=?,state=?,started_at=?,seconds=?,download=?,upload=?,observed_at=max(observed_at,?) WHERE id=?`, e.SessionID, state, started, sec, down, up, e.At.Unix(), e.LeaseID)
	if err != nil {
		return err
	}
	if state == "stopped" {
		if _, err = tx.Exec(`DELETE FROM subscriber_reservations WHERE lease_id=?`, e.LeaseID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO subscriber_events VALUES(?,?,?,?,?,?,?,?)`, key, e.LeaseID, e.Status, e.Seconds, e.Download, e.Upload, time.Now().Unix(), quality)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func charge(tx *sql.Tx, sub, lease, limit string, start time.Time, delta Quantity, quality string) error {
	var old Quantity
	err := tx.QueryRow(`SELECT used FROM subscriber_buckets WHERE subscription_id=? AND limit_id=? AND period_start=?`, sub, limit, start.Unix()).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	next, err := add(old, delta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO subscriber_buckets VALUES(?,?,?,?,?) ON CONFLICT(subscription_id,limit_id,period_start) DO UPDATE SET used=excluded.used,quality=CASE WHEN excluded.quality='fresh' THEN subscriber_buckets.quality ELSE excluded.quality END`, sub, limit, start.Unix(), next, quality)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE subscriber_reservations SET amount=max(0,amount-?) WHERE lease_id=? AND limit_id=? AND period_start=?`, delta, lease, limit, start.Unix())
	return err
}
