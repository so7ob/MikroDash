package subscriber

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// EditAccountName changes display metadata only. The expected name fences stale
// forms; the previous and replacement values commit with the reasoned audit.
func (s *Store) EditAccountName(ctx context.Context, actor, id, expected, name, reason string) (Account, error) {
	var account Account
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return account, errors.New("actor and change reason required")
	}
	if len(name) > 256 || !utf8.ValidString(name) {
		return account, errors.New("display name must be valid Unicode and at most 256 UTF-8 bytes")
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return account, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT id,username,name,status FROM subscriber_accounts WHERE id=?`, id).Scan(&account.ID, &account.Username, &account.Name, &account.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrDenied
		}
		return Account{}, err
	}
	if account.Name != expected {
		return Account{}, ErrConflict
	}
	if account.Name == name {
		return account, tx.Commit()
	}
	history, err := json.Marshal(struct {
		Reason string `json:"reason"`
		Before string `json:"before"`
		After  string `json:"after"`
	}{Reason: reason, Before: account.Name, After: name})
	if err != nil {
		return Account{}, err
	}
	if _, err = tx.Exec(`UPDATE subscriber_accounts SET name=? WHERE id=?`, name, id); err != nil {
		return Account{}, err
	}
	if err = audit(tx, actor, "account.name.edit", id, string(history), time.Now()); err != nil {
		return Account{}, err
	}
	account.Name = name
	return account, tx.Commit()
}
