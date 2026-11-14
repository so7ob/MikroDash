package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"mikrodash/internal/store"
)

// CredentialSchema is additive migration 46. Missing account revisions mean zero;
// old tickets without a credential proof cannot authenticate after upgrading.
const CredentialSchema = `
CREATE TABLE IF NOT EXISTS subscriber_credential_revisions (
 account_id TEXT PRIMARY KEY REFERENCES subscriber_accounts(id),
 revision INTEGER NOT NULL CHECK(revision>=0)
);
CREATE TABLE IF NOT EXISTS subscriber_ticket_credentials (
 token_hash TEXT PRIMARY KEY REFERENCES subscriber_tickets(token_hash) ON DELETE CASCADE,
 credential_revision INTEGER NOT NULL CHECK(credential_revision>=0)
);
`

// ErrCredentialInput identifies a refusal before any credential write began.
var ErrCredentialInput = errors.New("invalid credential reset")

const credentialRevisionColumn = `(SELECT COALESCE((SELECT revision FROM subscriber_credential_revisions WHERE account_id=subscriber_accounts.id),0))`

func credentialRevision(tx *sql.Tx, account string) (Quantity, error) {
	var revision Quantity
	err := tx.QueryRow(`SELECT `+credentialRevisionColumn+` FROM subscriber_accounts WHERE id=?`, account).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrDenied
	}
	return revision, err
}

// ValidateCredentialRevision is the durable authority for a portal proof. A
// transient database failure never becomes a successful authentication check.
func (s *Store) ValidateCredentialRevision(ctx context.Context, account string, expected Quantity) error {
	if account == "" || expected < 0 {
		return ErrDenied
	}
	var revision Quantity
	err := s.sql.QueryRowContext(ctx, `SELECT `+credentialRevisionColumn+` FROM subscriber_accounts WHERE id=?`, account).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) || err == nil && revision != expected {
		return ErrDenied
	}
	return err
}

// ResetCredential serializes with ticket issuance/redemption and authenticated
// admission. Hash outside the write transaction; compare the reviewed revision
// inside it, then commit the new secret, revocation and audit together.
func (s *Store) ResetCredential(ctx context.Context, actor, account, password, reason string, expected Quantity) (Account, error) {
	if expected < 0 || expected == math.MaxInt64 {
		return Account{}, fmt.Errorf("%w: credential revision cannot advance within the supported range", ErrCredentialInput)
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || !utf8.ValidString(reason) || len(reason) > 2048 {
		return Account{}, fmt.Errorf("%w: actor and a change reason of at most 2048 UTF-8 bytes are required", ErrCredentialInput)
	}
	if !utf8.ValidString(password) || len(password) < 8 || len(password) > 128 || strings.ContainsAny(password, "\x00\r\n") {
		return Account{}, fmt.Errorf("%w: password must be 8 to 128 UTF-8 bytes without NUL or line breaks", ErrCredentialInput)
	}
	if strings.Contains(reason, password) {
		return Account{}, fmt.Errorf("%w: do not include the new password in the audit reason", ErrCredentialInput)
	}
	salt := ID()
	hash := store.HashPassword(password, salt)
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	current, err := credentialRevision(tx, account)
	if err != nil {
		return Account{}, err
	}
	if current != expected {
		return Account{}, ErrConflict
	}
	next := current + 1
	if _, err = tx.Exec(`INSERT INTO subscriber_credential_revisions(account_id,revision) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET revision=excluded.revision`, account, next); err != nil {
		return Account{}, err
	}
	if _, err = tx.Exec(`UPDATE subscriber_accounts SET salt=?,password_hash=?,voucher_hash=NULL WHERE id=?`, salt, hash, account); err != nil {
		return Account{}, err
	}
	if _, err = tx.Exec(`UPDATE subscriber_tickets SET expires_at=0 WHERE account_id=?`, account); err != nil {
		return Account{}, err
	}
	if _, err = tx.Exec(`UPDATE subscriber_connection_flows SET state='rejected',reasons='["credentials-changed"]',updated_at=? WHERE token_hash IN (SELECT token_hash FROM subscriber_tickets WHERE account_id=?) AND state IN ('requested','authorizing')`, time.Now().Unix(), account); err != nil {
		return Account{}, err
	}
	history, _ := json.Marshal(struct {
		Reason string   `json:"reason"`
		Before Quantity `json:"beforeRevision"`
		After  Quantity `json:"afterRevision"`
	}{reason, current, next})
	if err = audit(tx, actor, "account.credential.reset", account, string(history), time.Now()); err != nil {
		return Account{}, err
	}
	var result Account
	if err = tx.QueryRow(`SELECT `+accountReviewColumns+` FROM subscriber_accounts WHERE id=?`, account).Scan(&result.ID, &result.Username, &result.Name, &result.Status, &result.StatusRevision, &result.CredentialRevision); err != nil {
		return Account{}, err
	}
	if err = tx.Commit(); err != nil {
		return Account{}, err
	}
	return result, nil
}
