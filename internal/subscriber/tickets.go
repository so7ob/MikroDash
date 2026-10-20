package subscriber

import (
	"context"
	"net/netip"
	"time"
)

// TicketRequest contains only server-verified context. The portal resolves its
// device from the enrolled NAS host table using the trusted HTTP peer address.
type TicketRequest struct {
	AccountID     string
	RouterID      string
	Network       string
	Device        string
	ClientAddress string
}

type TicketBinding struct {
	RouterID string
	Network  string
	Device   string
	Username string
}

func connectionPeer(raw string) (string, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" {
		return "", false
	}
	return ip.Unmap().String(), true
}

// IssueTicket atomically retains a short-lived credential and its connection
// context. Hashes identify the owned flow; a flow reference cannot authenticate.
func (s *Store) IssueTicket(ctx context.Context, in TicketRequest, at time.Time) (string, error) {
	peer, valid := connectionPeer(in.ClientAddress)
	if in.AccountID == "" || in.RouterID == "" || in.Network == "" || in.Device == "" || !valid {
		return "", ErrDenied
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	token := "mdt_" + ID()
	key := Digest(token)
	if _, err = tx.Exec(`INSERT INTO subscriber_tickets(token_hash,account_id,network,device,expires_at) VALUES(?,?,?,?,?)`, key, in.AccountID, in.Network, in.Device, at.Add(30*time.Second).Unix()); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO subscriber_connection_flows(token_hash,router_id,client_address,created_at,updated_at) VALUES(?,?,?,?,?)`, key, in.RouterID, peer, at.Unix(), at.Unix()); err != nil {
		return "", err
	}
	if err = audit(tx, "service:subscriber", "authentication.ticket.requested", key, "HTTPS portal request; trusted NAS device binding", at); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) RedeemTicket(ctx context.Context, token string, b TicketBinding, request string, at time.Time) (Account, error) {
	var a Account
	if b.RouterID == "" || b.Network == "" || b.Device == "" || b.Username == "" || request == "" {
		return a, ErrDenied
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var network, device, router, prior, state string
	var expires int64
	err = tx.QueryRow(`SELECT a.id,a.username,a.name,a.status,t.network,t.device,t.expires_at,t.request_key,f.router_id,f.state FROM subscriber_tickets t JOIN subscriber_accounts a ON a.id=t.account_id JOIN subscriber_connection_flows f ON f.token_hash=t.token_hash WHERE t.token_hash=?`, Digest(token)).Scan(&a.ID, &a.Username, &a.Name, &a.Status, &network, &device, &expires, &prior, &router, &state)
	if err != nil || a.Status != "active" || a.Username != b.Username || router != b.RouterID || network != b.Network || device != b.Device || at.Unix() >= expires || state == "rejected" || (prior != "" && prior != request) {
		return Account{}, ErrDenied
	}
	if prior == "" {
		if _, err = tx.Exec(`UPDATE subscriber_tickets SET request_key=? WHERE token_hash=?`, request, Digest(token)); err != nil {
			return Account{}, err
		}
		if _, err = tx.Exec(`UPDATE subscriber_connection_flows SET state='authorizing',updated_at=? WHERE token_hash=? AND state='requested'`, at.Unix(), Digest(token)); err != nil {
			return Account{}, err
		}
		if err = audit(tx, "service:subscriber", "authentication.ticket.redeemed", Digest(token), "NAS, network, account and device matched", at); err != nil {
			return Account{}, err
		}
	}
	return a, tx.Commit()
}
