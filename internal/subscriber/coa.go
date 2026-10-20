package subscriber

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"
	"mikrodash/internal/guard"
)

func operationPacket(o Operation, n NAS, at time.Time, observedAddress string) (*radius.Packet, error) {
	q := o.Request
	address, err := netip.ParseAddr(observedAddress)
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() || address.IsLoopback() {
		return nil, errors.New("confirmed HotSpot IPv4 address required for CoA")
	}
	// Recheck current capabilities before replaying a persisted packet. Removing
	// probe evidence or a capability must also revoke an uncertain retry.
	if o.Kind == "rate" {
		if !n.Capabilities.Recorded() || !n.Capabilities.CoARate {
			return nil, errors.New("live rate capability is unverified")
		}
	} else if o.Kind == "renew" {
		if !n.Capabilities.CanRenew() {
			return nil, errors.New("renewal requires verified total-session timeout and byte semantics")
		}
		if !n.Capabilities.CoAGigawords && (q.DownloadCeiling > math.MaxUint32 || q.UploadCeiling > math.MaxUint32 || q.TotalCeiling > math.MaxUint32) {
			return nil, errors.New("CoA gigaword limits are unverified")
		}
		if q.TotalCeiling > 0 && !n.Capabilities.CoATotal {
			return nil, errors.New("CoA total byte limit is unverified")
		}
	} else {
		return nil, errors.New("invalid session operation")
	}
	if o.Packet != "" {
		b, err := hex.DecodeString(o.Packet)
		if err != nil {
			return nil, err
		}
		p, err := radius.Parse(b, []byte(n.Secret))
		if err != nil {
			return nil, err
		}
		addresses, err := rfc2865.FramedIPAddress_Gets(p)
		if err != nil || len(addresses) != 1 || !addresses[0].Equal(net.IP(address.AsSlice())) {
			return nil, errors.New("persisted CoA address is missing or no longer matches the owned device")
		}
		return p, nil
	}
	p := radius.New(radius.CodeCoARequest, []byte(n.Secret))
	setters := []error{
		rfc2865.UserName_SetString(p, q.Username), rfc2865.CallingStationID_SetString(p, q.Device),
		rfc2865.FramedIPAddress_Set(p, net.IP(address.AsSlice())),
		rfc2865.CalledStationID_SetString(p, n.CalledStationID), rfc2866.AcctSessionID_SetString(p, q.SessionID),
		rfc2869.EventTimestamp_Set(p, at),
	}
	if o.Kind == "rate" {
		setters = append(setters, mikrotik.MikrotikRateLimit_SetString(p, fmt.Sprintf("%d/%d", q.Admission.Decision.Up, q.Admission.Decision.Down)))
	} else {
		timeout := o.TargetDeadline.Unix() - q.StartedAt.Unix()
		if timeout <= 0 || timeout > math.MaxUint32 {
			return nil, errors.New("delegation deadline cannot be encoded")
		}
		down, up, total := q.DownloadCeiling, q.UploadCeiling, q.TotalCeiling
		setters = append(setters, rfc2865.SessionTimeout_Set(p, rfc2865.SessionTimeout(timeout)),
			mikrotik.MikrotikXmitLimit_Set(p, mikrotik.MikrotikXmitLimit(uint64(down)&0xffffffff)),
			mikrotik.MikrotikRecvLimit_Set(p, mikrotik.MikrotikRecvLimit(uint64(up)&0xffffffff)))
		if n.Capabilities.CoAGigawords {
			setters = append(setters, mikrotik.MikrotikXmitLimitGigawords_Set(p, mikrotik.MikrotikXmitLimitGigawords(uint64(down)>>32)), mikrotik.MikrotikRecvLimitGigawords_Set(p, mikrotik.MikrotikRecvLimitGigawords(uint64(up)>>32)))
		}
		if total > 0 {
			setters = append(setters, mikrotik.MikrotikTotalLimit_Set(p, mikrotik.MikrotikTotalLimit(uint64(total)&0xffffffff)))
			if n.Capabilities.CoAGigawords {
				setters = append(setters, mikrotik.MikrotikTotalLimitGigawords_Set(p, mikrotik.MikrotikTotalLimitGigawords(uint64(total)>>32)))
			}
		}
	}
	for _, err := range setters {
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func operationVerified(o Operation, row ObservedSession, at time.Time) bool {
	if !at.After(o.CreatedAt) || at.Before(o.UpdatedAt) || at.Sub(o.UpdatedAt) > 10*time.Second {
		return false
	}
	if o.Kind == "rate" {
		return row.RateKnown && row.Down == o.Request.Admission.Decision.Down && row.Up == o.Request.Admission.Decision.Up
	}
	actual, err := add(row.Seconds, Quantity(row.TimeLeft))
	expected := Quantity(o.TargetDeadline.Unix() - o.Request.StartedAt.Unix())
	return err == nil && expected > 0 && row.TimeoutKnown && actual >= expected-2 && actual <= expected && row.LimitsKnown && row.DownloadLimit == o.Request.DownloadCeiling && row.UploadLimit == o.Request.UploadCeiling && (o.Request.TotalCeiling == 0 || row.TotalLimit == o.Request.TotalCeiling)
}

// ProcessOperations first checks readback, including after a crash or lost ACK.
// An ACK alone never extends the stored deadline or marks a rate as applied.
func (s *RadiusService) ProcessOperations(ctx context.Context, at time.Time) error {
	operations, err := s.Store.Operations(ctx, true)
	if err != nil {
		return err
	}
	for _, o := range operations {
		n, ok := findNAS(s.Clients, o.Request.RouterID, o.Request.Network)
		if !ok {
			return ErrDenied
		}
		if o.Kind == "renew" && o.Request.TimeBasis != renewalTimeBasis {
			if err = s.Store.retireLegacyRenewal(ctx, o, at); err != nil {
				return err
			}
			continue
		}
		grant := guard.SubscriberAuthority{Identity: "service:subscriber", Mode: s.Mode, RouterID: n.RouterID, Network: n.Network, Change: s.AllowCoA}
		if !guard.CheckSubscriberChange(grant, o.Request.RouterID, o.Request.Network, o.LeaseID, o.Request.SessionID) {
			continue
		}
		row, observed, present, err := s.Store.observation(ctx, o.LeaseID)
		if err != nil || !present || at.Sub(observed) > 5*time.Second || observed.After(at.Add(time.Second)) {
			continue
		}
		if n.Capabilities.Version == "" || row.Version != n.Capabilities.Version {
			continue
		}
		e := trackedSession{Enforcement: Enforcement{Username: o.Request.Username, Device: o.Request.Device, StartedAt: o.Request.StartedAt}}
		if !sessionMatches(e, row, n.CalledStationID, observed) {
			continue
		}
		// The address in an issued packet is part of its target identity. Do not
		// verify or replay that intent using a later address, even if its queue
		// happens to expose the requested ceiling.
		if o.Packet != "" {
			if _, err = operationPacket(o, n, at, row.Address); err != nil {
				if err = s.Store.transitionOperation(ctx, o, "pending-reconnect", err.Error(), "", at); err != nil {
					return err
				}
				continue
			}
		}
		var owner string
		if err = s.Store.sql.QueryRowContext(ctx, `SELECT sub.account_id FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id WHERE l.id=? AND l.state='active' AND l.session_id=? AND l.device=? AND l.router_id=? AND l.network=?`, o.LeaseID, o.Request.SessionID, o.Request.Device, o.Request.RouterID, o.Request.Network).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				if err = s.Store.transitionOperation(ctx, o, "failed", "owned session ended or changed", "", at); err != nil {
					return err
				}
				continue
			}
			return err
		}
		current, err := s.Store.Snapshot(ctx, owner, at)
		if err != nil {
			return err
		}
		u := map[string]Usage{}
		for _, v := range current.Decision.Limits {
			u[Key(v.Limit.ID, v.PeriodStart)] = Usage{Used: v.Used, Reserved: v.Provisional, Provisional: v.Provisional, Quality: v.Quality}
		}
		decision := Evaluate(current.Subscription.Plan, current.Subscription.SpeedID, at, current.Subscription.ExpiresAt, u)
		if current.PendingAccounting > 0 || !decision.Allowed || (o.Kind == "rate" && (current.Subscription.SpeedID != o.Request.SpeedID || o.Request.Admission.Decision.Down > decision.Down || o.Request.Admission.Decision.Up > decision.Up)) {
			if err = s.Store.transitionOperation(ctx, o, "failed", "policy or selected speed changed before application", "", at); err != nil {
				return err
			}
			continue
		}
		if o.Kind == "rate" && o.State == "requested" && row.RateKnown && row.Down == o.Request.Admission.Decision.Down && row.Up == o.Request.Admission.Decision.Up {
			if !observed.After(o.CreatedAt) {
				continue
			}
			if err = s.Store.transitionOperation(ctx, o, "applying", "requested rate already observed on this device", "", at); err != nil {
				return err
			}
			if err = s.Store.transitionOperation(ctx, o, "verified", "verified existing exact-device queue ceiling", "", at); err != nil {
				return err
			}
			continue
		}
		if o.State != "requested" && operationVerified(o, row, observed) {
			if err = s.Store.transitionOperation(ctx, o, "verified", "verified by session/queue readback", "", at); err != nil {
				return err
			}
			continue
		}
		if o.Attempts >= 3 || !at.Before(o.PreviousDeadline) || at.Sub(o.CreatedAt) > 15*time.Second {
			if err = s.Store.transitionOperation(ctx, o, "failed", "application was not verified before bounded deadline", "", at); err != nil {
				return err
			}
			continue
		}
		if o.State != "requested" && at.Sub(o.UpdatedAt) < 3*time.Second {
			continue
		}
		p, err := operationPacket(o, n, at, row.Address)
		if err != nil {
			if err = s.Store.transitionOperation(ctx, o, "pending-reconnect", err.Error(), "", at); err != nil {
				return err
			}
			continue
		}
		raw, err := p.MarshalBinary()
		if err != nil {
			return err
		}
		if err = s.Store.transitionOperation(ctx, o, "applying", "durable intent before network write", hex.EncodeToString(raw), at); err != nil {
			return err
		}
		deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
		// Retry is explicit and persisted. A retransmission by the transport must
		// never silently extend a relative Session-Timeout a second time.
		client := radius.Client{MaxPacketErrors: 10}
		reply, sendErr := client.Exchange(deadline, p, net.JoinHostPort(n.Address, fmt.Sprint(n.DisconnectPort)))
		cancel()
		if sendErr != nil {
			continue
		} // uncertain application: read back on the next cycle
		switch reply.Code {
		case radius.CodeCoAACK:
			err = s.Store.transitionOperation(ctx, o, "acknowledged", "NAS acknowledged; awaiting readback", "", at)
		case radius.CodeCoANAK:
			err = s.Store.transitionOperation(ctx, o, "failed", "NAS rejected change of authorization", "", at)
		default:
			err = s.Store.transitionOperation(ctx, o, "failed", "unexpected dynamic authorization response", "", at)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Tick is independent of browser lifetime and accepts a clock value for tests.
// A single shared observation per router precedes policy evaluation and CoA.
func (s *RadiusService) Tick(ctx context.Context, at time.Time) error {
	seen := map[string]bool{}
	for _, n := range s.Clients {
		if seen[n.RouterID] {
			continue
		}
		seen[n.RouterID] = true
		if s.Observe == nil {
			continue
		}
		snap, err := s.Observe(ctx, n.RouterID)
		if err != nil {
			continue
		} // retain last reliable values; never infer absence
		if snap.At.After(at) {
			at = snap.At
		}
		if err = s.Store.ObserveRouter(ctx, snap, s.Clients); err != nil {
			return err
		}
	}
	if s.Mode == "shadow" || s.Mode == "disabled" || !s.AllowCoA {
		return nil
	}
	if err := s.ProcessOperations(ctx, at); err != nil {
		return err
	}
	for router := range seen {
		leases, err := s.Store.tracked(ctx, router)
		if err != nil {
			return err
		}
		for _, e := range leases {
			n, ok := findNAS(s.Clients, router, e.Network)
			if !ok {
				continue
			}
			row, observed, present, err := s.Store.observation(ctx, e.LeaseID)
			if err != nil || !present || at.Sub(observed) > 5*time.Second {
				continue
			}
			if e.SessionID == "" {
				continue
			}
			kind := ""
			if n.Capabilities.CanRenew() && row.CountersKnown && row.Version == n.Capabilities.Version && e.Deadline.Sub(at) <= 20*time.Second && e.Deadline.After(at) {
				kind = "renew"
			} else if n.Capabilities.Recorded() && n.Capabilities.CoARate && row.RateKnown {
				v, err := s.Store.Snapshot(ctx, e.AccountID, at)
				if err != nil {
					return err
				}
				// Reservations are unavailable to a new device, but are not consumption.
				u := map[string]Usage{}
				for _, l := range v.Decision.Limits {
					u[Key(l.Limit.ID, l.PeriodStart)] = Usage{Used: l.Used, Reserved: l.Provisional, Provisional: l.Provisional, Quality: l.Quality}
				}
				d := Evaluate(v.Subscription.Plan, v.Subscription.SpeedID, at, v.Subscription.ExpiresAt, u)
				if row.Down != d.Down || row.Up != d.Up || v.Subscription.SpeedState == "requested" {
					kind = "rate"
				}
			}
			if kind != "" {
				if _, err = s.Store.PrepareOperation(ctx, e.LeaseID, kind, at); err != nil && !errors.Is(err, ErrDenied) {
					return err
				}
			}
		}
	}
	return s.ProcessOperations(ctx, at)
}
