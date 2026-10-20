package subscriberlab

import (
	"os"
	"path/filepath"
	"testing"

	"mikrodash/internal/db"
	"mikrodash/internal/rbac"
	"mikrodash/internal/store"
)

func TestRuntimeDataPreparationEnrollsAdministratorWithoutResettingData(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateJSON(filepath.Join(dir, "credentials.json"), credentials{"admin", "synthetic-router-password-only-123456"}); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateJSON(filepath.Join(dir, "fixture.json"), FixtureCredentials{Secret: "synthetic-radius-secret-for-tests-only", Password: "synthetic-subscriber-password-only"}); err != nil {
		t.Fatal(err)
	}
	c := &Client{state: State{Name: "synthetic-vm", APIPort: 18728}}
	if err := c.prepareRuntimeData(dir); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "runtime-data")
	st, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	users, err := st.Users()
	if err != nil || len(users) != 1 {
		t.Fatal(err)
	}
	resolver := rbac.New(database, func() []rbac.Router { return nil })
	if allowed, err := resolver.Can(users[0].ID, "system:subscribers", ""); err != nil || !allowed {
		t.Fatal("runtime preparation forgot administrator grant", allowed, err)
	}
	var cfg map[string]any
	if err := privateJSON(filepath.Join(dir, "runtime-config.json"), &cfg); err != nil || cfg["mode"] != "shadow" || cfg["localLimitsVerified"] != false {
		t.Fatal("preparation bypassed capability acceptance", err)
	}
	accounts, err := database.Subscribers().Accounts(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatal(err)
	}
	if err := c.prepareRuntimeData(dir); err == nil {
		t.Fatal("existing ledger overwritten on repeated preparation")
	}
	after, err := database.Subscribers().Accounts(t.Context())
	if err != nil || len(after) != 1 || after[0].ID != accounts[0].ID {
		t.Fatal("refused repeat changed the existing account", err)
	}
}

func TestRuntimeAdminUsesExistingRBACGrantAndRefusesOtherUsers(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := grantRuntimeAdmin(st, database); err == nil {
		t.Fatal("empty fixture accepted")
	}
	if _, err = st.CreateUser(store.NewUser{Username: "chr-lab-admin", Role: "admin", Password: "synthetic-only-password"}); err != nil {
		t.Fatal(err)
	}
	users, err := st.Users()
	if err != nil || len(users) != 1 {
		t.Fatal(err)
	}
	resolver := rbac.New(database, func() []rbac.Router { return nil })
	if allowed, err := resolver.Can(users[0].ID, "system:subscribers", ""); err != nil || allowed {
		t.Fatal("legacy role alone granted subscriber administration", allowed, err)
	}
	if err := grantRuntimeAdmin(st, database); err != nil {
		t.Fatal(err)
	}
	if allowed, err := resolver.Can(users[0].ID, "system:subscribers", ""); err != nil || !allowed {
		t.Fatal("prepared administrator cannot use the real subscriber API", allowed, err)
	}
	if _, err = st.CreateUser(store.NewUser{Username: "another-user", Role: "admin", Password: "synthetic-only-password"}); err != nil {
		t.Fatal(err)
	}
	if err := grantRuntimeAdmin(st, database); err == nil {
		t.Fatal("non-fixture user set accepted for grant repair")
	}
}
