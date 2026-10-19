package guard

// SubscriberAuthority is a service grant, not an administrator impersonation.
// It permits only narrow changes to a ledger-owned HotSpot session on an enrolled
// network. Configuration is explicit and never inferred from a browser claim.
type SubscriberAuthority struct {
	Identity   string
	Mode       string
	RouterID   string
	Network    string
	Disconnect bool
	Change     bool
}

func CheckSubscriberDisconnect(a SubscriberAuthority, router, network, lease, session string) bool {
	return a.Identity == "service:subscriber" && (a.Mode == "pilot" || a.Mode == "managed") && a.Disconnect && a.RouterID == router && a.Network == network && router != "" && network != "" && lease != "" && session != ""
}

// CheckSubscriberChange permits only enrolled ledger-owned session changes.
func CheckSubscriberChange(a SubscriberAuthority, router, network, lease, session string) bool {
	return a.Identity == "service:subscriber" && (a.Mode == "pilot" || a.Mode == "managed") && a.Change && a.RouterID == router && a.Network == network && router != "" && network != "" && lease != "" && session != ""
}
