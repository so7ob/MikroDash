package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func integrationSchema(t *testing.T, d *DB) string {
	t.Helper()
	rows, err := d.sql.Query(`SELECT name,sql FROM sqlite_master WHERE type IN ('table','index') AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s: %s\n", name, sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestUpstreamMigrationsPreserveForkData(t *testing.T) {
	fresh := openTest(t, t.TempDir())
	want := integrationSchema(t, fresh)
	for _, table := range []string{"activity_events", "activity_checkpoints", "dns_lists", "watch_devices", "wifi_samples", "wifi_history_routers"} {
		if !strings.Contains(want, table+":") {
			t.Fatalf("fresh schema lacks %s", table)
		}
	}
	for _, version := range []int{34, 35} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			dir := t.TempDir()
			d := openTest(t, dir)
			settings := []byte(`{"dnsBulkEnabled":false,"brandName":"Synthetic"}`)
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			cfgExec(t, d, `DROP TABLE dns_lists`, `DROP TABLE watch_devices`, `DROP TABLE wifi_samples`, `DROP TABLE wifi_history_routers`, fmt.Sprintf(`DELETE FROM schema_version WHERE version>%d`, version))
			if version == 34 {
				for _, table := range []string{"activity_settings", "activity_checkpoints", "activity_events", "activity_rules", "activity_coverage"} {
					cfgExec(t, d, "DROP TABLE "+table)
				}
			} else {
				cfgExec(t, d, `INSERT INTO activity_settings(router_id,enabled,timezone,folder) VALUES('synthetic',1,'UTC','usb1/log')`,
					`INSERT INTO activity_events(router_id,source_key,at,received,kind,payload) VALUES('synthetic','one',1,2,'dns','{"domain":"example.test"}')`,
					`INSERT INTO activity_checkpoints(router_id,generation,payload) VALUES('synthetic','one','{"offset":123}')`)
			}
			cfgExec(t, d, `INSERT INTO grants(id,principal_type,principal_id,scope_type,scope_id,role_id,created_at) SELECT 'preserved','user','synthetic','router','synthetic',id,1 FROM roles WHERE builtin=1 LIMIT 1`)
			var before int
			if err := d.sql.QueryRow(`SELECT count(*) FROM grants WHERE id='preserved'`).Scan(&before); err != nil || before != 1 {
				t.Fatalf("grant fixture missing: %d %v", before, err)
			}
			n, err := d.Migrate()
			if err != nil || n != 37-version {
				t.Fatalf("migrate = %d %v", n, err)
			}
			if got := integrationSchema(t, d); got != want {
				t.Fatalf("migrated schema differs from fresh schema")
			}
			if v, err := d.SchemaVersion(); err != nil || v != 37 {
				t.Fatalf("version %d %v", v, err)
			}
			var grants int
			if err := d.sql.QueryRow(`SELECT count(*) FROM grants WHERE id='preserved'`).Scan(&grants); err != nil || grants != 1 {
				t.Fatal("grant changed", err)
			}
			if version == 35 {
				var enabled int
				var folder, payload, checkpoint string
				if err := d.sql.QueryRow(`SELECT enabled,folder FROM activity_settings WHERE router_id='synthetic'`).Scan(&enabled, &folder); err != nil || enabled != 1 || folder != "usb1/log" {
					t.Fatal("Activity settings changed", err)
				}
				if err := d.sql.QueryRow(`SELECT payload FROM activity_events WHERE source_key='one'`).Scan(&payload); err != nil || payload != `{"domain":"example.test"}` {
					t.Fatal("Activity event changed", err)
				}
				if err := d.sql.QueryRow(`SELECT payload FROM activity_checkpoints WHERE generation='one'`).Scan(&checkpoint); err != nil || checkpoint != `{"offset":123}` {
					t.Fatal("checkpoint changed", err)
				}
			}
			got, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil || string(got) != string(settings) {
				t.Fatal("settings file changed", err)
			}
			if n, err := d.Migrate(); err != nil || n != 0 {
				t.Fatalf("second migration %d %v", n, err)
			}
			cfgExec(t, d, `DELETE FROM schema_version WHERE version>=36`)
			if n, err := d.Migrate(); err != nil || n != 2 {
				t.Fatalf("replay %d %v", n, err)
			}
		})
	}
}
