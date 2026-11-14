package server

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"mikrodash/internal/subscriber"
)

func (s *Server) subscribersAccountAudit(w http.ResponseWriter, r *http.Request) {
	if s.subscriberAdmin(w, r) == nil {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeJSONErr(w, 400, "Invalid audit query")
		return
	}
	allowed := map[string]bool{"account": true, "before": true, "category": true, "from": true, "until": true}
	for key, value := range values {
		if !allowed[key] || len(value) != 1 || value[0] == "" {
			writeJSONErr(w, 400, "Choose one account and one value for each audit filter")
			return
		}
	}
	q := subscriber.AccountAuditQuery{AccountID: values.Get("account"), Category: values.Get("category"), From: values.Get("from"), Until: values.Get("until")}
	if q.AccountID == "" {
		writeJSONErr(w, 400, "Choose one subscriber account")
		return
	}
	if raw := values.Get("before"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
			writeJSONErr(w, 400, "Audit cursor must be a positive decimal audit ID")
			return
		}
		q.Before = subscriber.Quantity(n)
	}
	page, err := s.auditDB.Subscribers().AccountAudit(r.Context(), q)
	if err != nil {
		switch {
		case errors.Is(err, subscriber.ErrAuditQuery):
			writeJSONErr(w, 400, "Invalid audit category or UTC date range")
		case errors.Is(err, sql.ErrNoRows):
			writeJSONErr(w, 404, "Subscriber account not found")
		default:
			writeJSONErr(w, 503, "Account audit is unavailable. Retry this page.")
		}
		return
	}
	writeJSON(w, page)
}
