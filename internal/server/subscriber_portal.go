package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"mikrodash/internal/subscriber"
)

func (s *Server) subscriberPortal(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", subscriberAsset(webDir, "subscriber.html"))
	mux.HandleFunc("GET /subscriber.js", subscriberAsset(webDir, "subscriber.js"))
	mux.HandleFunc("GET /subscriber.css", subscriberAsset(s.staticDir, "subscriber.css"))
	mux.HandleFunc("GET /app.css", subscriberAsset(s.staticDir, "app.css"))
	mux.HandleFunc("GET /logo.png", subscriberAsset(s.staticDir, "logo.png"))
	mux.HandleFunc("GET /vendor/tabler.min.css", subscriberAsset(s.staticDir, "vendor/tabler.min.css"))
	for _, prefix := range []string{"css/", "fonts/", "vendor/fonts/"} {
		directory := filepath.Join(s.staticDir, prefix)
		var files http.Handler = http.FileServer(http.Dir(directory))
		if prefix != "css/" {
			files = subscriberPublicFonts(directory)
		}
		mux.Handle("GET /"+prefix, http.StripPrefix("/"+prefix, files))
	}
	mux.HandleFunc("GET /api/session", s.subscriberPortalSession)
	mux.HandleFunc("POST /api/login", s.subscriberPortalLogin)
	mux.HandleFunc("POST /api/logout", s.subscriberPortalLogout)
	mux.HandleFunc("GET /api/account", s.subscriberPortalAccount)
	mux.HandleFunc("GET /api/connection", s.subscriberPortalConnection)
	mux.HandleFunc("POST /api/speed", s.subscriberPortalSpeed)
	mux.HandleFunc("POST /api/connect", s.subscriberPortalConnect)
	mux.HandleFunc("POST /api/disconnect", s.subscriberPortalDisconnect)
	mux.HandleFunc("GET /ws", s.subscriberPortalWS)
	return secureSubscriberHeaders(subscriberProxyBoundary(s.subscribers.cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forms := "'self'"
		for _, n := range s.subscribers.cfg.Clients {
			forms += " " + n.LoginURL
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action "+forms)
		mux.ServeHTTP(w, r)
	})))
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
	if s.subscribers.radius.LimitPortal(subscriberClientIPOf(r)) {
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
	matches := 0
	for _, n := range rt.cfg.Clients {
		if n.Network == in.Network {
			nas = n
			matches++
		}
	}
	if matches > 1 {
		writeJSONErr(w, 409, "This network needs administrator review. Contact support.")
		return
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
	device, err := s.subscriberDevice(r.Context(), nas, subscriberClientIPOf(r))
	if err != nil {
		subscriberError(w, err)
		return
	}
	account, err := rt.radius.Store.AccountByID(r.Context(), v.AccountID)
	if err != nil {
		subscriberError(w, err)
		return
	}
	ticket, err := rt.radius.Store.IssueTicket(r.Context(), subscriber.TicketRequest{AccountID: v.AccountID, RouterID: nas.RouterID, Network: nas.Network, Device: device, ClientAddress: subscriberClientIPOf(r)}, time.Now())
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, map[string]string{"loginUrl": nas.LoginURL, "username": account.Username, "password": ticket, "flowId": subscriber.Digest(ticket), "destination": rt.cfg.PortalOrigin + "/?connection=" + subscriber.Digest(ticket)})
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
