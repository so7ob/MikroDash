package subscriber

import "time"

// TicketOutcome records a local RADIUS decision, not proof of Internet access.
// Only a fresh, exact owned lease observation can establish connected status.
type TicketOutcome struct {
	LeaseID string
	Reasons []string
}

type ConnectionQuery struct {
	AccountID     string
	FlowID        string
	ClientAddress string
}

type ConnectionStatus struct {
	Version     string    `json:"version"`
	FlowID      string    `json:"flowId"`
	Stage       string    `json:"stage"`
	Network     string    `json:"network"`
	Device      string    `json:"device"`
	LeaseID     string    `json:"leaseId"`
	Reasons     []string  `json:"reasons"`
	RequestedAt time.Time `json:"requestedAt"`
	ObservedAt  time.Time `json:"observedAt"`
}
