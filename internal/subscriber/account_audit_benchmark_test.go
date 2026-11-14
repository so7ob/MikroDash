package subscriber

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// Synthetic population only. This measures the actual owned, indexed SQLite
// reader with one selected subscription, not HTTP, cold disk or runtime RSS.
func BenchmarkAccountAuditUnrelatedScale(b *testing.B) {
	for _, n := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			handle, err := sql.Open("sqlite", protocolFixtureURI(filepath.Join(b.TempDir(), "audit-scale.db")))
			if err != nil {
				b.Fatal(err)
			}
			defer handle.Close()
			handle.SetMaxOpenConns(1)
			if _, err = handle.Exec(Schema); err != nil {
				b.Fatal(err)
			}
			if _, err = handle.Exec(`INSERT INTO subscriber_accounts VALUES('synthetic-selected','synthetic-selected','Synthetic selected','synthetic','synthetic',NULL,'active',0);
INSERT INTO subscriber_plans VALUES('synthetic-scale-plan',1,'{}');
INSERT INTO subscriber_subscriptions(id,account_id,plan_id,plan_version,assigned_at,status,speed_id) VALUES('synthetic-selected-sub','synthetic-selected','synthetic-scale-plan',1,0,'active','synthetic');`); err != nil {
				b.Fatal(err)
			}
			tx, err := handle.Begin()
			if err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO subscriber_accounts SELECT 'synthetic-account-'||x,'synthetic-user-'||x,'Synthetic unrelated','synthetic','synthetic',NULL,'active',0 FROM n`, n); err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO subscriber_subscriptions(id,account_id,plan_id,plan_version,assigned_at,status,speed_id) SELECT 'synthetic-sub-'||id,id,'synthetic-scale-plan',1,0,'active','synthetic' FROM subscriber_accounts WHERE id!='synthetic-selected'`); err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) SELECT row_number() OVER(),'synthetic-admin','account.create',id,'Synthetic unrelated',0 FROM subscriber_accounts WHERE id!='synthetic-selected'`); err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) SELECT ?+row_number() OVER(),'synthetic-admin','subscription.assign',id,'Synthetic unrelated',0 FROM subscriber_subscriptions WHERE account_id!='synthetic-selected'`, n); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 60; i++ {
				action, target := "account.active", "synthetic-selected"
				if i%2 == 1 {
					action, target = "speed.request", "synthetic-selected-sub"
				}
				if _, err = tx.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,'synthetic-admin',?,?, 'Synthetic selected',0)`, n*2+1000+i, action, target); err != nil {
					b.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			var accounts, events int
			if handle.QueryRow(`SELECT count(*) FROM subscriber_accounts`).Scan(&accounts) != nil || accounts != n+1 {
				b.Fatal("wrong population")
			}
			if handle.QueryRow(`SELECT count(*) FROM subscriber_audit`).Scan(&events) != nil || events != n*2+60 {
				b.Fatal("wrong audit population")
			}
			store := NewStore(handle)
			query := AccountAuditQuery{AccountID: "synthetic-selected"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := store.AccountAudit(b.Context(), query)
				if err != nil || len(page.Events) != 50 || page.NextBefore == nil || page.Events[0].ID != Quantity(n*2+1059) {
					b.Fatal("incorrect measured page", err)
				}
			}
		})
	}
}
