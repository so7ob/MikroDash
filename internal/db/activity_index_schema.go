package db

// The source table remains authoritative. Pending rows make partial indexing
// observable; no reader may confuse an incomplete projection with empty history.
// Source triggers also cover metadata replay and retention in their transaction.
const activityIndexDDL = `
CREATE TABLE IF NOT EXISTS activity_index_state (
 router_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 1, total INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS activity_projection_context (router_id TEXT PRIMARY KEY,version TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS activity_shapes (
 id INTEGER PRIMARY KEY AUTOINCREMENT, router_id TEXT NOT NULL, fingerprint TEXT NOT NULL,
 payload TEXT NOT NULL, kind TEXT NOT NULL, domain TEXT NOT NULL, client TEXT NOT NULL,
 user TEXT NOT NULL, ip TEXT NOT NULL, mac TEXT NOT NULL, server TEXT NOT NULL,
 canonical TEXT NOT NULL, device_name TEXT NOT NULL, ip_basis TEXT NOT NULL,
 qtype TEXT NOT NULL, match_method TEXT NOT NULL, destination TEXT NOT NULL,
 label TEXT NOT NULL, user_label TEXT NOT NULL,
 service TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT '',
 country TEXT NOT NULL DEFAULT 'Unknown', search_text TEXT NOT NULL DEFAULT '',
 enriched TEXT NOT NULL DEFAULT '', ready INTEGER NOT NULL DEFAULT 0,
 UNIQUE(router_id,fingerprint)
);
CREATE INDEX IF NOT EXISTS activity_shapes_domain ON activity_shapes(router_id,domain,id);
CREATE INDEX IF NOT EXISTS activity_shapes_client ON activity_shapes(router_id,client,id);
CREATE INDEX IF NOT EXISTS activity_shapes_service ON activity_shapes(router_id,service,id);
CREATE INDEX IF NOT EXISTS activity_shapes_category ON activity_shapes(router_id,category,id);
CREATE INDEX IF NOT EXISTS activity_shapes_ready ON activity_shapes(router_id,ready,id);
CREATE TABLE IF NOT EXISTS activity_shape_answers (
 shape_id INTEGER NOT NULL REFERENCES activity_shapes(id) ON DELETE CASCADE,
 ip TEXT NOT NULL, country TEXT NOT NULL, PRIMARY KEY(shape_id,ip)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS activity_answers_ip ON activity_shape_answers(ip,shape_id);
CREATE INDEX IF NOT EXISTS activity_answers_country ON activity_shape_answers(country,shape_id);
CREATE TABLE IF NOT EXISTS activity_facts (
 event_id INTEGER PRIMARY KEY REFERENCES activity_events(id) ON DELETE CASCADE,
 router_id TEXT NOT NULL, at INTEGER NOT NULL,
 shape_id INTEGER NOT NULL REFERENCES activity_shapes(id)
);
CREATE INDEX IF NOT EXISTS activity_facts_time ON activity_facts(router_id,at,event_id,shape_id);
CREATE INDEX IF NOT EXISTS activity_facts_shape ON activity_facts(router_id,shape_id,at,event_id);
CREATE TABLE IF NOT EXISTS activity_event_groups (
 router_id TEXT NOT NULL, second INTEGER NOT NULL, shape_id INTEGER NOT NULL,
 kind TEXT NOT NULL,label TEXT NOT NULL,user_label TEXT NOT NULL,
 n INTEGER NOT NULL, first_at INTEGER NOT NULL, last_at INTEGER NOT NULL, first_id INTEGER NOT NULL,
 PRIMARY KEY(router_id,second,shape_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS activity_groups_kind ON activity_event_groups(router_id,kind,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_label ON activity_event_groups(router_id,label,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_user ON activity_event_groups(router_id,user_label,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_time_asc ON activity_event_groups(router_id,second,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_count_asc ON activity_event_groups(router_id,n,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_kind_desc ON activity_event_groups(router_id,kind DESC,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_label_desc ON activity_event_groups(router_id,label DESC,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_user_desc ON activity_event_groups(router_id,user_label DESC,second DESC,shape_id DESC);
CREATE INDEX IF NOT EXISTS activity_groups_shape ON activity_event_groups(router_id,shape_id,second);
CREATE INDEX IF NOT EXISTS activity_groups_count ON activity_event_groups(router_id,n,second,shape_id);
CREATE TABLE IF NOT EXISTS activity_daily (
 router_id TEXT NOT NULL,day INTEGER NOT NULL,shape_id INTEGER NOT NULL,n INTEGER NOT NULL,groups INTEGER NOT NULL,last_at INTEGER NOT NULL,
 PRIMARY KEY(router_id,day,shape_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS activity_daily_shape ON activity_daily(router_id,shape_id,day);
CREATE TABLE IF NOT EXISTS activity_hourly (
 router_id TEXT NOT NULL,hour INTEGER NOT NULL,shape_id INTEGER NOT NULL,n INTEGER NOT NULL,
 PRIMARY KEY(router_id,hour,shape_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS activity_hourly_shape ON activity_hourly(router_id,shape_id,hour);
CREATE TABLE IF NOT EXISTS activity_hour_totals (
 router_id TEXT NOT NULL,hour INTEGER NOT NULL,kind TEXT NOT NULL,n INTEGER NOT NULL,
 PRIMARY KEY(router_id,hour,kind)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS activity_pending (
 event_id INTEGER PRIMARY KEY REFERENCES activity_events(id) ON DELETE CASCADE, router_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS activity_pending_router ON activity_pending(router_id,event_id);
INSERT OR IGNORE INTO activity_index_state(router_id,total)
 SELECT router_id,COUNT(*) FROM activity_events GROUP BY router_id;
INSERT OR IGNORE INTO activity_pending(event_id,router_id)
 SELECT id,router_id FROM activity_events WHERE id NOT IN (SELECT event_id FROM activity_facts);
CREATE TRIGGER IF NOT EXISTS activity_source_insert AFTER INSERT ON activity_events BEGIN
 INSERT INTO activity_pending VALUES(new.id,new.router_id);
 INSERT INTO activity_index_state(router_id,total) VALUES(new.router_id,1)
 ON CONFLICT(router_id) DO UPDATE SET revision=revision+1,total=total+1;
END;
CREATE TRIGGER IF NOT EXISTS activity_source_update AFTER UPDATE OF payload ON activity_events WHEN old.payload IS NOT new.payload BEGIN
 DELETE FROM activity_facts WHERE event_id=new.id;
 INSERT OR IGNORE INTO activity_pending VALUES(new.id,new.router_id);
 UPDATE activity_index_state SET revision=revision+1 WHERE router_id=new.router_id;
END;
CREATE TRIGGER IF NOT EXISTS activity_source_delete AFTER DELETE ON activity_events BEGIN
 UPDATE activity_index_state SET revision=revision+1,total=total-1 WHERE router_id=old.router_id;
END;
CREATE TRIGGER IF NOT EXISTS activity_fact_insert AFTER INSERT ON activity_facts BEGIN
 INSERT INTO activity_event_groups SELECT new.router_id,new.at/1000,new.shape_id,kind,label,user_label,1,new.at,new.at,new.event_id FROM activity_shapes WHERE id=new.shape_id
 ON CONFLICT(router_id,second,shape_id) DO UPDATE SET n=n+1,
 first_at=MIN(first_at,new.at),last_at=MAX(last_at,new.at),first_id=MIN(first_id,new.event_id);
 INSERT INTO activity_daily VALUES(new.router_id,new.at/86400000,new.shape_id,1,
 (SELECT n=1 FROM activity_event_groups WHERE router_id=new.router_id AND second=new.at/1000 AND shape_id=new.shape_id),new.at)
 ON CONFLICT(router_id,day,shape_id) DO UPDATE SET n=n+1,groups=groups+excluded.groups,last_at=MAX(last_at,new.at);
 INSERT INTO activity_hourly VALUES(new.router_id,new.at/3600000,new.shape_id,1)
 ON CONFLICT(router_id,hour,shape_id) DO UPDATE SET n=n+1;
 INSERT INTO activity_hour_totals SELECT new.router_id,new.at/3600000,kind,1 FROM activity_shapes WHERE id=new.shape_id
 ON CONFLICT(router_id,hour,kind) DO UPDATE SET n=n+1;
END;
CREATE TRIGGER IF NOT EXISTS activity_fact_delete AFTER DELETE ON activity_facts BEGIN
 UPDATE activity_event_groups SET n=n-1 WHERE router_id=old.router_id AND second=old.at/1000 AND shape_id=old.shape_id;
 DELETE FROM activity_event_groups WHERE router_id=old.router_id AND second=old.at/1000 AND shape_id=old.shape_id AND n=0;
 UPDATE activity_event_groups SET
 first_id=(SELECT MIN(event_id) FROM activity_facts WHERE router_id=old.router_id AND shape_id=old.shape_id AND at>=old.at/1000*1000 AND at<old.at/1000*1000+1000),
 first_at=(SELECT MIN(at) FROM activity_facts WHERE router_id=old.router_id AND shape_id=old.shape_id AND at>=old.at/1000*1000 AND at<old.at/1000*1000+1000),
 last_at=(SELECT MAX(at) FROM activity_facts WHERE router_id=old.router_id AND shape_id=old.shape_id AND at>=old.at/1000*1000 AND at<old.at/1000*1000+1000)
 WHERE router_id=old.router_id AND second=old.at/1000 AND shape_id=old.shape_id
 AND (first_id=old.event_id OR first_at=old.at OR last_at=old.at);
 UPDATE activity_daily SET n=n-1,groups=groups-(NOT EXISTS(SELECT 1 FROM activity_event_groups WHERE router_id=old.router_id AND second=old.at/1000 AND shape_id=old.shape_id))
 WHERE router_id=old.router_id AND day=old.at/86400000 AND shape_id=old.shape_id;
 DELETE FROM activity_daily WHERE router_id=old.router_id AND day=old.at/86400000 AND shape_id=old.shape_id AND n=0;
 UPDATE activity_daily SET last_at=(SELECT MAX(at) FROM activity_facts WHERE router_id=old.router_id AND shape_id=old.shape_id AND at>=old.at/86400000*86400000 AND at<old.at/86400000*86400000+86400000)
 WHERE router_id=old.router_id AND day=old.at/86400000 AND shape_id=old.shape_id AND last_at=old.at;
 UPDATE activity_hourly SET n=n-1 WHERE router_id=old.router_id AND hour=old.at/3600000 AND shape_id=old.shape_id;
 DELETE FROM activity_hourly WHERE router_id=old.router_id AND hour=old.at/3600000 AND shape_id=old.shape_id AND n=0;
 UPDATE activity_hour_totals SET n=n-1 WHERE router_id=old.router_id AND hour=old.at/3600000 AND kind=(SELECT kind FROM activity_shapes WHERE id=old.shape_id);
 DELETE FROM activity_hour_totals WHERE router_id=old.router_id AND hour=old.at/3600000 AND n=0;
END;
`
