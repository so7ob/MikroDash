package subscriber

import (
	"context"
	"time"
)

// IssueTicket accepts a device resolved by the trusted shared RouterOS reader,
// never a browser's claimed MAC. The caller owns the portal session and network.
func (s *Store) IssueTicket(ctx context.Context, account, network, verifiedDevice string, at time.Time) (string, error) {
	if account == "" || network == "" || verifiedDevice == "" {
		return "", ErrDenied
	}
	token := "mdt_" + ID()
	_, err := s.sql.ExecContext(ctx, `INSERT INTO subscriber_tickets(token_hash,account_id,network,device,expires_at) VALUES(?,?,?,?,?)`, Digest(token), account, network, verifiedDevice, at.Add(30*time.Second).Unix())
	return token, err
}
func (s *Store) RedeemTicket(ctx context.Context, token, network, device, request string, at time.Time) (Account, error) {
	var a Account
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var n, d, prior string
	var expires int64
	err = tx.QueryRow(`SELECT a.id,a.username,a.name,a.status,t.network,t.device,t.expires_at,t.request_key FROM subscriber_tickets t JOIN subscriber_accounts a ON a.id=t.account_id WHERE t.token_hash=?`, Digest(token)).Scan(&a.ID, &a.Username, &a.Name, &a.Status, &n, &d, &expires, &prior)
	if err != nil || a.Status != "active" || n != network || d != device || at.Unix() >= expires || request == "" || (prior != "" && prior != request) {
		return Account{}, ErrDenied
	}
	if _, err = tx.Exec(`UPDATE subscriber_tickets SET request_key=? WHERE token_hash=?`, request, Digest(token)); err != nil {
		return Account{}, err
	}
	return a, tx.Commit()
}
