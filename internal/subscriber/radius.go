package subscriber

import (
	"context"
	"crypto/hmac"
	"crypto/md5" // Required by RADIUS Message-Authenticator (RFC 2869 section 5.14).
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"
)

type NAS struct {
	Capabilities    Capabilities `json:"capabilities"`
	RouterID        string       `json:"routerId"`
	Network         string       `json:"network"`
	Address         string       `json:"address"`
	CalledStationID string       `json:"calledStationId"`
	Secret          string       `json:"-"`
	SecretSealed    string       `json:"secretSealed"`
	DisconnectPort  int          `json:"disconnectPort"`
	LoginURL        string       `json:"loginUrl"`
}

type RadiusService struct {
	Observe       func(context.Context, string) (RouterObservation, error)
	AllowCoA      bool
	Store         *Store
	VerifySession func(context.Context, Enforcement) bool
	Mode          string
	Clients       []NAS
	PilotAccounts map[string]bool
	mu            sync.Mutex
	servers       []*radius.PacketServer
	authSlots     chan struct{}
	attempts      map[string]attempt
}

var ErrAuthenticationBusy = errors.New("subscriber authentication is busy")

// Authenticate bounds the memory used by concurrent scrypt work across the
// portal and RADIUS. Rate limits alone do not bound an allowed initial burst.
func (s *RadiusService) Authenticate(ctx context.Context, username, password string) (Account, error) {
	s.mu.Lock()
	if s.authSlots == nil {
		s.authSlots = make(chan struct{}, 2)
	}
	slots := s.authSlots
	s.mu.Unlock()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return Account{}, ctx.Err()
	default:
		return Account{}, ErrAuthenticationBusy
	}
	return s.Store.Authenticate(ctx, username, password)
}

type attempt struct {
	Since time.Time
	Count int
}

func (s *RadiusService) client(addr net.Addr) (NAS, bool) {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return NAS{}, false
	}
	for _, c := range s.Clients {
		if c.Address == host {
			return c, true
		}
	}
	return NAS{}, false
}
func (s *RadiusService) RADIUSSecret(ctx context.Context, addr net.Addr) ([]byte, error) {
	c, ok := s.client(addr)
	if !ok {
		return nil, ErrDenied
	}
	return []byte(c.Secret), nil
}
func (s *RadiusService) limited(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts == nil {
		s.attempts = map[string]attempt{}
	}
	now := time.Now()
	if len(s.attempts) > 4096 {
		for k, v := range s.attempts {
			if now.Sub(v.Since) > time.Minute {
				delete(s.attempts, k)
			}
		}
		if len(s.attempts) > 4096 {
			return true
		}
	}
	a := s.attempts[key]
	if now.Sub(a.Since) > time.Minute {
		a = attempt{Since: now}
	}
	a.Count++
	s.attempts[key] = a
	return a.Count > 120
}

