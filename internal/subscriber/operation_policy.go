package subscriber

import (
	"context"
	"time"
)

// evaluateSpan checks each policy period a persisted grant can still touch.
// Reservations are handled by its transaction, not counted as consumed quota.
func evaluateSpan(plan Plan, speed string, at, until, expires time.Time, usage map[string]Usage) Decision {
	d := evaluate(plan, speed, at, expires, usage, false)
	if !d.Allowed {
		return d
	}
	end := max(at.Unix()+1, until.Unix())
	if end-at.Unix() > 60 {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "authorization-clock-context-invalid")
		return d
	}
	for cursor := at.Unix(); cursor < end; {
		point := evaluate(plan, speed, time.Unix(cursor, 0), expires, usage, false)
		if !point.Allowed {
			point.Reasons = append(point.Reasons, "authorization-period-unavailable")
			return point
		}
		d.Down, d.Up = min(d.Down, point.Down), min(d.Up, point.Up)
		edge := min(end, cursor+point.Timeout)
		for _, view := range point.Limits {
			if view.Limit.State != "unlimited" && view.Limit.State != "disabled" {
				edge = min(edge, view.NextReset.Unix())
			}
		}
		if edge <= cursor {
			d.Allowed = false
			d.Reasons = append(d.Reasons, "authorization-period-invalid")
			return d
		}
		cursor = edge
	}
	for _, policy := range plan.Policies {
		for _, limit := range policy.Limits {
			if !hardLimit(limit) || limit.Kind == "seconds" {
				continue
			}
			start, _, err := limit.Period.Bounds(time.Unix(end, 0))
			budget, known := limitBudget(limit, start, usage, 1)
			if err != nil || !known || budget <= 0 {
				d.Allowed = false
				d.Reasons = append(d.Reasons, "authorization-endpoint-unavailable:"+limit.ID)
			}
		}
	}
	d.Timeout = end - at.Unix()
	return d
}

func (s *Store) operationPolicy(ctx context.Context, account string, at, until time.Time) (Decision, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRow(`SELECT status FROM subscriber_accounts WHERE id=?`, account).Scan(&status); err != nil {
		return Decision{}, err
	}
	if status != "active" {
		return Decision{Reasons: []string{"account-suspended"}}, tx.Commit()
	}
	pending, err := pendingAccounting(tx, account)
	if err != nil {
		return Decision{}, err
	}
	if pending > 0 {
		return Decision{Reasons: []string{"accounting-reconciliation-required"}}, tx.Commit()
	}
	sub, err := subscription(tx, account)
	if err != nil {
		return Decision{}, err
	}
	u, err := usage(tx, sub.ID)
	if err != nil {
		return Decision{}, err
	}
	for key, value := range u {
		value.Reserved = value.Provisional
		u[key] = value
	}
	d := evaluateSpan(sub.Plan, sub.SpeedID, at, until, sub.ExpiresAt, u)
	return d, tx.Commit()
}
