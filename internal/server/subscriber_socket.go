package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"mikrodash/internal/hub"
	"mikrodash/internal/subscriber"
)

var EvSubscriberSnapshot = hub.Declare[subscriber.Snapshot]("subscriber:snapshot")
var EvSubscriberConnection = hub.Declare[subscriber.ConnectionStatus]("subscriber:connection")

func (s *Server) subscriberPortalWS(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	if !exactOrigin(r, s.subscribers.cfg.PortalOrigin) {
		writeJSONErr(w, 403, "Request origin rejected")
		return
	}
	query := subscriber.ConnectionQuery{AccountID: identity.AccountID, FlowID: r.URL.Query().Get("flow"), ClientAddress: subscriberClientIPOf(r)}
	if query.FlowID != "" {
		if _, err := s.subscribers.radius.Store.ConnectionStatusFor(r.Context(), query, s.subscribers.cfg.Clients, time.Now()); err != nil {
			if errors.Is(err, subscriber.ErrDenied) {
				writeJSONErr(w, 404, "Connection request not found")
			} else {
				subscriberError(w, err)
			}
			return
		}
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{strings.TrimPrefix(s.subscribers.cfg.PortalOrigin, "https://")}})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	// Control frames only: a subscriber cannot subscribe to administration rooms.
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
	owned := func() bool {
		current, valid := s.subscribers.sessions.Get(cookie.Value)
		return valid && current.AccountID == identity.AccountID
	}
	writeFrame := func() bool {
		// Recheck before each individual send, including the connection event.
		if !owned() {
			return false
		}
		select {
		case frame := <-c.Send:
			writeCtx, done := context.WithTimeout(ctx, 3*time.Second)
			defer done()
			return ws.Write(writeCtx, websocket.MessageText, frame) == nil
		default:
			return true
		}
	}
	lastConnection := ""
	send := func() bool {
		if !owned() {
			return false
		}
		now := time.Now()
		snapshot, err := s.subscribers.radius.Store.Snapshot(ctx, identity.AccountID, now)
		if err == nil {
			EvSubscriberSnapshot.Send(s.subscribers.hub, c, snapshot)
			if !writeFrame() {
				return false
			}
		}
		if query.FlowID != "" {
			status, err := s.subscribers.radius.Store.ConnectionStatusFor(ctx, query, s.subscribers.cfg.Clients, now)
			if errors.Is(err, subscriber.ErrDenied) {
				return false
			}
			if err == nil && status.Version != lastConnection {
				EvSubscriberConnection.Send(s.subscribers.hub, c, status)
				if !writeFrame() {
					return false
				}
				lastConnection = status.Version
			}
		}
		return true
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
