package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiteralURICharactersInDataPath(t *testing.T) {
	for _, name := range []string{"seed#0", "data?mode=memory", "percent%20 space", "اختبار"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			d, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = d.sql.Exec(`CREATE TABLE path_proof(value INTEGER); INSERT INTO path_proof VALUES(42)`); err != nil {
				t.Fatal(err)
			}
			if err = d.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(dir, "mikrodash.db")); err != nil {
				t.Fatal("database was not created at the requested literal path", err)
			}
			d, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			var value int
			if err = d.sql.QueryRow(`SELECT value FROM path_proof`).Scan(&value); err != nil || value != 42 {
				t.Fatal("reopen did not retain exact-path data", err)
			}
		})
	}
}
