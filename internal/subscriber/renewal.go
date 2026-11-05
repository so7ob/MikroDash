package subscriber

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) prepareRenewal(tx *sql.Tx, sub Subscription, lease string, at time.Time, started int64, seconds, down, up Quantity, own map[string]Quantity, usage map[string]Usage, q *OperationRequest) (int64, error) {
	var active int
	if err := tx.QueryRow(`SELECT count(*) FROM subscriber_leases WHERE subscription_id=? AND state!='stopped'`, sub.ID).Scan(&active); err != nil {
		return 0, err
	}
	var observed int64
	if err := tx.QueryRow(`SELECT observed_at FROM subscriber_leases WHERE id=?`, lease).Scan(&observed); err != nil {
		return 0, err
	}
	if at.Unix()-observed > 5 || observed > at.Unix()+1 {
		return 0, errors.New("fresh session counters required for renewal")
	}
	from := time.Unix(min(at.Unix(), started+int64(seconds)), 0)
	a, reservations, end, err := planDelegation(sub.Plan, sub.SpeedID, at, from, sub.ExpiresAt, usage, Quantity(max(1, sub.Plan.MaxSessions-active+1)))
	if err != nil {
		return 0, err
	}
	a.LeaseID = lease
	q.RequireRate = a.Decision.Down < q.Admission.Decision.Down || a.Decision.Up < q.Admission.Decision.Up
	var raw string
	err = tx.QueryRow(`SELECT payload FROM subscriber_observations WHERE lease_id=? AND present=1`, lease).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var row ObservedSession
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &row); err != nil {
			return 0, err
		}
	}
	q.RequireRate = q.RequireRate || !row.RateKnown || row.Down > a.Decision.Down || row.Up > a.Decision.Up
	q.Admission, q.TimeBasis = a, renewalTimeBasis
	q.GrantedSeconds = Quantity(end.Unix()-started) - seconds
	if q.GrantedSeconds <= 0 {
		return 0, ErrDenied
	}
	if q.DownloadCeiling, err = add(down, a.Download); err != nil {
		return 0, err
	}
	if q.UploadCeiling, err = add(up, a.Upload); err != nil {
		return 0, err
	}
	if a.Total > 0 {
		used, err := add(down, up)
		if err != nil {
			return 0, err
		}
		if q.TotalCeiling, err = add(used, a.Total); err != nil {
			return 0, err
		}
	}
	for _, r := range reservations {
		// An older attempted cap can still be on the NAS. Do not release its
		// exposure while committing a different intent or waiting for readback.
		amount := max(r.Amount, own[Key(r.LimitID, r.Start)])
		if _, err = tx.Exec(`INSERT INTO subscriber_reservations VALUES(?,?,?,?) ON CONFLICT(lease_id,limit_id,period_start) DO UPDATE SET amount=excluded.amount`, lease, r.LimitID, r.Start.Unix(), amount); err != nil {
			return 0, err
		}
	}
	return end.Unix(), nil
}
