package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RecordTicketOutcome requires the same authenticated NAS/request that redeemed
// the ticket. The first committed verdict stays historical; a retry cannot
// redirect the flow to another device or lease. Audit and metadata are atomic.
func (s *Store) RecordTicketOutcome(ctx context.Context, token string, b TicketBinding, request string, outcome TicketOutcome, at time.Time) error {
	if request == "" || len(outcome.Reasons) > 64 {
		return ErrDenied
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var account, username, accountStatus, network, device, prior, router, state, oldLease string
	var expires int64
	err = tx.QueryRow(`SELECT a.id,a.username,a.status,t.network,t.device,t.request_key,t.expires_at,f.router_id,f.state,coalesce(f.lease_id,'') FROM subscriber_connection_flows f JOIN subscriber_tickets t ON t.token_hash=f.token_hash JOIN subscriber_accounts a ON a.id=t.account_id WHERE f.token_hash=?`, Digest(token)).Scan(&account, &username, &accountStatus, &network, &device, &prior, &expires, &router, &state, &oldLease)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if request != prior || router != b.RouterID || network != b.Network || device != b.Device || username != b.Username {
		return ErrDenied
	}
	if outcome.LeaseID != "" && accountStatus != "active" {
		return ErrDenied
	}
	if state == "authorized" {
		if outcome.LeaseID != "" && outcome.LeaseID != oldLease {
			return ErrConflict
		}
		return tx.Commit()
	}
	if state == "rejected" {
		if outcome.LeaseID != "" {
			return ErrConflict
		}
		return tx.Commit()
	}
	if state != "authorizing" || at.Unix() >= expires {
		return ErrDenied
	}
	next := "rejected"
	reasons := append([]string{}, outcome.Reasons...)
	var lease any
	if outcome.LeaseID != "" {
		var matches int
		err = tx.QueryRow(`SELECT count(*) FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE l.id=? AND sub.account_id=? AND l.router_id=? AND l.network=? AND l.device=? AND l.request_key=? AND l.state!='stopped'`, outcome.LeaseID, account, router, network, device, request).Scan(&matches)
		if err != nil {
			return err
		}
		if matches != 1 {
			return ErrDenied
		}
		next = "authorized"
		lease = outcome.LeaseID
		reasons = []string{}
	} else if len(reasons) == 0 {
		reasons = []string{"authorization-denied"}
	}
	for _, reason := range reasons {
		if strings.TrimSpace(reason) == "" || len(reason) > 1024 {
			return ErrDenied
		}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_connection_flows SET state=?,lease_id=?,reasons=?,updated_at=? WHERE token_hash=?`, next, lease, string(raw), at.Unix(), Digest(token)); err != nil {
		return err
	}
	if err = audit(tx, "service:subscriber", "authentication.ticket."+next, Digest(token), strings.Join(append([]string{"NAS authorization verdict recorded"}, reasons...), "; "), at); err != nil {
		return err
	}
	return tx.Commit()
}
