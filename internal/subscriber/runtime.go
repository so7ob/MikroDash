package subscriber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sync"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"mikrodash/internal/guard"
)

type Config struct {
	Mode                string   `json:"mode"`
	PortalListen        string   `json:"portalListen"`
	PortalOrigin        string   `json:"portalOrigin"`
	TLSCert             string   `json:"tlsCert"`
	TLSKey              string   `json:"tlsKey"`
	AuthListen          string   `json:"authListen"`
	AccountingListen    string   `json:"accountingListen"`
	Clients             []NAS    `json:"clients"`
	PilotAccounts       []string `json:"pilotAccounts"`
	AllowCoA            bool     `json:"allowCoA"`
	AllowDisconnect     bool     `json:"allowDisconnect"`
	LocalLimitsVerified bool     `json:"localLimitsVerified"`
}

func (c Config) Validate() error {
	if !slices.Contains([]string{"disabled", "shadow", "pilot", "managed"}, c.Mode) {
		return errors.New("invalid subscriber mode")
	}
	if c.Mode == "disabled" {
		return nil
	}
	u, err := url.Parse(c.PortalOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("portal origin must be an HTTPS origin")
	}
	if c.PortalListen == "" || c.TLSCert == "" || c.TLSKey == "" {
		return errors.New("separate portal TLS listener required")
	}
	if (c.Mode == "pilot" || c.Mode == "managed") && (!c.LocalLimitsVerified || !c.AllowDisconnect) {
		return errors.New("managed admission requires verified local limits and explicit disconnect grant")
	}
	if c.Mode == "pilot" && (len(c.PilotAccounts) != 1 || len(c.Clients) != 1) {
		return errors.New("pilot requires exactly one account and network")
	}
	peers := map[string]string{}
	networks := map[string]bool{}
	for _, n := range c.Clients {
		if previous, ok := peers[n.Address]; ok && previous != n.RouterID {
			return errors.New("one router identity per NAS source address is required")
		}
		key := n.RouterID + ":" + n.CalledStationID
		if networks[key] || n.Network == "" || n.CalledStationID == "" {
			return errors.New("enrolled HotSpot network identity must be unique")
		}
		peers[n.Address] = n.RouterID
		networks[key] = true
		u, err := url.Parse(n.LoginURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "/login" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("NAS login URL must be an explicit HTTPS /login endpoint")
		}
		if n.DisconnectPort < 1 || n.DisconnectPort > 65535 {
			return errors.New("invalid disconnect port")
		}
	}
	return nil
}

type Enforcement struct {
	Username  string
	StartedAt time.Time
	LeaseID   string
	AccountID string
	RouterID  string
	Network   string
	Device    string
	SessionID string
	Reason    string
}

