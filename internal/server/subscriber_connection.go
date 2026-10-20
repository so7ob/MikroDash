package server

import (
	"errors"
	"net/http"
	"time"

	"mikrodash/internal/subscriber"
)

func (s *Server) subscriberPortalConnection(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.subscriberIdentity(w, r)
	if !ok {
		return
	}
	status, err := s.subscribers.radius.Store.ConnectionStatusFor(r.Context(), subscriber.ConnectionQuery{AccountID: identity.AccountID, FlowID: r.URL.Query().Get("flow"), ClientAddress: subscriberClientIPOf(r)}, s.subscribers.cfg.Clients, time.Now())
	if errors.Is(err, subscriber.ErrDenied) {
		writeJSONErr(w, 404, "Connection request not found")
		return
	}
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, status)
}
