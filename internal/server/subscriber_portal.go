package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"mikrodash/internal/hub"
	"mikrodash/internal/subscriber"
)

var EvSubscriberSnapshot = hub.Declare[subscriber.Snapshot]("subscriber:snapshot")

func (s *Server) subscriberPortal(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", subscriberAsset(webDir, "subscriber.html"))
	mux.HandleFunc("GET /subscriber.js", subscriberAsset(webDir, "subscriber.js"))
	mux.HandleFunc("GET /subscriber.css", subscriberAsset(s.staticDir, "subscriber.css"))
	mux.HandleFunc("GET /app.css", subscriberAsset(s.staticDir, "app.css"))
	mux.HandleFunc("GET /logo.png", subscriberAsset(s.staticDir, "logo.png"))
	mux.HandleFunc("GET /vendor/tabler.min.css", subscriberAsset(s.staticDir, "vendor/tabler.min.css"))
	for _, prefix := range []string{"css/", "fonts/", "vendor/fonts/"} {
		mux.Handle("GET /"+prefix, http.StripPrefix("/"+prefix, http.FileServer(http.Dir(filepath.Join(s.staticDir, prefix)))))
	}
	mux.HandleFunc("GET /api/session", s.subscriberPortalSession)
	mux.HandleFunc("POST /api/login", s.subscriberPortalLogin)
	mux.HandleFunc("POST /api/logout", s.subscriberPortalLogout)
	mux.HandleFunc("GET /api/account", s.subscriberPortalAccount)
	mux.HandleFunc("POST /api/speed", s.subscriberPortalSpeed)
	mux.HandleFunc("POST /api/connect", s.subscriberPortalConnect)
	mux.HandleFunc("POST /api/disconnect", s.subscriberPortalDisconnect)
	mux.HandleFunc("GET /ws", s.subscriberPortalWS)
	return secureSubscriberHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forms := "'self'"
		for _, n := range s.subscribers.cfg.Clients {
			forms += " " + n.LoginURL
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action "+forms)
		mux.ServeHTTP(w, r)
	}))
}
func (s *Server) subscriberIdentity(w http.ResponseWriter, r *http.Request) (subscriber.PortalSession, bool) {
	c, err := r.Cookie("__Host-mikrodash_subscriber")
	if err != nil {
		writeJSONErr(w, 401, "Sign in to your account")
		return subscriber.PortalSession{}, false
	}
	v, ok := s.subscribers.sessions.Get(c.Value)
	if !ok {
		writeJSONErr(w, 401, "Your portal session has expired")
		return v, false
	}
	if r.Method != "GET" && (!exactOrigin(r, s.subscribers.cfg.PortalOrigin) || r.Header.Get("X-CSRF-Token") != v.CSRF) {
		writeJSONErr(w, 403, "Request verification failed")
		return v, false
	}
	return v, true
}
func (s *Server) subscriberPortalLogin(w http.ResponseWriter, r *http.Request) {
	if !exactOrigin(r, s.subscribers.cfg.PortalOrigin) {
		writeJSONErr(w, 403, "Request origin rejected")
		return
	}
	if s.subscribers.radius.LimitPortal(clientIPOf(r)) {
		writeJSONErr(w, 429, "Too many attempts. Try again later.")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	if len(in.Password) > 128 || len(in.Username) > 128 {
		writeJSONErr(w, 400, "Invalid credentials")
		return
	}
	a, err := s.subscribers.radius.Authenticate(r.Context(), in.Username, in.Password)
	if errors.Is(err, subscriber.ErrAuthenticationBusy) {
		w.Header().Set("Retry-After", "2")
		writeJSONErr(w, 429, "Sign-in is busy. Try again shortly.")
		return
	}
	if err != nil {
		writeJSONErr(w, 401, "Invalid credentials")
		return
	}
	token, v := s.subscribers.sessions.New(a.ID)
	http.SetCookie(w, &http.Cookie{Name: "__Host-mikrodash_subscriber", Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 8 * 3600})
	writeJSON(w, map[string]any{"csrf": v.CSRF, "account": a})
}
func (s *Server) subscriberPortalSession(w http.ResponseWriter, r *http.Request) {
	v, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	networks := []string{}
	for _, n := range s.subscribers.cfg.Clients {
		networks = append(networks, n.Network)
	}
	writeJSON(w, map[string]any{"csrf": v.CSRF, "networks": networks, "mode": s.subscribers.cfg.Mode})
}
func (s *Server) subscriberPortalLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.subscriberIdentity(w, r); !ok {
		return
	}
	c, _ := r.Cookie("__Host-mikrodash_subscriber")
	s.subscribers.sessions.Delete(c.Value)
	http.SetCookie(w, &http.Cookie{Name: c.Name, Path: "/", Secure: true, HttpOnly: true, MaxAge: -1})
	writeJSON(w, map[string]string{"status": "signed-out", "internet": "unchanged"})
}
func (s *Server) subscriberPortalAccount(w http.ResponseWriter, r *http.Request) {
	v, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	snap, err := s.subscribers.radius.Store.Snapshot(r.Context(), v.AccountID, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, snap)
}
func (s *Server) subscriberPortalSpeed(w http.ResponseWriter, r *http.Request) {
	v, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	var in struct {
		ProfileID string `json:"profileId"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	if err := s.subscribers.radius.Store.ChooseSpeed(r.Context(), v.AccountID, in.ProfileID, time.Now()); err != nil {
		subscriberError(w, err)
		return
	}
	snapshot, err := s.subscribers.radius.Store.Snapshot(r.Context(), v.AccountID, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": snapshot.Subscription.SpeedState})
}
func (s *Server) subscriberPortalConnect(w http.ResponseWriter, r *http.Request) {
	v, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	rt := s.subscribers
	if rt.cfg.Mode != "pilot" && rt.cfg.Mode != "managed" {
		writeJSONErr(w, 409, "Internet connection is not enabled in shadow mode")
		return
	}
	var in struct {
		Network string `json:"network"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	if rt.radius.LimitPortal("connect:" + v.AccountID) {
		writeJSONErr(w, 429, "Too many connection attempts")
		return
	}
	var nas subscriber.NAS
	for _, n := range rt.cfg.Clients {
		if n.Network == in.Network {
			nas = n
		}
	}
	if nas.Network == "" {
		writeJSONErr(w, 403, "Network is not enrolled")
		return
	}
	snap, err := rt.radius.Store.Snapshot(r.Context(), v.AccountID, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	allowed := false
	for _, n := range snap.Subscription.Plan.Networks {
		allowed = allowed || n == nas.Network
	}
	if !allowed {
		writeJSONErr(w, 403, "Network is not included in your subscription")
		return
	}
	device, err := s.subscriberDevice(r.Context(), nas, clientIPOf(r))
	if err != nil {
		subscriberError(w, err)
		return
	}
	account, err := rt.radius.Store.AccountByID(r.Context(), v.AccountID)
	if err != nil {
		subscriberError(w, err)
		return
	}
	ticket, err := rt.radius.Store.IssueTicket(r.Context(), v.AccountID, nas.Network, device, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, map[string]string{"loginUrl": nas.LoginURL, "username": account.Username, "password": ticket, "destination": rt.cfg.PortalOrigin + "/"})
}
func (s *Server) subscriberPortalDisconnect(w http.ResponseWriter, r *http.Request) {
	v, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	var in struct {
		LeaseID string `json:"leaseId"`
		Confirm bool   `json:"confirm"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeJSONErr(w, 400, "Confirm disconnection before continuing")
		return
	}
	snap, err := s.subscribers.radius.Store.Snapshot(r.Context(), v.AccountID, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	for _, l := range snap.Sessions {
		if l.ID == in.LeaseID && l.State != "stopped" {
			err = s.subscribers.radius.Disconnect(r.Context(), subscriber.Enforcement{LeaseID: l.ID, AccountID: v.AccountID, RouterID: l.RouterID, Network: l.Network, Device: l.Device, SessionID: l.SessionID, Reason: "subscriber-confirmed"}, s.subscribers.cfg.AllowDisconnect)
			if err != nil {
				subscriberError(w, err)
				return
			}
			writeJSON(w, map[string]string{"status": "acknowledged-awaiting-accounting"})
			return
		}
	}
	writeJSONErr(w, 404, "Session not found")
}
func (s *Server) subscriberPortalWS(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	if !exactOrigin(r, s.subscribers.cfg.PortalOrigin) {
		writeJSONErr(w, 403, "Request origin rejected")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{strings.TrimPrefix(s.subscribers.cfg.PortalOrigin, "https://")}})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	// Read frames only to process control messages. No subscriber may join rooms.
	ws.SetReadLimit(1024)
	requestCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if s.subscribers.ctx != nil {
		stop := context.AfterFunc(s.subscribers.ctx, cancel)
		defer stop()
	}
	ctx := ws.CloseRead(requestCtx)
	cookie, _ := r.Cookie("__Host-mikrodash_subscriber")
	c := hub.NewClient(subscriber.ID(), 2)
	s.subscribers.hub.Add(c)
	defer s.subscribers.hub.Remove(c)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	send := func() bool {
		if _, ok := s.subscribers.sessions.Get(cookie.Value); !ok {
			return false
		}
		snapshot, err := s.subscribers.radius.Store.Snapshot(ctx, identity.AccountID, time.Now())
		if err != nil {
			return true
		}
		EvSubscriberSnapshot.Send(s.subscribers.hub, c, snapshot)
		select {
		case frame := <-c.Send:
			writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			return ws.Write(writeCtx, websocket.MessageText, frame) == nil
		default:
			return true
		}
	}
	if !send() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}
