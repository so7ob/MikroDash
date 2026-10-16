package db

import (
	"encoding/json"
	"mikrodash/internal/activity"
)

func (d *DB) ActivityCheckpoints(router string) ([]activity.Checkpoint, error) {
	rows, err := d.sql.Query(`SELECT payload FROM activity_checkpoints WHERE router_id=?`, router)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activity.Checkpoint{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var c activity.Checkpoint
		if err = json.Unmarshal([]byte(b), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
