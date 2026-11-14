package subscriber

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// AccountAuditIndexes is additive migration 47. Generic target lookup retains
// all old audit rows; migration 42 already indexes subscription account ownership.
const AccountAuditIndexes = `
CREATE INDEX IF NOT EXISTS subscriber_audit_target ON subscriber_audit(target,id DESC);
`
const AccountAuditPageSize = 50
const AccountAuditEvidenceRunes = 8192

var ErrAuditQuery = errors.New("invalid account audit query")

type AccountAuditQuery struct {
	AccountID string
	Before    Quantity
	Category  string
	From      string
	Until     string
}
type AccountAuditEvent struct {
	ID             Quantity `json:"id"`
	Actor          string   `json:"actor"`
	Action         string   `json:"action"`
	Target         string   `json:"target"`
	SubscriptionID string   `json:"subscriptionId,omitempty"`
	PlanID         string   `json:"planId,omitempty"`
	PlanVersion    int      `json:"planVersion,omitempty"`
	Reason         string   `json:"reason"`
	EvidenceState  string   `json:"evidenceState"`
	CreatedAt      int64    `json:"createdAt"`
}
type AccountAuditPage struct {
	AccountID  string              `json:"accountId"`
	Category   string              `json:"category"`
	From       string              `json:"from"`
	Until      string              `json:"until"`
	Events     []AccountAuditEvent `json:"events"`
	NextBefore *Quantity           `json:"nextBefore,omitempty"`
}

var auditCategories = map[string]string{
	"all":           "",
	"profile":       `a.action IN ('account.create','account.name.edit')`,
	"status":        `a.action IN ('account.active','account.suspended','account.status.batch')`,
	"credentials":   `a.action='account.credential.reset'`,
	"subscriptions": `a.action='subscription.assign'`,
	"speed":         `a.action='speed.request'`,
}

func auditDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil || value.Format("2006-01-02") != raw || value.Year() < 1 {
		return time.Time{}, ErrAuditQuery
	}
	return value, nil
}

// auditStatement binds all input values and joins only supported action/identity
// pairs. An ID reused in another entity domain cannot change account ownership.
func auditStatement(q AccountAuditQuery) (string, []any, error) {
	if q.Category == "" {
		q.Category = "all"
	}
	category, ok := auditCategories[q.Category]
	from, errFrom := auditDate(q.From)
	until, errUntil := auditDate(q.Until)
	if q.AccountID == "" || q.Before < 0 || !ok || errFrom != nil || errUntil != nil || q.From != "" && q.Until != "" && from.After(until) {
		return "", nil, ErrAuditQuery
	}
	filter := ""
	tail := []any{}
	if q.Before != 0 {
		filter += " AND a.id<?"
		tail = append(tail, q.Before)
	}
	if q.From != "" {
		filter += " AND a.created_at>=?"
		tail = append(tail, from.Unix())
	}
	if q.Until != "" {
		filter += " AND a.created_at<?"
		tail = append(tail, until.AddDate(0, 0, 1).Unix())
	}
	if category != "" {
		filter += " AND " + category
	}
	cols := `a.id,a.actor,a.action,a.target,substr(CAST(a.reason AS BLOB),1,32772) AS reason,a.created_at`
	query := `SELECT * FROM (SELECT * FROM (SELECT ` + cols + `,'' AS subscription_id,'' AS plan_id,0 AS plan_version FROM subscriber_audit a WHERE a.target=? AND a.action IN ('account.create','account.name.edit','account.active','account.suspended','account.status.batch','account.credential.reset')` + filter + ` ORDER BY a.id DESC LIMIT ?) UNION ALL SELECT * FROM (SELECT ` + cols + `,s.id AS subscription_id,s.plan_id,s.plan_version FROM subscriber_subscriptions s JOIN subscriber_audit a ON a.target=s.id WHERE s.account_id=? AND a.action IN ('subscription.assign','speed.request')` + filter + ` ORDER BY a.id DESC LIMIT ?)) ORDER BY id DESC LIMIT ?`
	args := append([]any{q.AccountID}, tail...)
	args = append(args, AccountAuditPageSize+1, q.AccountID)
	args = append(args, tail...)
	args = append(args, AccountAuditPageSize+1, AccountAuditPageSize+1)
	return query, args, nil
}

