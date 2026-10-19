package db

import "testing"

func TestSubscriberMigrationFrom37PreservesExistingData(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, table := range []string{"subscriber_tickets", "subscriber_events", "subscriber_reservations", "subscriber_buckets", "subscriber_leases", "subscriber_subscriptions", "subscriber_plans", "subscriber_accounts", "subscriber_audit"} {
		if _, err = d.sql.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = d.sql.Exec(`DELETE FROM schema_version WHERE version>37`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = d.sql.QueryRow(`SELECT count(*) FROM roles`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	n, err := d.Migrate()
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err = d.Subscribers().CheckReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = d.sql.QueryRow(`SELECT count(*) FROM roles`).Scan(&after); err != nil || before != after {
		t.Fatal("existing roles changed")
	}
	n, err = d.Migrate()
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
