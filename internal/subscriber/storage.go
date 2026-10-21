package subscriber

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"mikrodash/internal/store"
)

const Schema = `
CREATE TABLE IF NOT EXISTS subscriber_accounts (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
 salt TEXT NOT NULL, password_hash TEXT NOT NULL, voucher_hash TEXT UNIQUE,
 status TEXT NOT NULL CHECK(status IN ('active','suspended')), created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS subscriber_plans (
 id TEXT NOT NULL, version INTEGER NOT NULL, definition TEXT NOT NULL,
 PRIMARY KEY(id,version)
);
CREATE TABLE IF NOT EXISTS subscriber_subscriptions (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES subscriber_accounts(id),
 plan_id TEXT NOT NULL, plan_version INTEGER NOT NULL, assigned_at INTEGER NOT NULL,
 activated_at INTEGER NOT NULL DEFAULT 0, expires_at INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('pending','active','superseded')),
 speed_id TEXT NOT NULL, speed_state TEXT NOT NULL DEFAULT 'pending-reconnect', speed_changed_at INTEGER NOT NULL DEFAULT 0,
 FOREIGN KEY(plan_id,plan_version) REFERENCES subscriber_plans(id,version)
);
CREATE UNIQUE INDEX IF NOT EXISTS subscriber_current ON subscriber_subscriptions(account_id) WHERE status != 'superseded';
CREATE TABLE IF NOT EXISTS subscriber_leases (
 id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL REFERENCES subscriber_subscriptions(id),
 router_id TEXT NOT NULL, network TEXT NOT NULL, device TEXT NOT NULL, request_key TEXT NOT NULL UNIQUE,
 session_id TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'reserved',
 created_at INTEGER NOT NULL, deadline INTEGER NOT NULL, started_at INTEGER NOT NULL DEFAULT 0,
 seconds INTEGER NOT NULL DEFAULT 0, download INTEGER NOT NULL DEFAULT 0, upload INTEGER NOT NULL DEFAULT 0,
 observed_at INTEGER NOT NULL DEFAULT 0, decision TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS subscriber_lease_sub ON subscriber_leases(subscription_id,state);
CREATE TABLE IF NOT EXISTS subscriber_buckets (
 subscription_id TEXT NOT NULL REFERENCES subscriber_subscriptions(id), limit_id TEXT NOT NULL,
 period_start INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0 CHECK(used>=0),
 quality TEXT NOT NULL DEFAULT 'fresh', PRIMARY KEY(subscription_id,limit_id,period_start)
);
CREATE TABLE IF NOT EXISTS subscriber_reservations (
 lease_id TEXT NOT NULL REFERENCES subscriber_leases(id), limit_id TEXT NOT NULL,
 period_start INTEGER NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0),
 PRIMARY KEY(lease_id,limit_id,period_start)
);
CREATE TABLE IF NOT EXISTS subscriber_events (
 event_key TEXT PRIMARY KEY, lease_id TEXT NOT NULL REFERENCES subscriber_leases(id),
 status TEXT NOT NULL, seconds INTEGER NOT NULL, download INTEGER NOT NULL, upload INTEGER NOT NULL,
 received_at INTEGER NOT NULL, quality TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS subscriber_tickets (
 token_hash TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES subscriber_accounts(id),
 network TEXT NOT NULL, device TEXT NOT NULL, expires_at INTEGER NOT NULL, request_key TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS subscriber_audit (
 id INTEGER PRIMARY KEY, actor TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL,
 reason TEXT NOT NULL, created_at INTEGER NOT NULL
);
` + SessionSchema + ReconciliationSchema + ReceiptSchema + FinalityIndexes + ConnectionSchema + AccountStatusIndexes

type Store struct{ sql *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{sql: db} }
func ID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

var ErrDenied = errors.New("subscriber access denied")
var ErrConflict = errors.New("subscriber state conflict")

func audit(tx *sql.Tx, actor, action, target, reason string, now time.Time) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return errors.New("actor and reason required")
	}
	// SQLite may choose a smaller random rowid after MaxInt64. Review tokens
	// require monotonic committed identities, so exhaustion must roll back writes.
	var previous int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(id),0) FROM subscriber_audit`).Scan(&previous); err != nil {
		return err
	}
	if previous < 0 || previous == math.MaxInt64 {
		return errors.New("subscriber audit revision cannot advance within the supported range")
	}
	_, err := tx.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,?,?,?,?)`, previous+1, actor, action, target, reason, now.Unix())
	return err
}

