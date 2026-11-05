package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ConnectionStatusFor reads one coherent SQLite snapshot. It never asks the NAS
// per browser and never derives this device's state from another account lease.
func (s *Store) ConnectionStatusFor(ctx context.Context, q ConnectionQuery, clients []NAS, at time.Time) (ConnectionStatus, error) {
	out := EmptyConnectionStatus()
	peer, valid := connectionPeer(q.ClientAddress)
	if !valid || q.AccountID == "" || len(q.FlowID) != 64 || strings.IndexFunc(q.FlowID, func(c rune) bool { return !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) }) >= 0 {
		return out, ErrDenied
	}
	var router, username, state, reasons, request string
	var created, expires int64
	var leaseID, leaseState, leaseRouter, leaseNetwork, leaseDevice, leaseRequest, session, raw sql.NullString
	var leaseCreated, leaseStarted, observed sql.NullInt64
	var present sql.NullBool
	err := s.sql.QueryRowContext(ctx, `SELECT t.network,t.device,t.expires_at,t.request_key,f.router_id,f.created_at,f.state,f.reasons,a.username,f.lease_id,l.state,l.router_id,l.network,l.device,l.request_key,l.session_id,l.created_at,l.started_at,o.observed_at,o.present,o.payload FROM subscriber_connection_flows f JOIN subscriber_tickets t ON t.token_hash=f.token_hash JOIN subscriber_accounts a ON a.id=t.account_id LEFT JOIN subscriber_leases l ON l.id=f.lease_id LEFT JOIN subscriber_observations o ON o.lease_id=l.id WHERE f.token_hash=? AND t.account_id=? AND f.client_address=?`, q.FlowID, q.AccountID, peer).Scan(&out.Network, &out.Device, &expires, &request, &router, &created, &state, &reasons, &username, &leaseID, &leaseState, &leaseRouter, &leaseNetwork, &leaseDevice, &leaseRequest, &session, &leaseCreated, &leaseStarted, &observed, &present, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrDenied
	}
	if err != nil {
		return out, err
	}
	out.FlowID = q.FlowID
	out.LeaseID = leaseID.String
	out.RequestedAt = time.Unix(created, 0).UTC()
	if err = json.Unmarshal([]byte(reasons), &out.Reasons); err != nil {
		return out, err
	}
	if out.Reasons == nil {
		out.Reasons = []string{}
	}
	if observed.Valid {
		out.ObservedAt = time.Unix(observed.Int64, 0).UTC()
	}
	switch state {
	case "requested", "authorizing":
		out.Stage = state
		if at.Unix() >= expires {
			out.Stage = "expired"
			out.Reasons = []string{"ticket-expired"}
		}
	case "rejected":
		out.Stage = "rejected"
	case "authorized":
		out.Stage = "authorized"
		switch {
		case !leaseID.Valid || !leaseState.Valid:
			out.Stage = "unconfirmed"
			out.Reasons = []string{"authorization-state-unavailable"}
		case leaseRouter.String != router || leaseNetwork.String != out.Network || leaseDevice.String != out.Device || leaseRequest.String != request:
			out.Stage = "unconfirmed"
			out.Reasons = []string{"connection-identity-changed"}
		case leaseState.String == "stopped":
			out.Stage = "disconnected"
			out.Reasons = []string{"session-ended"}
		case !observed.Valid:
			out.Stage = "authorized"
			out.Reasons = []string{"waiting-for-hotspot-confirmation"}
			if at.Sub(out.RequestedAt) > 5*time.Second || out.RequestedAt.After(at.Add(time.Second)) {
				out.Stage = "unconfirmed"
				out.Reasons = []string{"hotspot-confirmation-unavailable"}
			}
		case at.Sub(out.ObservedAt) > 5*time.Second || out.ObservedAt.After(at.Add(time.Second)):
			out.Stage = "unconfirmed"
			out.Reasons = []string{"router-observation-stale"}
		case !present.Valid || !present.Bool:
			out.Stage = "authorized"
			out.Reasons = []string{"waiting-for-hotspot-confirmation"}
		default:
			out.Stage = "unconfirmed"
			out.Reasons = []string{"connection-identity-unconfirmed"}
			n, enrolled := findNAS(clients, router, out.Network)
			if enrolled && present.Valid && present.Bool && observed.Valid && at.Sub(out.ObservedAt) <= 5*time.Second && !out.ObservedAt.After(at.Add(time.Second)) {
				var row ObservedSession
				if err = json.Unmarshal([]byte(raw.String), &row); err != nil {
					return out, err
				}
				started := leaseStarted.Int64
				if started == 0 {
					started = leaseCreated.Int64
				}
				expected := trackedSession{Enforcement: Enforcement{AccountID: q.AccountID, RouterID: router, Network: out.Network, Device: out.Device, Username: username, SessionID: session.String, StartedAt: time.Unix(started, 0)}, CreatedAt: time.Unix(leaseCreated.Int64, 0)}
				address, known := connectionPeer(row.Address)
				if known && address == peer && n.Capabilities.Recorded() && row.Version == n.Capabilities.Version && sessionMatches(expected, row, n.CalledStationID, out.ObservedAt) {
					out.Stage = "connected"
					out.Reasons = []string{}
				}
			}
		}
	default:
		out.Stage = "unconfirmed"
		out.Reasons = []string{"authorization-state-unavailable"}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	out.Version = Digest(string(encoded))
	return out, nil
}