func (s *Store) Due(ctx context.Context, at time.Time) ([]Enforcement, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT l.id,s.account_id,l.router_id,l.network,l.device,l.session_id,l.deadline,a.status FROM subscriber_leases l JOIN subscriber_subscriptions s ON s.id=l.subscription_id JOIN subscriber_accounts a ON a.id=s.account_id WHERE l.state!='stopped'`)
	if err != nil {
		return nil, err
	}
	all := []Enforcement{}
	reasons := map[string]string{}
	for rows.Next() {
		var e Enforcement
		var deadline int64
		var status string
		if err = rows.Scan(&e.LeaseID, &e.AccountID, &e.RouterID, &e.Network, &e.Device, &e.SessionID, &deadline, &status); err != nil {
			rows.Close()
			return nil, err
		}
		if deadline <= at.Unix() {
			e.Reason = "delegation-expired"
		}
		if status != "active" {
			e.Reason = "account-suspended"
		}
		all = append(all, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []Enforcement{}
	for _, e := range all {
		if e.Reason == "" {
			reason, ok := reasons[e.AccountID]
			if !ok {
				snap, serr := s.Snapshot(ctx, e.AccountID, at)
				if serr != nil {
					return nil, serr
				}
				// Admission reservations do not represent consumed quota. Re-evaluate
				// enforcement using only durable usage, not other devices' reservations.
				usage := map[string]Usage{}
				for _, v := range snap.Decision.Limits {
					usage[Key(v.Limit.ID, v.PeriodStart)] = Usage{Used: v.Used, Quality: v.Quality}
				}
				d := Evaluate(snap.Subscription.Plan, snap.Subscription.SpeedID, at, snap.Subscription.ExpiresAt, usage)
				if d.Disconnect {
					reason = "policy-disconnect"
				}
				reasons[e.AccountID] = reason
			}
			e.Reason = reason
		}
		if e.Reason != "" && e.SessionID != "" {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *Store) RecordEnforcement(ctx context.Context, e Enforcement, result string) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = audit(tx, "service:subscriber", "session.disconnect."+result, e.LeaseID, e.Reason, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// Run owns enforcement independently of every browser, page and collector.
// Outstanding leases survive restarts; a missing Stop retains its reservation
// and blocks unsafe re-admission rather than inventing unused credit.
func (s *RadiusService) Run(ctx context.Context, allowDisconnect bool, report func(error)) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	last := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.Tick(ctx, now); err != nil {
				report(err)
			}
			due, err := s.Store.Due(ctx, now)
			if err != nil {
				report(err)
				continue
			}
			for _, e := range due {
				if now.Sub(last[e.LeaseID]) < 10*time.Second {
					continue
				}
				last[e.LeaseID] = now
				if s.Mode == "shadow" {
					continue
				}
				err = s.Disconnect(ctx, e, allowDisconnect)
				if err != nil {
					report(err)
				}
			}
		}
	}
}
func (s *RadiusService) Disconnect(ctx context.Context, e Enforcement, allowed bool) error {
	var client NAS
	for _, n := range s.Clients {
		if n.RouterID == e.RouterID && n.Network == e.Network {
			client = n
		}
	}
	grant := guard.SubscriberAuthority{Identity: "service:subscriber", Mode: s.Mode, RouterID: client.RouterID, Network: client.Network, Disconnect: allowed}
	if !guard.CheckSubscriberDisconnect(grant, e.RouterID, e.Network, e.LeaseID, e.SessionID) {
		return ErrDenied
	}
	var started int64
	if err := s.Store.sql.QueryRowContext(ctx, `SELECT a.username,l.started_at FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id JOIN subscriber_accounts a ON a.id=sub.account_id WHERE l.id=? AND a.id=? AND l.router_id=? AND l.network=? AND l.device=? AND l.session_id=? AND l.state!='stopped'`, e.LeaseID, e.AccountID, e.RouterID, e.Network, e.Device, e.SessionID).Scan(&e.Username, &started); err != nil {
		return ErrDenied
	}
	e.StartedAt = time.Unix(started, 0)
	if s.VerifySession == nil || !s.VerifySession(ctx, e) {
		return errors.New("live session identity could not be verified")
	}
	// Record the intent before a network write. If audit is unavailable, refuse.
	if err := s.Store.RecordEnforcement(ctx, e, "requested"); err != nil {
		return err
	}
	p := radius.New(radius.CodeDisconnectRequest, []byte(client.Secret))
	if err := rfc2866.AcctSessionID_SetString(p, e.SessionID); err != nil {
		return err
	}
	if err := rfc2865.CallingStationID_SetString(p, e.Device); err != nil {
		return err
	}
	if err := rfc2865.UserName_SetString(p, e.Username); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := radius.Exchange(deadline, p, net.JoinHostPort(client.Address, fmt.Sprint(client.DisconnectPort)))
	result := "failed"
	if err == nil && response.Code == radius.CodeDisconnectACK {
		result = "acknowledged"
	}
	if aerr := s.Store.RecordEnforcement(ctx, e, result); aerr != nil {
		return aerr
	}
	if result != "acknowledged" {
		return errors.New("router did not acknowledge disconnect")
	}
	return nil
}

// SessionRegistry holds portal sessions separately from administrator sessions.
// Only hashes of bearer tokens are retained. Restart signs out portal browsers;
// it does not remove durable network leases or accounting events.
type SessionRegistry struct {
	mu     sync.Mutex
	tokens map[string]PortalSession
}
type PortalSession struct {
	AccountID string
	CSRF      string
	Expires   time.Time
}

func (s *SessionRegistry) New(account string) (string, PortalSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]PortalSession{}
	}
	now := time.Now()
	for k, v := range s.tokens {
		if now.After(v.Expires) {
			delete(s.tokens, k)
		}
	}
	token := ID()
	v := PortalSession{AccountID: account, CSRF: ID(), Expires: now.Add(8 * time.Hour)}
	s.tokens[Digest(token)] = v
	return token, v
}
func (s *SessionRegistry) Get(token string) (PortalSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.tokens[Digest(token)]
	return v, ok && time.Now().Before(v.Expires)
}
func (s *SessionRegistry) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, Digest(token))
}

func DecodeConfig(raw []byte) (Config, error) {
	var c Config
	err := json.Unmarshal(raw, &c)
	if err != nil {
		return c, errors.New("invalid subscriber configuration")
	}
	return c, c.Validate()
}
