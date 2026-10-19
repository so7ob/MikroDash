package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// RouterObservation is a complete, successful shared read, never a truncated
// response or a failed API request. Counters are session-cumulative. Version and
// counter direction must match the explicitly recorded per-router capability.
type RouterObservation struct {
	RouterID string
	Version  string
	At       time.Time
	Complete bool
	Sessions []ObservedSession
}
type ObservedSession struct {
	Version       string   `json:"version"`
	ID            string   `json:"id"`
	Server        string   `json:"server"`
	Username      string   `json:"username"`
	Device        string   `json:"device"`
	Address       string   `json:"address"`
	Seconds       Quantity `json:"seconds"`
	Download      Quantity `json:"download"`
	Upload        Quantity `json:"upload"`
	CountersKnown bool     `json:"countersKnown"`
	TimeLeft      int64    `json:"timeLeft"`
	TimeoutKnown  bool     `json:"timeoutKnown"`
	DownloadLimit Quantity `json:"downloadLimit"`
	UploadLimit   Quantity `json:"uploadLimit"`
	TotalLimit    Quantity `json:"totalLimit"`
	LimitsKnown   bool     `json:"limitsKnown"`
	Down          Quantity `json:"down"`
	Up            Quantity `json:"up"`
	RateKnown     bool     `json:"rateKnown"`
}
type trackedSession struct {
	Enforcement
	CreatedAt time.Time
	Deadline  time.Time
}

