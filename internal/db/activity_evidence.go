package db

import (
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"modernc.org/sqlite"
)

const activitySummaryIndexDDL = `CREATE INDEX IF NOT EXISTS activity_shapes_summary ON activity_shapes(
 router_id,id,kind,domain,service,category,user,server,ip,mac,client,canonical,device_name,ip_basis,qtype,match_method,country,search_text)`

// A fixed-width key orders the exact signed timestamp/ID pair as a SQLite BLOB.
// Rollups retain this key so each query need not seek the source fact index once
// for every selected shape just to recover its latest evidence ID.
func activityEvidenceKey(at, id int64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key, uint64(at)^(uint64(1)<<63))
	binary.BigEndian.PutUint64(key[8:], uint64(id)^(uint64(1)<<63))
	return key
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("activity_evidence_key", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		at, aok := args[0].(int64)
		id, iok := args[1].(int64)
		if !aok || !iok {
			return nil, errors.New("invalid activity evidence identity")
		}
		return activityEvidenceKey(at, id), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("activity_evidence_id", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		key, ok := args[0].([]byte)
		if !ok || len(key) != 16 {
			return nil, errors.New("activity evidence index requires preparation")
		}
		return int64(binary.BigEndian.Uint64(key[8:]) ^ (uint64(1) << 63)), nil
	})
}

// One atomic migration, preserving source rows, counts, revisions and checkpoints.
// Build the representative keys from a covering fact scan, then fill the existing
// hour/day rollups. Neither legacy payloads nor subscriber records are rewritten.
var activityEvidenceMigration = []string{
	`ALTER TABLE activity_facts ADD COLUMN evidence_key BLOB NOT NULL DEFAULT X''`,
	`ALTER TABLE activity_daily ADD COLUMN evidence_key BLOB NOT NULL DEFAULT X''`,
	`ALTER TABLE activity_hourly ADD COLUMN groups INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE activity_hourly ADD COLUMN last_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE activity_hourly ADD COLUMN evidence_key BLOB NOT NULL DEFAULT X''`,
	`ALTER TABLE activity_hour_totals ADD COLUMN groups INTEGER NOT NULL DEFAULT 0`,
	`UPDATE activity_facts SET evidence_key=activity_evidence_key(at,event_id) WHERE length(evidence_key)<>16`,
	`DROP INDEX IF EXISTS activity_facts_time`,
	`DROP INDEX IF EXISTS activity_facts_shape`,
	`CREATE INDEX activity_facts_time ON activity_facts(router_id,at,event_id,shape_id,evidence_key)`,
	`CREATE INDEX activity_facts_shape ON activity_facts(router_id,shape_id,at,event_id,evidence_key)`,
	activitySummaryIndexDDL,
	`CREATE TEMP TABLE IF NOT EXISTS activity_hour_evidence_upgrade (
 router_id TEXT NOT NULL,hour INTEGER NOT NULL,shape_id INTEGER NOT NULL,groups INTEGER NOT NULL,last_at INTEGER NOT NULL,evidence_key BLOB NOT NULL,
 PRIMARY KEY(router_id,hour,shape_id)) WITHOUT ROWID`,
	`DELETE FROM activity_hour_evidence_upgrade`,
	`INSERT INTO activity_hour_evidence_upgrade SELECT router_id,at/3600000,shape_id,COUNT(DISTINCT at/1000),MAX(at),MAX(evidence_key)
 FROM activity_facts GROUP BY router_id,at/3600000,shape_id`,
	`UPDATE activity_hourly AS h SET (groups,last_at,evidence_key)=(SELECT b.groups,b.last_at,b.evidence_key FROM activity_hour_evidence_upgrade b
 WHERE b.router_id=h.router_id AND b.hour=h.hour AND b.shape_id=h.shape_id)`,
	`CREATE TEMP TABLE IF NOT EXISTS activity_day_evidence_upgrade (
 router_id TEXT NOT NULL,day INTEGER NOT NULL,shape_id INTEGER NOT NULL,evidence_key BLOB NOT NULL,
 PRIMARY KEY(router_id,day,shape_id)) WITHOUT ROWID`,
	`DELETE FROM activity_day_evidence_upgrade`,
	`INSERT INTO activity_day_evidence_upgrade SELECT router_id,hour/24,shape_id,MAX(evidence_key)
 FROM activity_hour_evidence_upgrade GROUP BY router_id,hour/24,shape_id`,
	`UPDATE activity_daily AS d SET evidence_key=(SELECT b.evidence_key FROM activity_day_evidence_upgrade b
 WHERE b.router_id=d.router_id AND b.day=d.day AND b.shape_id=d.shape_id)`,
	`UPDATE activity_hour_totals AS t SET groups=(SELECT COALESCE(SUM(b.groups),0) FROM activity_hour_evidence_upgrade b
	 CROSS JOIN activity_shapes s INDEXED BY activity_shapes_summary ON s.router_id=b.router_id AND s.id=b.shape_id
 WHERE b.router_id=t.router_id AND b.hour=t.hour AND s.kind=t.kind)`,
	`DROP TABLE activity_hour_evidence_upgrade`,
	`DROP TABLE activity_day_evidence_upgrade`,
	`DROP INDEX IF EXISTS activity_daily_shape`,
	`DROP INDEX IF EXISTS activity_hourly_shape`,
	`CREATE INDEX activity_daily_shape ON activity_daily(router_id,shape_id,day,n,groups,last_at,evidence_key)`,
	`CREATE INDEX activity_hourly_shape ON activity_hourly(router_id,shape_id,hour,n,groups,last_at,evidence_key)`,
	`DROP TRIGGER IF EXISTS activity_fact_insert`,
	`DROP TRIGGER IF EXISTS activity_fact_delete`,
	activityFactTriggersDDL,
}
