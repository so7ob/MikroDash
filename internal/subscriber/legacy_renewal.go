package subscriber

import (
	"context"
	"time"
)

// retireLegacyRenewal fences pre-uptime renewal intents. A packet may have
// reached the NAS before a crash; retain its maximum remaining time exposure.
// No network command is sent and no used balance or subscription is reset.
func (s *Store) retireLegacyRenewal(ctx context.Context, o Operation, at time.Time) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if o.Attempts > 0 {
		var seconds Quantity
		if err = tx.QueryRow(`SELECT seconds FROM subscriber_leases WHERE id=?`, o.LeaseID).Scan(&seconds); err != nil {
			return err
		}
		remaining := max(Quantity(0), Quantity(o.TargetDeadline.Unix()-o.Request.StartedAt.Unix())-seconds)
		for _, view := range o.Request.Admission.Decision.Limits {
			l := view.Limit
			if l.Kind != "seconds" || l.State != "limited" || (l.Action != "deny" && l.Action != "disconnect") {
				continue
			}
			if _, err = tx.Exec(`UPDATE subscriber_reservations SET amount=max(amount,?) WHERE lease_id=? AND limit_id=? AND period_start=?`, remaining, o.LeaseID, l.ID, view.PeriodStart.Unix()); err != nil {
				return err
			}
		}
	}
	result, err := tx.Exec(`UPDATE subscriber_operations SET state='failed',updated_at=?,error='legacy renewal requires a fresh NAS uptime budget' WHERE id=? AND state IN ('requested','applying','acknowledged')`, at.Unix(), o.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		if err = audit(tx, "service:subscriber", "session.renew.legacy-retired", o.LeaseID, "legacy intent refused; maximum attempted exposure retained", at); err != nil {
			return err
		}
	}
	return tx.Commit()
}