func auditEvidence(event *AccountAuditEvent) {
	event.EvidenceState = "complete"
	// Read a bounded byte prefix so embedded NULs remain evidence. If its
	// four-byte-per-scalar cap bisects a final UTF-8 scalar, trim that suffix
	// before the ordinary character bound. Invalid interior data stays hidden.
	if len(event.Reason) == (AccountAuditEvidenceRunes+1)*utf8.UTFMax && !utf8.ValidString(event.Reason) {
		original := event.Reason
		for trim := 1; trim < utf8.UTFMax; trim++ {
			candidate := original[:len(original)-trim]
			if utf8.ValidString(candidate) {
				event.Reason = candidate
				event.EvidenceState = "truncated"
				break
			}
		}
	}
	if !utf8.ValidString(event.Reason) {
		event.Reason = ""
		event.EvidenceState = "unavailable"
		return
	}
	text := []rune(event.Reason)
	if len(text) > AccountAuditEvidenceRunes {
		event.Reason = string(text[:AccountAuditEvidenceRunes])
		event.EvidenceState = "truncated"
	}
	if event.Action != "account.credential.reset" {
		return
	}
	// Original credential evidence is safe only for the current strict envelope.
	// Unknown fields, malformed revisions and incomplete historical envelopes do
	// not get a raw fallback that could reveal legacy credential material.
	keys := json.NewDecoder(strings.NewReader(event.Reason))
	start, keyErr := keys.Token()
	seen := map[string]bool{}
	if keyErr != nil || start != json.Delim('{') {
		event.Reason = ""
		event.EvidenceState = "unavailable"
		return
	}
	for keys.More() {
		token, e := keys.Token()
		key, ok := token.(string)
		var value json.RawMessage
		if e != nil || !ok || seen[key] || key != "reason" && key != "beforeRevision" && key != "afterRevision" || keys.Decode(&value) != nil {
			event.Reason = ""
			event.EvidenceState = "unavailable"
			return
		}
		seen[key] = true
	}
	var envelope struct {
		Reason *string   `json:"reason"`
		Before *Quantity `json:"beforeRevision"`
		After  *Quantity `json:"afterRevision"`
	}
	decoder := json.NewDecoder(strings.NewReader(event.Reason))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&envelope)
	var extra any
	if err != nil || decoder.Decode(&extra) != io.EOF || event.EvidenceState != "complete" || envelope.Reason == nil || strings.TrimSpace(*envelope.Reason) == "" || len(*envelope.Reason) > 2048 || envelope.Before == nil || envelope.After == nil || *envelope.Before < 0 || *envelope.Before == math.MaxInt64 || *envelope.After != *envelope.Before+1 {
		event.Reason = ""
		event.EvidenceState = "unavailable"
	}
}

func (s *Store) AccountAudit(ctx context.Context, q AccountAuditQuery) (AccountAuditPage, error) {
	if q.Category == "" {
		q.Category = "all"
	}
	out := AccountAuditPage{AccountID: q.AccountID, Category: q.Category, From: q.From, Until: q.Until, Events: []AccountAuditEvent{}}
	query, args, err := auditStatement(q)
	if err != nil {
		return out, err
	}
	// Account existence is checked without reading current credentials or using
	// current mutable profile values as historical event evidence.
	var exists int
	if err = s.sql.QueryRowContext(ctx, `SELECT 1 FROM subscriber_accounts WHERE id=?`, q.AccountID).Scan(&exists); err != nil {
		return out, err
	}
	rows, err := s.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var event AccountAuditEvent
		if err = rows.Scan(&event.ID, &event.Actor, &event.Action, &event.Target, &event.Reason, &event.CreatedAt, &event.SubscriptionID, &event.PlanID, &event.PlanVersion); err != nil {
			return out, err
		}
		auditEvidence(&event)
		out.Events = append(out.Events, event)
	}
	if err = rows.Err(); err != nil {
		return AccountAuditPage{}, err
	}
	if len(out.Events) > AccountAuditPageSize {
		out.Events = out.Events[:AccountAuditPageSize]
		cursor := out.Events[len(out.Events)-1].ID
		out.NextBefore = &cursor
	}
	return out, nil
}
