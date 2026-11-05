package subscriber

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

func accountingReceipt(p *radius.Packet, n NAS, now time.Time) AccountingReceipt {
	r := AccountingReceipt{LeaseID: rfc2865.Class_GetString(p), RouterID: n.RouterID, Network: n.Network, Device: rfc2865.CallingStationID_GetString(p), SessionID: rfc2866.AcctSessionID_GetString(p), ReceivedAt: now, At: now.Add(-time.Duration(rfc2866.AcctDelayTime_Get(p)) * time.Second), Fields: map[string]string{}}
	switch rfc2866.AcctStatusType_Get(p) {
	case rfc2866.AcctStatusType_Value_Start:
		r.Status = "start"
	case rfc2866.AcctStatusType_Value_InterimUpdate:
		r.Status = "interim"
	case rfc2866.AcctStatusType_Value_Stop:
		r.Status = "stop"
	case rfc2866.AcctStatusType_Value_AccountingOn:
		r.Status = "nas-on"
	case rfc2866.AcctStatusType_Value_AccountingOff:
		r.Status = "nas-off"
	default:
		r.Status = "unsupported"
	}
	// Read each allowed integer exactly once through the protocol library. A
	// repeated attribute is ambiguous even when its first value parses.
	counter := func(kind radius.Type, name string) (uint32, bool, bool) {
		values := []radius.Attribute{}
		for _, avp := range p.Attributes {
			if avp.Type == kind {
				values = append(values, avp.Attribute)
			}
		}
		if len(values) == 0 {
			return 0, false, false
		}
		if len(values) == 1 {
			if value, err := radius.Integer(values[0]); err == nil {
				r.Fields[name] = strconv.FormatUint(uint64(value), 10)
				return value, true, true
			}
		}
		raw := make([]string, len(values))
		for i, value := range values {
			raw[i] = hex.EncodeToString(value)
		}
		r.Fields[name] = "malformed:" + strings.Join(raw, ",")
		return 0, false, true
	}
	seconds, timeKnown, _ := counter(rfc2866.AcctSessionTime_Type, "Acct-Session-Time")
	r.TimeKnown = timeKnown
	if timeKnown {
		r.Seconds = strconv.FormatUint(uint64(seconds), 10)
	}
	input, inKnown, _ := counter(rfc2866.AcctInputOctets_Type, "Acct-Input-Octets")
	output, outKnown, _ := counter(rfc2866.AcctOutputOctets_Type, "Acct-Output-Octets")
	inHigh, inHighKnown, inHighPresent := counter(rfc2869.AcctInputGigawords_Type, "Acct-Input-Gigawords")
	outHigh, outHighKnown, outHighPresent := counter(rfc2869.AcctOutputGigawords_Type, "Acct-Output-Gigawords")
	// An absent high word means zero. Valid fragments retain a lower bound,
	// but never become a complete reading when another fragment is malformed.
	if inKnown || inHighKnown {
		r.Upload = strconv.FormatUint(uint64(inHigh)<<32|uint64(input), 10)
	}
	if outKnown || outHighKnown {
		r.Download = strconv.FormatUint(uint64(outHigh)<<32|uint64(output), 10)
	}
	r.UploadKnown = inKnown && (inHighKnown || !inHighPresent)
	r.DownloadKnown = outKnown && (outHighKnown || !outHighPresent)
	r.ID = receiptIdentity(r)
	if r.Status == "nas-on" || r.Status == "nas-off" {
		r.Network = "" // A reboot marker belongs to the router, not the first enrolled HotSpot.
		r.ID = Digest("nas-marker:" + n.RouterID + ":" + r.Status + ":" + hex.EncodeToString(p.Authenticator[:]) + ":" + strconv.Itoa(int(p.Identifier)))
	}
	return r
}