type Account struct {
	ID             string   `json:"id"`
	Username       string   `json:"username"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	StatusRevision Quantity `json:"statusRevision"`
}

func (s *Store) CreateAccount(ctx context.Context, actor, username, name, password, reason string, voucherLength int) (Account, string, error) {
	a := Account{ID: ID(), Username: username, Name: name, Status: "active"}
	code := ""
	if len(username) == 0 || len(username) > 128 || len(name) > 256 || strings.ContainsAny(username, "\x00\r\n") {
		return a, code, errors.New("invalid subscriber name")
	}
	if voucherLength != 0 {
		if voucherLength < 16 || voucherLength > 64 {
			return a, code, errors.New("voucher length must be 16..64")
		}
		code = ID() + ID()
		code = code[:voucherLength]
		password = ID()
	}
	if len(password) < 8 || len(password) > 128 {
		return a, code, errors.New("password length must be 8..128")
	}
	salt := ID()
	hash := store.HashPassword(password, salt)
	var voucher any
	if code != "" {
		voucher = Digest(code)
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return a, "", err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO subscriber_accounts VALUES(?,?,?,?,?,?,?,?)`, a.ID, username, name, salt, hash, voucher, "active", time.Now().Unix())
	if err != nil {
		return a, "", ErrConflict
	}
	if err = audit(tx, actor, "account.create", a.ID, reason, time.Now()); err != nil {
		return a, "", err
	}
	return a, code, tx.Commit()
}
func (s *Store) Authenticate(ctx context.Context, username, password string) (Account, error) {
	var a Account
	var salt, hash string
	var err error
	if username == "" {
		err = s.sql.QueryRowContext(ctx, `SELECT `+accountReviewColumns+`,salt,password_hash FROM subscriber_accounts WHERE voucher_hash=?`, Digest(password)).Scan(&a.ID, &a.Username, &a.Name, &a.Status, &a.StatusRevision, &salt, &hash)
	} else {
		err = s.sql.QueryRowContext(ctx, `SELECT `+accountReviewColumns+`,salt,password_hash FROM subscriber_accounts WHERE username=?`, username).Scan(&a.ID, &a.Username, &a.Name, &a.Status, &a.StatusRevision, &salt, &hash)
	}
	if err != nil {
		store.HashPassword(password, "subscriber-missing-account")
		return Account{}, ErrDenied
	}
	// Suspended accounts may inspect their balance; network admission checks status.
	if username != "" && !store.VerifyPassword(store.User{PasswordHash: hash, Salt: salt}, password) {
		return Account{}, ErrDenied
	}
	return a, nil
}
func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT `+accountReviewColumns+` FROM subscriber_accounts ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err = rows.Scan(&a.ID, &a.Username, &a.Name, &a.Status, &a.StatusRevision); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) SetStatus(ctx context.Context, actor, id, status, reason string) error {
	if status != "active" && status != "suspended" {
		return errors.New("invalid status")
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`UPDATE subscriber_accounts SET status=? WHERE id=?`, status, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrDenied
	}
	if err = audit(tx, actor, "account."+status, id, reason, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SavePlan(ctx context.Context, actor, reason string, p Plan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	p = normalizePlan(p)
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO subscriber_plans VALUES(?,?,?)`, p.ID, p.Version, string(b)); err != nil {
		return ErrConflict
	}
	if err = audit(tx, actor, "plan.version", p.ID, reason, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Plans(ctx context.Context) ([]Plan, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT definition FROM subscriber_plans ORDER BY id,version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var raw string
		var p Plan
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type Subscription struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"accountId"`
	Plan        Plan      `json:"plan"`
	AssignedAt  time.Time `json:"assignedAt"`
	ActivatedAt time.Time `json:"activatedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Status      string    `json:"status"`
	SpeedID     string    `json:"speedId"`
	SpeedState  string    `json:"speedState"`
}

func (s *Store) Assign(ctx context.Context, actor, account, plan string, version int, reason string, at time.Time) (string, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var raw string
	var p Plan
	if err = tx.QueryRow(`SELECT definition FROM subscriber_plans WHERE id=? AND version=?`, plan, version).Scan(&raw); err != nil {
		return "", err
	}
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return "", err
	}
	if err = p.Validate(); err != nil {
		return "", err
	}
	var active int
	if err = tx.QueryRow(`SELECT count(*) FROM subscriber_leases l JOIN subscriber_subscriptions s ON s.id=l.subscription_id WHERE s.account_id=? AND l.state!='stopped'`, account).Scan(&active); err != nil {
		return "", err
	}
	if active > 0 {
		return "", errors.New("reconcile active sessions before renewing")
	}
	if _, err = tx.Exec(`UPDATE subscriber_subscriptions SET status='superseded' WHERE account_id=? AND status!='superseded'`, account); err != nil {
		return "", err
	}
	id := ID()
	status := "pending"
	var activated, expires int64
	if p.Start == "assigned" {
		activated = at.Unix()
		expires = activated + int64(p.ValidSeconds)
		status = "active"
	}
	if _, err = tx.Exec(`INSERT INTO subscriber_subscriptions(id,account_id,plan_id,plan_version,assigned_at,activated_at,expires_at,status,speed_id) VALUES(?,?,?,?,?,?,?,?,?)`, id, account, p.ID, p.Version, at.Unix(), activated, expires, status, p.Speeds[0].ID); err != nil {
		return "", err
	}
	if err = audit(tx, actor, "subscription.assign", id, reason, at); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func subscription(tx *sql.Tx, account string) (Subscription, error) {
	var v Subscription
	var raw string
	var assigned, activated, expires int64
	err := tx.QueryRow(`SELECT s.id,s.account_id,p.definition,s.assigned_at,s.activated_at,s.expires_at,s.status,s.speed_id,s.speed_state FROM subscriber_subscriptions s JOIN subscriber_plans p ON p.id=s.plan_id AND p.version=s.plan_version WHERE s.account_id=? AND s.status!='superseded'`, account).Scan(&v.ID, &v.AccountID, &raw, &assigned, &activated, &expires, &v.Status, &v.SpeedID, &v.SpeedState)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v.Plan)
	v.AssignedAt = time.Unix(assigned, 0)
	if activated != 0 {
		v.ActivatedAt = time.Unix(activated, 0)
	}
	if expires != 0 {
		v.ExpiresAt = time.Unix(expires, 0)
	}
	return v, err
}
func usage(tx *sql.Tx, id string) (map[string]Usage, error) {
	out := map[string]Usage{}
	rows, err := tx.Query(`SELECT limit_id,period_start,used,quality FROM subscriber_buckets WHERE subscription_id=?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l, q string
		var start int64
		var n Quantity
		if err = rows.Scan(&l, &start, &n, &q); err != nil {
			rows.Close()
			return nil, err
		}
		out[Key(l, time.Unix(start, 0))] = Usage{Used: n, Quality: q}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(`SELECT limit_id,period_start,sum(amount),sum(provisional) FROM (
SELECT r.limit_id,r.period_start,r.amount,0 AS provisional FROM subscriber_reservations r JOIN subscriber_leases l ON l.id=r.lease_id WHERE l.subscription_id=? AND l.state!='stopped'
UNION ALL SELECT h.limit_id,h.period_start,h.amount,h.amount FROM subscriber_holds h JOIN subscriber_leases l ON l.id=h.lease_id WHERE l.subscription_id=? AND h.resolved_at=0
UNION ALL SELECT limit_id,period_start,amount,amount FROM subscriber_legacy_exposure WHERE subscription_id=? AND resolved_at=0
) GROUP BY limit_id,period_start`, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l string
		var start int64
		var n, provisional Quantity
		if err = rows.Scan(&l, &start, &n, &provisional); err != nil {
			return nil, err
		}
		key := Key(l, time.Unix(start, 0))
		v := out[key]
		v.Reserved = n
		v.Provisional = provisional
		if provisional > 0 {
			v.Quality = "reconciling"
		}
		out[key] = v
	}
	return out, rows.Err()
}

type Snapshot struct {
	PendingAccounting int          `json:"pendingAccounting"`
	Version           string       `json:"version"`
	Subscription      Subscription `json:"subscription"`
	Decision          Decision     `json:"decision"`
	ObservedAt        time.Time    `json:"observedAt"`
	LastSyncedAt      time.Time    `json:"lastSyncedAt"`
	Quality           string       `json:"quality"`
	LifetimeSeconds   Quantity     `json:"lifetimeSeconds"`
	LifetimeDownload  Quantity     `json:"lifetimeDownload"`
	LifetimeUpload    Quantity     `json:"lifetimeUpload"`
	Sessions          []Lease      `json:"sessions"`
}

func (s *Store) Snapshot(ctx context.Context, account string, at time.Time) (Snapshot, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	sub, err := subscription(tx, account)
	if err != nil {
		return Snapshot{}, err
	}
	u, err := usage(tx, sub.ID)
	if err != nil {
		return Snapshot{}, err
	}
	v := EmptySnapshot()
	v.Subscription = sub
	v.Decision = Evaluate(sub.Plan, sub.SpeedID, at, sub.ExpiresAt, u)
	v.ObservedAt = sub.AssignedAt
	v.Quality = "fresh"
	var status string
	var outstanding int
	if err = tx.QueryRow(`SELECT status FROM subscriber_accounts WHERE id=?`, account).Scan(&status); err != nil {
		return v, err
	}
	if err = tx.QueryRow(`SELECT count(*) FROM subscriber_leases WHERE subscription_id=? AND state!='stopped'`, sub.ID).Scan(&outstanding); err != nil {
		return v, err
	}
	if status != "active" {
		v.Decision.Allowed = false
		v.Decision.Reasons = append(v.Decision.Reasons, "account-suspended")
	}
	if outstanding >= sub.Plan.MaxSessions {
		v.Decision.Allowed = false
		v.Decision.Reasons = append(v.Decision.Reasons, "session-limit")
	}
	if !sub.ExpiresAt.IsZero() && !at.Before(sub.ExpiresAt) {
		v.Subscription.Status = "expired"
	}
	var synced sql.NullInt64
	err = tx.QueryRow(`SELECT coalesce(sum(l.seconds),0),coalesce(sum(l.download),0),coalesce(sum(l.upload),0),max(l.observed_at) FROM subscriber_leases l JOIN subscriber_subscriptions s ON s.id=l.subscription_id WHERE s.account_id=?`, account).Scan(&v.LifetimeSeconds, &v.LifetimeDownload, &v.LifetimeUpload, &synced)
	if err != nil {
		return v, err
	}
	if synced.Valid && synced.Int64 > 0 {
		v.LastSyncedAt = time.Unix(synced.Int64, 0)
		v.ObservedAt = v.LastSyncedAt
	}
	rows, err := tx.Query(`SELECT l.id,l.router_id,l.network,l.device,l.session_id,l.state,l.created_at,l.deadline,l.seconds,l.download,l.upload,l.observed_at,o.observed_at,o.present,o.payload FROM subscriber_leases l LEFT JOIN subscriber_observations o ON o.lease_id=l.id WHERE l.subscription_id=? ORDER BY l.created_at DESC LIMIT 100`, sub.ID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var l Lease
		var created, deadline, observed int64
		var apiAt sql.NullInt64
		var present sql.NullBool
		var payload sql.NullString
		if err = rows.Scan(&l.ID, &l.RouterID, &l.Network, &l.Device, &l.SessionID, &l.State, &created, &deadline, &l.Seconds, &l.Download, &l.Upload, &observed, &apiAt, &present, &payload); err != nil {
			rows.Close()
			return v, err
		}
		l.CreatedAt = time.Unix(created, 0)
		l.Deadline = time.Unix(deadline, 0)
		l.ObservedAt = time.Unix(observed, 0)
		l.ConnectionState = "unknown"
		l.RateState = "unknown"
		if l.State == "stopped" {
			l.ConnectionState = "disconnected"
		}
		if l.State == "reserved" {
			l.ConnectionState = "awaiting-accounting"
		}
		if apiAt.Valid {
			var obs ObservedSession
			if err = json.Unmarshal([]byte(payload.String), &obs); err != nil {
				rows.Close()
				return v, err
			}
			l.ObservedAt = time.Unix(apiAt.Int64, 0)
			if l.ObservedAt.After(v.ObservedAt) {
				v.ObservedAt = l.ObservedAt
			}
			fresh := at.Unix()-apiAt.Int64 <= 5 && at.Unix() >= apiAt.Int64-1
			if fresh && l.State != "stopped" {
				if present.Bool {
					l.ConnectionState = "connected"
				} else {
					l.ConnectionState = "disconnected"
				}
			}
			if obs.RateKnown {
				l.AppliedDown = obs.Down
				l.AppliedUp = obs.Up
				l.RateObservedAt = l.ObservedAt
				l.RateState = "stale"
				if fresh && present.Bool {
					l.RateState = "verified"
				}
			}
			if !fresh && l.State != "stopped" {
				v.Quality = "stale"
			}
		}
		v.Sessions = append(v.Sessions, l)
		if l.State != "stopped" {
			if observed == 0 && at.Before(l.Deadline) && v.Quality != "stale" {
				v.Quality = "reconciling"
			} else if at.Sub(l.ObservedAt) > 65*time.Second {
				v.Quality = "stale"
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	var radiusSync sql.NullInt64
	if err = tx.QueryRow(`SELECT max(src.observed_at) FROM subscriber_sources src JOIN subscriber_leases l ON l.id=src.lease_id JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE sub.account_id=? AND src.source='radius'`, account).Scan(&radiusSync); err != nil {
		return v, err
	}
	v.LastSyncedAt = time.Time{}
	if radiusSync.Valid {
		v.LastSyncedAt = time.Unix(radiusSync.Int64, 0)
	}
	v.PendingAccounting, err = pendingAccounting(tx, account)
	if err != nil {
		return v, err
	}
	if v.PendingAccounting > 0 {
		v.Quality = "reconciling"
		v.Decision.Allowed = false
		v.Decision.Reasons = append(v.Decision.Reasons, "accounting-reconciliation-required")
	}
	// The digest covers every client-visible decision, including expiry and
	// freshness changes without a new accounting event.
	raw, _ := json.Marshal(v)
	v.Version = Digest(string(raw))
	return v, tx.Commit()
}
func (s *Store) ChooseSpeed(ctx context.Context, account, speed string, at time.Time) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sub, err := subscription(tx, account)
	if err != nil {
		return err
	}
	found := false
	for _, p := range sub.Plan.Speeds {
		found = found || p.ID == speed
	}
	if !found {
		return ErrDenied
	}
	var last int64
	if err = tx.QueryRow(`SELECT speed_changed_at FROM subscriber_subscriptions WHERE id=?`, sub.ID).Scan(&last); err != nil {
		return err
	}
	if at.Unix()-last < 10 {
		return errors.New("wait before changing speed again")
	}
	// Persisting a choice is not evidence that a live queue has changed.
	state := "pending-reconnect"
	var active int
	if err = tx.QueryRow(`SELECT count(*) FROM subscriber_leases WHERE subscription_id=? AND state!='stopped'`, sub.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		state = "requested"
	}
	if _, err = tx.Exec(`UPDATE subscriber_subscriptions SET speed_id=?,speed_state=?,speed_changed_at=? WHERE id=?`, speed, state, at.Unix(), sub.ID); err != nil {
		return err
	}
	if err = audit(tx, account, "speed.request", sub.ID, "subscriber selected "+speed, at); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CheckReady(ctx context.Context) error {
	if _, err := s.sql.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		return err
	}
	var n int
	if err := s.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('subscriber_accounts','subscriber_operations','subscriber_sources','subscriber_observations','subscriber_holds','subscriber_reconciliations','subscriber_byte_gaps','subscriber_gap_periods','subscriber_legacy_exposure','subscriber_accounting_receipts')`).Scan(&n); err != nil {
		return err
	}
	if n != 10 {
		return errors.New("subscriber schema 41 migration required")
	}
	return nil
}
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("subscriber ledger: %w", err)
}

func (s *Store) AccountByID(ctx context.Context, id string) (Account, error) {
	var a Account
	err := s.sql.QueryRowContext(ctx, `SELECT `+accountReviewColumns+` FROM subscriber_accounts WHERE id=?`, id).Scan(&a.ID, &a.Username, &a.Name, &a.Status, &a.StatusRevision)
	return a, err
}

func EmptySnapshot() Snapshot {
	return Snapshot{Subscription: Subscription{Plan: Plan{Networks: []string{}, Policies: []Policy{}, Speeds: []Speed{}}}, Decision: Decision{Reasons: []string{}, Limits: []LimitView{}}, Sessions: []Lease{}}
}
func normalizePlan(p Plan) Plan {
	if p.Networks == nil {
		p.Networks = []string{}
	}
	if p.Speeds == nil {
		p.Speeds = []Speed{}
	}
	if p.Policies == nil {
		p.Policies = []Policy{}
	}
	for i := range p.Policies {
		if p.Policies[i].Limits == nil {
			p.Policies[i].Limits = []Limit{}
		}
		if p.Policies[i].Windows == nil {
			p.Policies[i].Windows = []Window{}
		}
		for j := range p.Policies[i].Windows {
			if p.Policies[i].Windows[j].Days == nil {
				p.Policies[i].Windows[j].Days = []int{}
			}
		}
	}
	return p
}
