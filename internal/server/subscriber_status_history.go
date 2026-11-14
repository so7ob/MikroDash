package server

import (
	"mikrodash/internal/subscriber"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Server) subscribersStatusHistory(w http.ResponseWriter, r *http.Request) {
	if s.subscriberAdmin(w, r) == nil {
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeJSONErr(w, 400, "Invalid history query")
		return
	}
	account := query.Get("account")
	var before int64
	cursors, exists := query["before"]
	if len(query["account"]) != 1 || account == "" || exists && len(cursors) != 1 {
		writeJSONErr(w, 400, "Choose one account and at most one history cursor")
		return
	}
	if exists {
		raw := cursors[0]
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 || strconv.FormatInt(before, 10) != raw {
			writeJSONErr(w, 400, "History cursor must be a positive decimal audit ID")
			return
		}
	}
	history, err := s.auditDB.Subscribers().StatusHistory(r.Context(), account, subscriber.Quantity(before))
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, history)
}
