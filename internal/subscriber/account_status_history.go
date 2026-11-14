package subscriber

import (
	"context"
	"errors"
)

const AccountStatusHistoryPageSize = 50

type AccountStatusEvent struct {
	ID        Quantity `json:"id"`
	Actor     string   `json:"actor"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	CreatedAt int64    `json:"createdAt"`
}

type AccountStatusHistory struct {
	AccountID  string               `json:"accountId"`
	Events     []AccountStatusEvent `json:"events"`
	NextBefore *Quantity            `json:"nextBefore,omitempty"`
}

// StatusHistory reads only status audit entries for an existing account. The
// exclusive ID cursor avoids repeats when newer entries arrive between pages.
// Reasons retain their original representation, including batch JSON envelopes.
func (s *Store) StatusHistory(ctx context.Context, account string, before Quantity) (AccountStatusHistory, error) {
	out := AccountStatusHistory{AccountID: account, Events: []AccountStatusEvent{}}
	if account == "" || before < 0 {
		return out, errors.New("account and nonnegative history cursor required")
	}
	if _, err := s.AccountByID(ctx, account); err != nil {
		return out, err
	}
	query := `SELECT id,actor,action,reason,created_at FROM subscriber_audit WHERE target=? AND action IN ('account.active','account.suspended','account.status.batch')`
	args := []any{account}
	if before != 0 {
		query += ` AND id<?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, AccountStatusHistoryPageSize+1)
	rows, err := s.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var event AccountStatusEvent
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.Reason, &event.CreatedAt); err != nil {
			return AccountStatusHistory{}, err
		}
		out.Events = append(out.Events, event)
	}
	if err := rows.Err(); err != nil {
		return AccountStatusHistory{}, err
	}
	if len(out.Events) > AccountStatusHistoryPageSize {
		out.Events = out.Events[:AccountStatusHistoryPageSize]
		cursor := out.Events[len(out.Events)-1].ID
		out.NextBefore = &cursor
	}
	return out, nil
}
