package subscriber

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Synthetic store-read benchmark, not HTTP, authentication or NAS acceptance.
// Direct fixture insertion excludes password hashing and issuance from setup.
func BenchmarkStatusHistoryScale(b *testing.B) {
	for _, count := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprintf("%d-subscribers", count), func(b *testing.B) {
			h, err := sql.Open("sqlite", protocolFixtureURI(filepath.Join(b.TempDir(), "ledger.db")))
			if err != nil {
				b.Fatal(err)
			}
			defer h.Close()
			h.SetMaxOpenConns(1)
			if _, err = h.Exec(Schema); err != nil {
				b.Fatal(err)
			}
			tx, err := h.Begin()
			if err != nil {
				b.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?)
    INSERT INTO subscriber_accounts(id,username,name,salt,password_hash,status,created_at)
    SELECT 'synthetic-account-'||i,'synthetic-user-'||i,'Synthetic fixture','','','active',0 FROM n`, count); err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO subscriber_audit(actor,action,target,reason,created_at)
    SELECT 'synthetic-admin','account.active',id,'Synthetic scale fixture',0 FROM subscriber_accounts`); err != nil {
				b.Fatal(err)
			}
			if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<100)
    INSERT INTO subscriber_audit(actor,action,target,reason,created_at)
    SELECT 'synthetic-admin','account.suspended','synthetic-account-1','Synthetic page fixture',i FROM n`); err != nil {
				b.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			s := NewStore(h)
			var actual int
			if err = h.QueryRow(`SELECT COUNT(*) FROM subscriber_accounts`).Scan(&actual); err != nil || actual != count {
				b.Fatal("fixture size", actual, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := s.StatusHistory(b.Context(), "synthetic-account-1", 0)
				if err != nil || len(page.Events) != 50 || page.NextBefore == nil {
					b.Fatal("bounded page", len(page.Events), err)
				}
			}
		})
	}
}
