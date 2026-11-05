package subscriber

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// AccountingReceipt retains presence separately from numeric zero. Raw unsigned
// counters remain exact decimal strings even outside the supported ledger range.
// Fields contains only whitelisted accounting attributes, never a RADIUS secret,
// password, authenticator, ticket or arbitrary packet payload.
type AccountingReceipt struct {
	ID            string            `json:"id"`
	LeaseID       string            `json:"leaseId"`
	RouterID      string            `json:"routerId"`
	Network       string            `json:"network"`
	Device        string            `json:"device"`
	SessionID     string            `json:"sessionId"`
	Status        string            `json:"status"`
	Seconds       string            `json:"seconds"`
	Download      string            `json:"download"`
	Upload        string            `json:"upload"`
	TimeKnown     bool              `json:"timeKnown"`
	DownloadKnown bool              `json:"downloadKnown"`
	UploadKnown   bool              `json:"uploadKnown"`
	Fields        map[string]string `json:"fields"`
	At            time.Time         `json:"at"`
	ReceivedAt    time.Time         `json:"receivedAt"`
	ResolvedAt    time.Time         `json:"resolvedAt"`
	State         string            `json:"state"`
	Reason        string            `json:"reason"`
}

func (r AccountingReceipt) reading() (Accounting, error) {
	e := Accounting{LeaseID: r.LeaseID, RouterID: r.RouterID, Network: r.Network, Device: r.Device, SessionID: r.SessionID, Status: r.Status, Source: "radius", At: r.At}
	missingAtStart := func(known bool, fields ...string) bool {
		if known {
			return true
		}
		if r.Status != "start" {
			return false
		}
		for _, field := range fields {
			if _, present := r.Fields[field]; present {
				return false
			}
		}
		return true
	}
	if !missingAtStart(r.TimeKnown, "Acct-Session-Time") ||
		!missingAtStart(r.DownloadKnown, "Acct-Output-Octets", "Acct-Output-Gigawords") ||
		!missingAtStart(r.UploadKnown, "Acct-Input-Octets", "Acct-Input-Gigawords") {
		return e, errors.New("accounting statistics are incomplete")
	}
	parse := func(raw string) (Quantity, error) {
		if raw == "" && r.Status == "start" {
			return 0, nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return 0, errors.New("accounting quantity exceeds the ledger range")
		}
		return Quantity(n), nil
	}
	var err error
	if e.Seconds, err = parse(r.Seconds); err != nil {
		return e, err
	}
	if e.Download, err = parse(r.Download); err != nil {
		return e, err
	}
	if e.Upload, err = parse(r.Upload); err != nil {
		return e, err
	}
	if e.Seconds > 315360000 {
		return e, errors.New("session duration exceeds the supported range")
	}
	if _, err = add(e.Download, e.Upload); err != nil {
		return e, err
	}
	return e, nil
}

func receiptIdentity(r AccountingReceipt) string {
	// Delay/receipt times change on retransmission. The financial identity does
	// not; presence bits prevent a missing value from colliding with known zero.
	raw, _ := json.Marshal([]any{r.RouterID, r.Network, r.LeaseID, r.Device, r.SessionID, r.Status, r.Seconds, r.Download, r.Upload, r.TimeKnown, r.DownloadKnown, r.UploadKnown, r.Fields})
	return Digest("accounting-receipt:" + string(raw))
}
