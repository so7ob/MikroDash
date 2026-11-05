package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Capabilities must describe a recorded probe for this NAS/version. Documentation
// alone does not prove timeout semantics, API counter direction or queue identity.
type Capabilities struct {
	Version      string `json:"version"`
	Evidence     string `json:"evidence"`
	TimeoutMode  string `json:"timeoutMode"` // total or remaining; empty is unverified
	ByteMode     string `json:"byteMode"`    // total or remaining; empty is unverified
	CoARate      bool   `json:"coaRate"`
	CoATotal     bool   `json:"coaTotal"`
	CoAGigawords bool   `json:"coaGigawords"`
	CounterMode  string `json:"counterMode"` // input-upload or output-upload
}

func (c Capabilities) Recorded() bool { return c.Version != "" && c.Evidence != "" }
func (c Capabilities) CanRenew() bool {
	return c.Recorded() && (c.CounterMode == "input-upload" || c.CounterMode == "output-upload") && c.TimeoutMode == "total" && c.ByteMode == "total"
}

const renewalTimeBasis = "nas-uptime-v1"

type OperationRequest struct {
	TimeBasis       string    `json:"timeBasis"`
	GrantedSeconds  Quantity  `json:"grantedSeconds"`
	RequireRate     bool      `json:"requireRate"`
	SpeedID         string    `json:"speedId"`
	Admission       Admission `json:"admission"`
	Username        string    `json:"username"`
	SessionID       string    `json:"sessionId"`
	Device          string    `json:"device"`
	RouterID        string    `json:"routerId"`
	Network         string    `json:"network"`
	StartedAt       time.Time `json:"startedAt"`
	DownloadCeiling Quantity  `json:"downloadCeiling"`
	UploadCeiling   Quantity  `json:"uploadCeiling"`
	TotalCeiling    Quantity  `json:"totalCeiling"`
}
type Operation struct {
	ID               string           `json:"id"`
	LeaseID          string           `json:"leaseId"`
	Kind             string           `json:"kind"`
	State            string           `json:"state"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	Attempts         int              `json:"attempts"`
	TargetDeadline   time.Time        `json:"targetDeadline"`
	PreviousDeadline time.Time        `json:"previousDeadline"`
	Request          OperationRequest `json:"request"`
	Packet           string           `json:"-"`
	Error            string           `json:"error"`
}

func scanOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var o Operation
	var created, updated, target, previous int64
	var raw string
	err := row.Scan(&o.ID, &o.LeaseID, &o.Kind, &o.State, &created, &updated, &o.Attempts, &target, &previous, &raw, &o.Packet, &o.Error)
	if err != nil {
		return o, err
	}
	o.CreatedAt = time.Unix(created, 0)
	o.UpdatedAt = time.Unix(updated, 0)
	o.TargetDeadline = time.Unix(target, 0)
	o.PreviousDeadline = time.Unix(previous, 0)
	err = json.Unmarshal([]byte(raw), &o.Request)
	return o, err
}

const operationColumns = `id,lease_id,kind,state,created_at,updated_at,attempts,target_deadline,previous_deadline,request,packet,error`

func (s *Store) Operations(ctx context.Context, pending bool) ([]Operation, error) {
	query := `SELECT ` + operationColumns + ` FROM subscriber_operations`
	if pending {
		query += ` WHERE state IN ('requested','applying','acknowledged')`
	}
	query += ` ORDER BY created_at,id LIMIT 1000`
	rows, err := s.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		o, e := scanOperation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PrepareOperation reserves a new delegation and its durable intent in one
// immediate transaction. Own reservations are returned to available capacity
// exactly once; other devices' reservations remain unavailable.
func (s *Store) PrepareOperation(ctx context.Context, lease, kind string, at time.Time) (Operation, error) {
	var o Operation
	if kind != "renew" && kind != "rate" {
		return o, errors.New("invalid session operation")
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	existing, e := scanOperation(tx.QueryRow(`SELECT `+operationColumns+` FROM subscriber_operations WHERE lease_id=? AND state IN ('requested','applying','acknowledged')`, lease))
	if e == nil {
		return existing, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return o, e
	}
	var account, state, status, oldDecision string
	var deadline, started int64
	var seconds, down, up Quantity
	q := OperationRequest{}
	err = tx.QueryRow(`SELECT sub.account_id,a.username,a.status,l.router_id,l.network,l.device,l.session_id,l.state,l.deadline,l.started_at,l.seconds,l.download,l.upload,l.decision FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id JOIN subscriber_accounts a ON a.id=sub.account_id WHERE l.id=?`, lease).Scan(&account, &q.Username, &status, &q.RouterID, &q.Network, &q.Device, &q.SessionID, &state, &deadline, &started, &seconds, &down, &up, &oldDecision)
	if err != nil {
		return o, err
	}
	if state != "active" || q.SessionID == "" || status != "active" || deadline <= at.Unix() {
		return o, ErrDenied
	}
	if pending, err := pendingAccounting(tx, account); err != nil {
		return o, err
	} else if pending > 0 {
		return o, ErrDenied
	}
	sub, err := subscription(tx, account)
	if err != nil {
		return o, err
	}
	if err = retainLegacyExposure(tx, account, at); err != nil {
		return o, err
	}
	u, err := usage(tx, sub.ID)
	if err != nil {
		return o, err
	}
	rows, err := tx.Query(`SELECT limit_id,period_start,amount FROM subscriber_reservations WHERE lease_id=?`, lease)
	if err != nil {
		return o, err
	}
	own := map[string]Quantity{}
	for rows.Next() {
		var id string
		var start int64
		var amount Quantity
		if err = rows.Scan(&id, &start, &amount); err != nil {
			rows.Close()
			return o, err
		}
		key := Key(id, time.Unix(start, 0))
		v := u[key]
		v.Reserved = max(0, v.Reserved-amount)
		u[key] = v
		own[key] = amount
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return o, err
	}
	q.SpeedID = sub.SpeedID
	if kind == "rate" {
		for key, v := range u {
			v.Reserved = 0
			u[key] = v
		}
	}
	q.Admission = Admission{LeaseID: lease, Decision: evaluate(sub.Plan, sub.SpeedID, at, sub.ExpiresAt, u, false)}
	if kind == "rate" {
		q.Admission.Decision = evaluateSpan(sub.Plan, sub.SpeedID, at, time.Unix(deadline, 0), sub.ExpiresAt, u)
	}
	if !q.Admission.Decision.Allowed {
		return o, ErrDenied
	}
	if kind == "rate" {
		last, err := scanOperation(tx.QueryRow(`SELECT `+operationColumns+` FROM subscriber_operations WHERE lease_id=? AND kind='rate' ORDER BY created_at DESC,id DESC LIMIT 1`, lease))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return o, err
		}
		if err == nil && (last.State == "failed" || last.State == "pending-reconnect") && last.Request.SpeedID == q.SpeedID && last.Request.Admission.Decision.Down == q.Admission.Decision.Down && last.Request.Admission.Decision.Up == q.Admission.Decision.Up {
			return o, ErrDenied
		}
	}
	q.StartedAt = time.Unix(started, 0)
	target := deadline
	if kind == "renew" {
		target, err = s.prepareRenewal(tx, sub, lease, at, started, seconds, down, up, own, u, &q)
		if err != nil {
			return o, err
		}
	}
	raw, err := json.Marshal(q)
	if err != nil {
		return o, err
	}
	o = Operation{ID: ID(), LeaseID: lease, Kind: kind, State: "requested", CreatedAt: at, UpdatedAt: at, TargetDeadline: time.Unix(target, 0), PreviousDeadline: time.Unix(deadline, 0), Request: q}
	if _, err = tx.Exec(`INSERT INTO subscriber_operations(id,lease_id,kind,state,created_at,updated_at,target_deadline,previous_deadline,request) VALUES(?,?,?,?,?,?,?,?,?)`, o.ID, lease, kind, o.State, at.Unix(), at.Unix(), target, deadline, string(raw)); err != nil {
		return o, err
	}
	if err = audit(tx, "service:subscriber", "session."+kind+".requested", lease, "operation "+o.ID, at); err != nil {
		return o, err
	}
	return o, tx.Commit()
}

func (s *Store) transitionOperation(ctx context.Context, o Operation, state, message, packet string, at time.Time) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	increment := 0
	if state == "applying" {
		increment = 1
	}
	r, err := tx.Exec(`UPDATE subscriber_operations SET state=?,updated_at=?,attempts=attempts+?,error=?,packet=CASE WHEN ?='' THEN packet ELSE ? END WHERE id=? AND state IN ('requested','applying','acknowledged')`, state, at.Unix(), increment, message, packet, packet, o.ID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if err = audit(tx, "service:subscriber", "session."+o.Kind+"."+state, o.LeaseID, "operation "+o.ID+": "+message, at); err != nil {
		return err
	}
	if o.Kind == "rate" {
		visible := state
		if state == "acknowledged" {
			visible = "applying"
		}
		if state != "verified" {
			if _, err = tx.Exec(`UPDATE subscriber_subscriptions SET speed_state=? WHERE id=(SELECT subscription_id FROM subscriber_leases WHERE id=?) AND speed_id=? AND speed_changed_at<=?`, visible, o.LeaseID, o.Request.SpeedID, o.CreatedAt.Unix()); err != nil {
				return err
			}
		}
	}
	if state == "verified" {
		if o.Kind == "renew" {
			if _, err = tx.Exec(`UPDATE subscriber_leases SET deadline=? WHERE id=? AND session_id=? AND state='active'`, o.TargetDeadline.Unix(), o.LeaseID, o.Request.SessionID); err != nil {
				return err
			}
		}
		// A newer speed choice must not be marked verified by an older operation.
		if o.Kind == "rate" {
			if _, err = tx.Exec(`UPDATE subscriber_subscriptions SET speed_state='verified' WHERE id=(SELECT subscription_id FROM subscriber_leases WHERE id=?) AND speed_changed_at<=? AND speed_id=? AND NOT EXISTS (SELECT 1 FROM subscriber_leases l WHERE l.subscription_id=subscriber_subscriptions.id AND l.state!='stopped' AND NOT EXISTS (SELECT 1 FROM subscriber_operations op WHERE op.lease_id=l.id AND op.kind='rate' AND op.state='verified' AND op.created_at>=subscriber_subscriptions.speed_changed_at AND json_extract(op.request,'$.speedId')=subscriber_subscriptions.speed_id))`, o.LeaseID, o.CreatedAt.Unix(), o.Request.SpeedID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