// Access-Request's random authenticator does not authenticate the sender.
// Require exactly one HMAC Message-Authenticator even for PAP; the library
// validates Accounting-Request's keyed Request Authenticator before this handler.
func validMessageAuthenticator(p *radius.Packet) bool {
	values, err := rfc2869.MessageAuthenticator_Gets(p)
	if err != nil || len(values) != 1 || len(values[0]) != 16 {
		return false
	}
	raw, err := p.MarshalBinary()
	if err != nil {
		return false
	}
	q, err := radius.Parse(raw, p.Secret)
	if err != nil {
		return false
	}
	if err = rfc2869.MessageAuthenticator_Set(q, make([]byte, 16)); err != nil {
		return false
	}
	raw, err = q.MarshalBinary()
	if err != nil {
		return false
	}
	mac := hmac.New(md5.New, p.Secret)
	mac.Write(raw)
	return hmac.Equal(values[0], mac.Sum(nil))
}
func signResponse(p *radius.Packet) error {
	if err := rfc2869.MessageAuthenticator_Set(p, make([]byte, 16)); err != nil {
		return err
	}
	b, err := p.MarshalBinary()
	if err != nil {
		return err
	}
	mac := hmac.New(md5.New, p.Secret)
	mac.Write(b)
	return rfc2869.MessageAuthenticator_Set(p, mac.Sum(nil))
}
func (s *RadiusService) ServeRADIUS(w radius.ResponseWriter, r *radius.Request) {
	c, ok := s.packetClient(r.RemoteAddr, r.Packet)
	if !ok {
		return
	}
	p := r.Packet
	if p.Code == radius.CodeAccountingRequest {
		s.account(w, r, c)
		return
	}
	if p.Code != radius.CodeAccessRequest || !validMessageAuthenticator(p) || s.limited(c.RouterID) {
		return
	}
	reject := func() {
		out := r.Response(radius.CodeAccessReject)
		if signResponse(out) == nil {
			_ = w.Write(out)
		}
	}
	if s.Mode != "pilot" && s.Mode != "managed" {
		reject()
		return
	}
	if rfc2865.CalledStationID_GetString(p) != c.CalledStationID {
		reject()
		return
	}
	// PAP only. CHAP cannot be verified using a one-way scrypt credential.
	if _, err := rfc2865.CHAPPassword_Lookup(p); err == nil {
		reject()
		return
	}
	password, err := rfc2865.UserPassword_LookupString(p)
	if err != nil {
		reject()
		return
	}
	user := rfc2865.UserName_GetString(p)
	device := rfc2865.CallingStationID_GetString(p)
	request := Digest(c.RouterID + ":" + hex.EncodeToString(p.Authenticator[:]) + ":" + fmt.Sprint(p.Identifier))
	var a Account
	if strings.HasPrefix(password, "mdt_") {
		a, err = s.Store.RedeemTicket(r.Context(), password, c.Network, device, request, time.Now())
	} else {
		a, err = s.Authenticate(r.Context(), user, password)
	}
	if err != nil || a.Username != user || (s.Mode == "pilot" && !s.PilotAccounts[a.ID]) {
		reject()
		return
	}
	admitted, err := s.Store.Reserve(r.Context(), a.ID, c.RouterID, c.Network, device, request, time.Now())
	if err != nil {
		reject()
		return
	}
	out := r.Response(radius.CodeAccessAccept)
	err = acceptAttributes(out, admitted)
	if err != nil || signResponse(out) != nil {
		return
	}
	_ = w.Write(out)
}
func acceptAttributes(p *radius.Packet, a Admission) error {
	for _, err := range []error{
		rfc2865.Class_SetString(p, a.LeaseID),
		rfc2865.SessionTimeout_Set(p, rfc2865.SessionTimeout(a.Decision.Timeout)),
		rfc2865.PortLimit_Set(p, 1),
		rfc2869.AcctInterimInterval_Set(p, 60),
		mikrotik.MikrotikRateLimit_SetString(p, fmt.Sprintf("%d/%d", a.Decision.Up, a.Decision.Down)),
		mikrotik.MikrotikRecvLimit_Set(p, mikrotik.MikrotikRecvLimit(uint64(a.Upload)&0xffffffff)),
		mikrotik.MikrotikRecvLimitGigawords_Set(p, mikrotik.MikrotikRecvLimitGigawords(uint64(a.Upload)>>32)),
		mikrotik.MikrotikXmitLimit_Set(p, mikrotik.MikrotikXmitLimit(uint64(a.Download)&0xffffffff)),
		mikrotik.MikrotikXmitLimitGigawords_Set(p, mikrotik.MikrotikXmitLimitGigawords(uint64(a.Download)>>32)),
	} {
		if err != nil {
			return err
		}
	}
	if a.Total > 0 {
		if err := mikrotik.MikrotikTotalLimit_Set(p, mikrotik.MikrotikTotalLimit(uint64(a.Total)&0xffffffff)); err != nil {
			return err
		}
		return mikrotik.MikrotikTotalLimitGigawords_Set(p, mikrotik.MikrotikTotalLimitGigawords(uint64(a.Total)>>32))
	}
	return nil
}
func (s *RadiusService) account(w radius.ResponseWriter, r *radius.Request, c NAS) {
	p := r.Packet
	status := ""
	switch rfc2866.AcctStatusType_Get(p) {
	case rfc2866.AcctStatusType_Value_Start:
		status = "start"
	case rfc2866.AcctStatusType_Value_InterimUpdate:
		status = "interim"
	case rfc2866.AcctStatusType_Value_Stop:
		status = "stop"
	default:
		return
	}
	// Input octets enter the NAS from the subscriber: upload. Output is download.
	up := uint64(rfc2869.AcctInputGigawords_Get(p))<<32 | uint64(rfc2866.AcctInputOctets_Get(p))
	down := uint64(rfc2869.AcctOutputGigawords_Get(p))<<32 | uint64(rfc2866.AcctOutputOctets_Get(p))
	if up > 1<<63-1 || down > 1<<63-1 {
		return
	}
	when := time.Now()
	delay := time.Duration(rfc2866.AcctDelayTime_Get(p)) * time.Second
	when = when.Add(-delay)
	e := Accounting{LeaseID: rfc2865.Class_GetString(p), RouterID: c.RouterID, Network: c.Network, Device: rfc2865.CallingStationID_GetString(p), SessionID: rfc2866.AcctSessionID_GetString(p), Status: status, Seconds: Quantity(rfc2866.AcctSessionTime_Get(p)), Download: Quantity(down), Upload: Quantity(up), At: when}
	if err := s.Store.Account(r.Context(), e); err != nil {
		return
	} // no durable commit, no response
	_ = w.Write(r.Response(radius.CodeAccountingResponse))
}
func (s *RadiusService) Start(auth, accounting string) error {
	if s.Mode == "disabled" {
		return nil
	}
	if s.Store == nil || len(s.Clients) == 0 {
		return errors.New("RADIUS requires ledger and explicit NAS allowlist")
	}
	peers := map[string]NAS{}
	for _, c := range s.Clients {
		if prev, ok := peers[c.Address]; ok && (prev.RouterID != c.RouterID || prev.Secret != c.Secret) {
			return errors.New("NAS networks must share one router identity and secret")
		}
		peers[c.Address] = c
		if net.ParseIP(c.Address) == nil || len(c.Secret) < 16 || c.RouterID == "" || c.Network == "" || c.CalledStationID == "" {
			return errors.New("invalid NAS configuration")
		}
	}
	var conns []net.PacketConn
	for _, address := range []string{auth, accounting} {
		c, err := net.ListenPacket("udp", address)
		if err != nil {
			for _, v := range conns {
				v.Close()
			}
			return err
		}
		conns = append(conns, c)
	}
	for _, conn := range conns {
		srv := &radius.PacketServer{SecretSource: s, Handler: s, ErrorLog: log.New(io.Discard, "", 0)}
		s.servers = append(s.servers, srv)
		go func() { _ = srv.Serve(conn) }()
	}
	return nil
}
func (s *RadiusService) Close(ctx context.Context) {
	for _, srv := range s.servers {
		_ = srv.Shutdown(ctx)
	}
}

func (s *RadiusService) LimitPortal(key string) bool { return s.limited("portal:" + key) }

// Source address and shared secret authenticate the router. Called-Station-Id
// selects its enrolled HotSpot. Accounting without that attribute may use only
// an already-owned Class lease, never the first configured network.
func (s *RadiusService) packetClient(addr net.Addr, p *radius.Packet) (NAS, bool) {
	base, ok := s.client(addr)
	if !ok {
		return NAS{}, false
	}
	called := rfc2865.CalledStationID_GetString(p)
	if called == "" && p.Code == radius.CodeAccountingRequest {
		var router, network string
		if err := s.Store.sql.QueryRow(`SELECT router_id,network FROM subscriber_leases WHERE id=?`, rfc2865.Class_GetString(p)).Scan(&router, &network); err != nil || router != base.RouterID {
			return NAS{}, false
		}
		return findNAS(s.Clients, router, network)
	}
	var found NAS
	count := 0
	for _, n := range s.Clients {
		if n.Address == base.Address && n.RouterID == base.RouterID && n.CalledStationID == called {
			found = n
			count++
		}
	}
	return found, count == 1
}
