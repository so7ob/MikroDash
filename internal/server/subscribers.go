package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/safe"
	"mikrodash/internal/subscriber"
)

type subscriberRuntime struct {
	cfg      subscriber.Config
	radius   *subscriber.RadiusService
	sessions subscriber.SessionRegistry
	hub      *hub.Hub
	http     *http.Server
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
}

// StartSubscribers is explicit opt-in. No public listener, network writes or
// migrations of subscriber credentials are enabled by an ordinary app upgrade.
func (s *Server) StartSubscribers(configPath, webDir string) error {
	if configPath == "" {
		return nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return errors.New("cannot read subscriber configuration")
	}
	cfg, err := subscriber.DecodeConfig(raw)
	if err != nil {
		return err
	}
	if cfg.Mode == "disabled" {
		return nil
	}
	if s.auditDB == nil {
		return errors.New("subscriber ledger unavailable")
	}
	ledger := s.auditDB.Subscribers()
	if err = ledger.CheckReady(context.Background()); err != nil {
		return errors.New("subscriber migration required")
	}
	for i := range cfg.Clients {
		secret, e := s.store.Decrypt(cfg.Clients[i].SecretSealed)
		if e != nil {
			return errors.New("cannot decrypt NAS secret")
		}
		cfg.Clients[i].Secret = secret
	}
	var tlsConfig *tls.Config
	if !cfg.UsesPortalProxy() {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return errors.New("cannot load subscriber TLS identity")
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	listener, err := net.Listen("tcp", cfg.PortalListen)
	if err != nil {
		return err
	}
	service := &subscriber.RadiusService{Store: ledger, Observe: s.subscriberObserveRouter, AllowCoA: cfg.AllowCoA, VerifySession: s.subscriberVerifySession, Mode: cfg.Mode, Clients: cfg.Clients, PilotAccounts: map[string]bool{}}
	for _, id := range cfg.PilotAccounts {
		service.PilotAccounts[id] = true
	}
	if err = service.Start(cfg.AuthListen, cfg.AccountingListen); err != nil {
		listener.Close()
		return err
	}
	retained := []string{}
	for _, n := range cfg.Clients {
		if _, err := s.sessions.Retain(n.RouterID, "subscribers"); err != nil {
			for _, id := range retained {
				s.sessions.Drop(id, "subscribers")
			}
			service.Close(context.Background())
			listener.Close()
			return errors.New("cannot retain subscriber router connection")
		}
		retained = append(retained, n.RouterID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &subscriberRuntime{cfg: cfg, radius: service, hub: hub.New(), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.subscribers = runtime
	runtime.http = &http.Server{Addr: listener.Addr().String(), Handler: s.subscriberPortal(webDir), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, TLSConfig: tlsConfig}
	go func() {
		var err error
		if cfg.UsesPortalProxy() {
			err = runtime.http.Serve(listener)
		} else {
			err = runtime.http.ServeTLS(listener, "", "")
		}
		if err != nil && err != http.ErrServerClosed {
			log.Print("[subscriber] portal listener stopped")
			cancel()
		}
	}()
	go func() {
		defer close(runtime.done)
		service.Run(ctx, cfg.AllowDisconnect, func(error) { log.Print("[subscriber] enforcement unavailable; local delegations remain bounded") })
	}()
	return nil
}
func (s *Server) stopSubscribers() {
	rt := s.subscribers
	if rt == nil {
		return
	}
	rt.cancel()
	for _, n := range rt.cfg.Clients {
		s.sessions.Drop(n.RouterID, "subscribers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt.radius.Close(ctx)
	_ = rt.http.Shutdown(ctx)
	<-rt.done
}

func (s *Server) subscriberDevice(ctx context.Context, n subscriber.NAS, ip string) (string, error) {
	rs := s.sessions.Live()[n.RouterID]
	if rs == nil || !rs.Connected() {
		return "", errors.New("router is not connected")
	}
	// Exec uses the existing cache/limiter/connection. No per-subscriber socket.
	rows, err := rs.Exec(routeros.Cmd{Path: "/ip/hotspot/host/print", Args: []string{"=.proplist=address,mac-address,server"}})
	if err != nil {
		return "", err
	}
	device := ""
	for _, row := range rows {
		if row["address"] == ip && row["server"] == n.CalledStationID {
			if device != "" && device != row["mac-address"] {
				return "", subscriber.ErrDenied
			}
			device = row["mac-address"]
		}
	}
	if device == "" {
		return "", errors.New("device is not present on the enrolled HotSpot")
	}
	return device, nil
}

func (s *Server) registerSubscribers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/subscribers", s.subscribersGet)
	mux.HandleFunc("POST /api/subscribers", s.subscribersPost)
	mux.HandleFunc("GET /api/subscribers/snapshot", s.subscribersSnapshot)
	mux.HandleFunc("GET /api/subscribers/reconciliation", s.subscribersReconciliationGet)
	mux.HandleFunc("POST /api/subscribers/reconciliation", s.subscribersReconciliationPost)
}
func (s *Server) subscriberAdmin(w http.ResponseWriter, r *http.Request) *Session {
	sess := s.layoutSession(w, r)
	if sess == nil {
		return nil
	}
	if sess.AuthMode != "modern" || s.rbac == nil || !permitted(s.rbac.Can(s.userIDFor(sess.Username), "system:subscribers", "")) {
		writeJSONErr(w, 403, "Subscriber administration permission required")
		return nil
	}
	if s.auditDB == nil || s.auditDB.Subscribers() == nil {
		writeJSONErr(w, 503, "Subscriber ledger unavailable. Check the database startup log.")
		return nil
	}
	if r.Method != "GET" {
		origin := r.Header.Get("Origin")
		if origin == "" || !sameOriginHost(origin, r.Host) || r.Header.Get("X-MikroDash-Request") != "subscriber-admin" {
			writeJSONErr(w, 403, "Request origin rejected")
			return nil
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	return sess
}
func (s *Server) subscribersGet(w http.ResponseWriter, r *http.Request) {
	if s.subscriberAdmin(w, r) == nil {
		return
	}
	store := s.auditDB.Subscribers()
	accounts, err := store.Accounts(r.Context())
	if err != nil {
		subscriberError(w, err)
		return
	}
	plans, err := store.Plans(r.Context())
	if err != nil {
		subscriberError(w, err)
		return
	}
	mode := "disabled"
	if s.subscribers != nil {
		mode = s.subscribers.cfg.Mode
	}
	writeJSON(w, map[string]any{"accounts": accounts, "plans": plans, "mode": mode})
}
func (s *Server) subscribersSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.subscriberAdmin(w, r) == nil {
		return
	}
	v, err := s.auditDB.Subscribers().Snapshot(r.Context(), r.URL.Query().Get("account"), time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, v)
}
func (s *Server) subscribersPost(w http.ResponseWriter, r *http.Request) {
	sess := s.subscriberAdmin(w, r)
	if sess == nil {
		return
	}
	var in struct {
		Action        string          `json:"action"`
		Username      string          `json:"username"`
		Name          string          `json:"name"`
		Password      string          `json:"password"`
		VoucherLength int             `json:"voucherLength"`
		AccountID     string          `json:"accountId"`
		PlanID        string          `json:"planId"`
		Version       int             `json:"version"`
		Reason        string          `json:"reason"`
		Status        string          `json:"status"`
		Plan          subscriber.Plan `json:"plan"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	actor := s.userIDFor(sess.Username)
	store := s.auditDB.Subscribers()
	var err error
	var result any = map[string]bool{"ok": true}
	switch in.Action {
	case "create":
		var a subscriber.Account
		var code string
		a, code, err = store.CreateAccount(r.Context(), actor, in.Username, in.Name, in.Password, in.Reason, in.VoucherLength)
		result = map[string]any{"account": a, "voucher": code}
	case "plan":
		err = store.SavePlan(r.Context(), actor, in.Reason, in.Plan)
	case "assign":
		var id string
		id, err = store.Assign(r.Context(), actor, in.AccountID, in.PlanID, in.Version, in.Reason, time.Now())
		result = map[string]string{"subscriptionId": id}
	case "status":
		err = store.SetStatus(r.Context(), actor, in.AccountID, in.Status, in.Reason)
	default:
		err = errors.New("unsupported subscriber action")
	}
	if err != nil {
		subscriberError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, result)
}
func subscriberBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSONErr(w, 400, "Invalid request")
		return false
	}
	return true
}
func subscriberError(w http.ResponseWriter, err error) {
	writeJSONErr(w, 400, safe.Message(err.Error()))
}

// subscriberAsset serves only named public resources. The portal has no admin
// fallback, /api/auth routes, /app.js, or access to administrator WebSocket rooms.
func subscriberAsset(root, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, filepath.Join(root, name)) }
}
func secureSubscriberHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
func exactOrigin(r *http.Request, origin string) bool {
	return strings.TrimSpace(r.Header.Get("Origin")) == origin
}

func (s *Server) subscriberVerifySession(ctx context.Context, e subscriber.Enforcement) bool {
	if s.subscribers == nil {
		return false
	}
	for _, n := range s.subscribers.cfg.Clients {
		if n.RouterID == e.RouterID && n.Network == e.Network {
			return s.subscribers.radius.Store.VerifyObservedSession(ctx, e, n.CalledStationID, time.Now())
		}
	}
	return false
}
