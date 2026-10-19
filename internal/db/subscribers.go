package db

import "mikrodash/internal/subscriber"

// Subscribers uses the existing SQLite handle and its immediate transactions.
// It never creates a second accounting database or changes users.json.
func (d *DB) Subscribers() *subscriber.Store {
	if d == nil || d.sql == nil {
		return nil
	}
	return subscriber.NewStore(d.sql)
}
