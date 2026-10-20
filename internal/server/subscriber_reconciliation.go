package server

import (
	"errors"
	"mikrodash/internal/subscriber"
	"net/http"
	"time"
)

func (s *Server) subscribersReconciliationGet(w http.ResponseWriter, r *http.Request) {
	if s.subscriberAdmin(w, r) == nil {
		return
	}
	account := r.URL.Query().Get("account")
	if account == "" || len(account) > 128 {
		subscriberError(w, errors.New("Choose a subscriber"))
		return
	}
	ledger := s.auditDB.Subscribers()
	gaps, err := ledger.ByteGaps(r.Context(), account)
	if err != nil {
		subscriberError(w, err)
		return
	}
	legacy, err := ledger.LegacyExposures(r.Context(), account)
	if err != nil {
		subscriberError(w, err)
		return
	}
	sessions, err := ledger.SessionReviews(r.Context(), account)
	if err != nil {
		subscriberError(w, err)
		return
	}
	receipts, err := ledger.AccountingReceipts(r.Context(), account)
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, struct {
		Gaps     []subscriber.ByteGap           `json:"gaps"`
		Legacy   []subscriber.LegacyExposure    `json:"legacy"`
		Sessions []subscriber.SessionReview     `json:"sessions"`
		Receipts []subscriber.AccountingReceipt `json:"receipts"`
	}{gaps, legacy, sessions, receipts})
}
func (s *Server) subscribersReconciliationPost(w http.ResponseWriter, r *http.Request) {
	sess := s.subscriberAdmin(w, r)
	if sess == nil {
		return
	}
	var in struct {
		Action      string                     `json:"action"`
		ID          string                     `json:"id"`
		Reason      string                     `json:"reason"`
		Evidence    string                     `json:"evidence"`
		Amount      subscriber.Quantity        `json:"amount"`
		Allocations []subscriber.GapAllocation `json:"allocations"`
		Reading     subscriber.FinalReading    `json:"reading"`
	}
	if !subscriberBody(w, r, &in) {
		return
	}
	ledger := s.auditDB.Subscribers()
	actor := s.userIDFor(sess.Username)
	now := time.Now()
	var err error
	switch in.Action {
	case "byte-gap":
		err = ledger.ResolveByteGap(r.Context(), actor, in.ID, in.Reason, in.Evidence, in.Allocations, now)
	case "missing-stop":
		if in.Reading.LeaseID != in.ID {
			subscriberError(w, errors.New("Final reading must match the selected session"))
			return
		}
		in.Reading.Reason = in.Reason
		in.Reading.Evidence = in.Evidence
		err = ledger.ReconcileMissingStop(r.Context(), actor, in.Reading, now)
	case "legacy":
		err = ledger.ResolveLegacyExposure(r.Context(), actor, in.ID, in.Reason, in.Evidence, in.Amount, now)
	default:
		err = errors.New("Choose a supported reconciliation action")
	}
	if err != nil {
		subscriberError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