func (s *Store) tracked(ctx context.Context, router string) ([]trackedSession, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT l.id,sub.account_id,a.username,l.network,l.device,l.session_id,l.created_at,l.deadline,l.started_at FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id JOIN subscriber_accounts a ON a.id=sub.account_id WHERE l.router_id=? AND l.state!='stopped'`, router)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []trackedSession{}
	for rows.Next() {
		var v trackedSession
		var created, deadline, started int64
		v.RouterID = router
		if err = rows.Scan(&v.LeaseID, &v.AccountID, &v.Username, &v.Network, &v.Device, &v.SessionID, &created, &deadline, &started); err != nil {
			return nil, err
		}
		v.CreatedAt = time.Unix(created, 0)
		v.Deadline = time.Unix(deadline, 0)
		if started > 0 {
			v.StartedAt = time.Unix(started, 0)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func sessionMatches(e trackedSession, row ObservedSession, server string, at time.Time) bool {
	if row.ID == "" || row.Server != server || row.Username != e.Username || row.Device != e.Device || row.Seconds < 0 {
		return false
	}
	start := at.Add(-time.Duration(row.Seconds) * time.Second)
	expected := e.StartedAt
	if expected.IsZero() {
		expected = e.CreatedAt
	}
	skew := start.Sub(expected)
	return skew >= -5*time.Second && skew <= 5*time.Second
}
func findNAS(clients []NAS, router, network string) (NAS, bool) {
	for _, c := range clients {
		if c.RouterID == router && c.Network == network {
			return c, true
		}
	}
	return NAS{}, false
}
func (s *Store) ObserveRouter(ctx context.Context, v RouterObservation, clients []NAS) error {
	if !v.Complete || v.At.IsZero() || v.RouterID == "" {
		return errors.New("complete router observation required")
	}
	leases, err := s.tracked(ctx, v.RouterID)
	if err != nil {
		return err
	}
	for _, e := range leases {
		n, ok := findNAS(clients, v.RouterID, e.Network)
		if !ok {
			return ErrDenied
		}
		var match ObservedSession
		count := 0
		for _, row := range v.Sessions {
			if sessionMatches(e, row, n.CalledStationID, v.At) {
				match = row
				count++
			}
		}
		if count > 1 {
			return errors.New("ambiguous live HotSpot session")
		}
		present := count == 1
		previous, _, _, perr := s.observation(ctx, e.LeaseID)
		if perr != nil && !errors.Is(perr, sql.ErrNoRows) {
			return perr
		}
		if present && previous.ID != "" && previous.ID != match.ID {
			return errors.New("HotSpot session identity changed; reconciliation required")
		}
		if present {
			match.Version = v.Version
		}
		raw, err := json.Marshal(match)
		if err != nil {
			return err
		}
		if _, err = s.sql.ExecContext(ctx, `INSERT INTO subscriber_observations VALUES(?,?,?,?,?) ON CONFLICT(lease_id) DO UPDATE SET observed_at=excluded.observed_at,present=excluded.present,payload=CASE WHEN excluded.present=1 THEN excluded.payload ELSE subscriber_observations.payload END`, e.LeaseID, v.RouterID, v.At.Unix(), present, string(raw)); err != nil {
			return err
		}
		if present && match.CountersKnown && n.Capabilities.Recorded() && n.Capabilities.Version == v.Version && (n.Capabilities.CounterMode == "input-upload" || n.Capabilities.CounterMode == "output-upload") && e.SessionID != "" {
			event := Accounting{LeaseID: e.LeaseID, RouterID: e.RouterID, Network: e.Network, Device: e.Device, SessionID: e.SessionID, Status: "interim", Source: "api", Seconds: match.Seconds, Download: match.Download, Upload: match.Upload, At: v.At}
			if err = s.Account(ctx, event); err != nil {
				return err
			}
		}
		if !present {
			// A complete API absence is evidence. Unreachable/truncated reads never
			// enter this path and cannot release either a quota hold or a session slot.
			if err = s.reconcileAbsent(ctx, e, v.At); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) observation(ctx context.Context, lease string) (ObservedSession, time.Time, bool, error) {
	var row ObservedSession
	var at int64
	var present bool
	var raw string
	err := s.sql.QueryRowContext(ctx, `SELECT observed_at,present,payload FROM subscriber_observations WHERE lease_id=?`, lease).Scan(&at, &present, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &row)
	}
	return row, time.Unix(at, 0), present, err
}
func (s *Store) reconcileAbsent(ctx context.Context, e trackedSession, at time.Time) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRow(`SELECT state FROM subscriber_leases WHERE id=?`, e.LeaseID).Scan(&state); err != nil {
		return err
	}
	if state == "stopped" {
		return tx.Commit()
	}
	// Wait for initial admission to start; an Access-Accept may still be in flight.
	// Pending CoA intent can have reached the NAS before a crash, so its maximum
	// planned deadline participates even if no ACK was saved locally.
	until := e.Deadline.Unix()
	var planned sql.NullInt64
	if err = tx.QueryRow(`SELECT max(target_deadline) FROM subscriber_operations WHERE lease_id=? AND kind='renew' AND attempts>0`, e.LeaseID).Scan(&planned); err != nil {
		return err
	}
	if planned.Valid {
		until = max(until, planned.Int64)
	}
	if at.Unix() <= until {
		return tx.Commit()
	}
	if _, err = tx.Exec(`INSERT INTO subscriber_holds(id,lease_id,limit_id,period_start,amount) SELECT ?||':'||limit_id||':'||period_start,lease_id,limit_id,period_start,amount FROM subscriber_reservations WHERE lease_id=? AND amount>0`, ID(), e.LeaseID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_leases SET state='stopped' WHERE id=? AND state!='stopped'`, e.LeaseID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM subscriber_reservations WHERE lease_id=?`, e.LeaseID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriber_operations SET state='failed',error='session absent after bounded delegation',updated_at=? WHERE lease_id=? AND state IN ('requested','applying','acknowledged')`, at.Unix(), e.LeaseID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO subscriber_reconciliations(id,lease_id,actor,reason,evidence,created_at,seconds,download,upload) SELECT ?,id,'service:subscriber','missing Stop; unused grant retained as unresolved hold','complete API absence after maximum issued delegation',?,seconds,download,upload FROM subscriber_leases WHERE id=?`, ID(), at.Unix(), e.LeaseID); err != nil {
		return err
	}
	if err = audit(tx, "service:subscriber", "session.absence.settled", e.LeaseID, "complete shared API snapshot; unresolved exposure retained as hold", at); err != nil {
		return err
	}
	return tx.Commit()
}

// VerifyObservedSession reuses a fresh shared read for a narrow write guard.
func (s *Store) VerifyObservedSession(ctx context.Context, e Enforcement, server string, at time.Time) bool {
	row, observed, present, err := s.observation(ctx, e.LeaseID)
	if err != nil || !present || at.Sub(observed) > 5*time.Second || observed.After(at.Add(time.Second)) {
		return false
	}
	return sessionMatches(trackedSession{Enforcement: e}, row, server, observed)
}
