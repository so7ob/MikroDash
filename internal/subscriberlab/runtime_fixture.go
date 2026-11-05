package subscriberlab

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"mikrodash/internal/db"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
)

// PrepareRuntime creates fresh synthetic data for the actual mikrodash binary.
// It starts in Shadow; verified local-limit probes are required before Pilot.
func (c *Client) PrepareRuntime(privateDir string) error {
	if privateDir != filepath.Dir(c.state.Console) || !c.state.NativePortal || os.Getenv("DATA_SECRET") != "" {
		return errors.New("isolated native portal and independent data key required")
	}
	if _, err := c.Identity(); err != nil {
		return err
	}
	return c.prepareRuntimeData(privateDir)
}

// prepareRuntimeData is separated from hardware ownership checks so tests can
// exercise the actual fixture preparation, encryption and RBAC call sites.
func (c *Client) prepareRuntimeData(privateDir string) error {
	var auth credentials
	var fixture FixtureCredentials
	if err := privateJSON(filepath.Join(privateDir, "credentials.json"), &auth); err != nil {
		return err
	}
	if err := privateJSON(filepath.Join(privateDir, "fixture.json"), &fixture); err != nil {
		return err
	}
	data := filepath.Join(privateDir, "runtime-data")
	if err := os.Mkdir(data, 0700); err != nil {
		return errors.New("fresh runtime-data required; existing ledgers are never overwritten")
	}
	st, err := store.Open(data)
	if err != nil {
		return err
	}
	adminPassword := subscriber.ID()
	if _, err = st.CreateUser(store.NewUser{Username: "chr-lab-admin", Password: adminPassword, Role: "admin"}); err != nil {
		return err
	}
	router, err := st.AddRouter(map[string]any{"label": "Synthetic owned CHR", "host": "127.0.0.1", "port": c.state.APIPort, "tls": false, "username": auth.Username, "password": auth.Password, "alertsEnabled": false})
	if err != nil {
		return err
	}
	database, err := db.Open(data)
	if err != nil {
		return err
	}
	defer database.Close()
	if err = grantRuntimeAdmin(st, database); err != nil {
		return err
	}
	ledger := database.Subscribers()
	ctx := context.Background()
	account, _, err := ledger.CreateAccount(ctx, "test:subscriber-chr", "chr-lab", "Synthetic CHR subscriber", fixture.Password, "isolated runtime lab", 0)
	if err != nil {
		return err
	}
	now := time.Now()
	plan := subscriber.Plan{ID: "chr-runtime", Version: 1, Name: "Synthetic CHR runtime", Start: "first-login", ValidSeconds: 86400,
		MaxSessions: 1, Networks: []string{"C"}, Down: 512000, Up: 128000,
		Speeds: []subscriber.Speed{{ID: "economy", Name: "Economy", Down: 128000, Up: 64000}, {ID: "fast", Name: "Fast", Down: 512000, Up: 128000}},
		Policies: []subscriber.Policy{{ID: "all", Limits: []subscriber.Limit{
			{ID: "time", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
			{ID: "download", Kind: "download", State: "limited", Amount: 16 << 20, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
			{ID: "upload", Kind: "upload", State: "limited", Amount: 4 << 20, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
		}}}}
	if err = ledger.SavePlan(ctx, "test:subscriber-chr", "isolated runtime lab", plan); err != nil {
		return err
	}
	if _, err = ledger.Assign(ctx, "test:subscriber-chr", account.ID, plan.ID, 1, "isolated runtime lab", now); err != nil {
		return err
	}
	if err = createNativePortalTLS(privateDir); err != nil {
		return err
	}
	sealed, err := st.Encrypt(fixture.Secret)
	if err != nil {
		return err
	}
	cfg := subscriber.Config{Mode: "shadow", PortalListen: "198.51.100.10:9444", PortalOrigin: "https://portal.example.test:9444",
		TLSCert: filepath.Join(privateDir, "portal-chain.pem"), TLSKey: filepath.Join(privateDir, "portal-key.pem"),
		AuthListen: "127.0.0.1:12812", AccountingListen: "127.0.0.1:12813", PilotAccounts: []string{account.ID},
		Clients: []subscriber.NAS{{RouterID: router.ID, Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", SecretSealed: sealed, DisconnectPort: 13799, LoginURL: "https://login.example.test/login"}}}
	if err = cfg.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]any{
		"runtime-config.json":   cfg,
		"admin-login.json":      map[string]string{"username": "chr-lab-admin", "password": adminPassword},
		"subscriber-login.json": map[string]string{"username": account.Username, "password": fixture.Password},
		"runtime-fixture.json":  map[string]string{"vm": c.state.Name, "data": data, "accountId": account.ID, "routerId": router.ID},
	} {
		if err = writePrivateJSON(filepath.Join(privateDir, name), value); err != nil {
			return err
		}
	}
	return nil
}

// RepairRuntimeAdmin enrolls only the fixture's sole synthetic administrator in
// the existing RBAC store. A legacy role string alone is not a permission grant.
func (c *Client) RepairRuntimeAdmin(privateDir string) error {
	if _, err := c.Identity(); err != nil {
		return err
	}
	var fixture map[string]string
	if err := privateJSON(filepath.Join(privateDir, "runtime-fixture.json"), &fixture); err != nil {
		return err
	}
	data := filepath.Join(privateDir, "runtime-data")
	if privateDir != filepath.Dir(c.state.Console) || fixture["vm"] != c.state.Name || fixture["data"] != data {
		return errors.New("owned runtime fixture required")
	}
	st, err := store.Open(data)
	if err != nil {
		return err
	}
	database, err := db.Open(data)
	if err != nil {
		return err
	}
	defer database.Close()
	return grantRuntimeAdmin(st, database)
}

func grantRuntimeAdmin(st *store.Store, database *db.DB) error {
	users, err := st.Users()
	if err != nil || len(users) != 1 || users[0].Username != "chr-lab-admin" || users[0].Role != "admin" || users[0].ID == "" {
		return errors.New("sole synthetic runtime administrator required")
	}
	return database.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: users[0].ID, RoleID: "administrator", ScopeType: "global", CreatedBy: "test:subscriber-chr"})
}

func writePrivateJSON(path string, value any) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(value)
}

func createNativePortalTLS(dir string) error {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic CHR portal CA"},
		IsCA: true, BasicConstraintsValid: true, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageCertSign}
	root, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "portal.example.test"}, DNSNames: []string{"portal.example.test"},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root})
	for name, raw := range map[string][]byte{
		"portal-ca.crt":    rootPEM,
		"portal-chain.pem": append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), rootPEM...),
		"portal-key.pem":   pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}),
	} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(raw)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return errors.New("cannot retain synthetic native TLS identity")
		}
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return writePrivateJSON(filepath.Join(dir, "portal-pin.json"), map[string]string{"sha256SPKI": base64.StdEncoding.EncodeToString(digest[:])})
}
