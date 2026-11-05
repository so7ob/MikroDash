package subscriberlab

import (
	"errors"
	"strconv"
	"time"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

func verifyProbeBefore(v subscriber.Snapshot) error {
	if v.LifetimeSeconds != 0 || v.LifetimeDownload != 0 || v.LifetimeUpload != 0 || len(v.Sessions) != 0 || v.PendingAccounting != 0 {
		return errors.New("fresh protocol fixture already has consumption or uncertainty")
	}
	for _, limit := range v.Decision.Limits {
		if limit.Limit.ID == "time" && limit.Used == 0 && limit.Remaining == 3600 {
			return nil
		}
	}
	return errors.New("initial one-hour entitlement not proven")
}

// These bounded lab probes use the qualified firmware's h/m/s API format.
// Reject unsupported or malformed samples rather than coercing them to zero.
func probeSeconds(raw string) (int64, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 || d%time.Second != 0 {
		return 0, errors.New("valid whole-second lab duration required")
	}
	return int64(d / time.Second), nil
}

func verifyTotalTimeout(row routeros.Reply, total int) error {
	uptime, uptimeErr := probeSeconds(row["uptime"])
	left, leftErr := probeSeconds(row["session-time-left"])
	if uptimeErr != nil || leftErr != nil {
		return errors.New("local timeout readback is missing or malformed")
	}
	actual := uptime + left
	if actual < int64(total)-1 || actual > int64(total) {
		return errors.New("CoA readback does not prove total-session timeout semantics")
	}
	return nil
}

func verifyProbeFinal(before, after subscriber.Snapshot, sample routeros.Reply, session string) error {
	if after.PendingAccounting != 0 || len(after.Sessions) != 1 || after.Sessions[0].State != "stopped" || after.Sessions[0].SessionID != session {
		return errors.New("covering committed Stop not proven")
	}
	if before.Subscription.ID != after.Subscription.ID || before.Subscription.SpeedID != after.Subscription.SpeedID || before.Subscription.AssignedAt != after.Subscription.AssignedAt {
		return errors.New("wire probes changed subscription identity or saved selection")
	}
	up, upErr := strconv.ParseInt(sample["bytes-in"], 10, 64)
	down, downErr := strconv.ParseInt(sample["bytes-out"], 10, 64)
	seconds, secondsErr := probeSeconds(sample["uptime"])
	if secondsErr != nil || upErr != nil || downErr != nil || up < 0 || down < 0 || after.LifetimeUpload != subscriber.Quantity(up) || after.LifetimeDownload != subscriber.Quantity(down) || after.LifetimeSeconds < subscriber.Quantity(seconds) || after.LifetimeSeconds > subscriber.Quantity(seconds+2) {
		return errors.New("final RADIUS/ledger counters do not match the settled API sample")
	}
	for _, limit := range after.Decision.Limits {
		var used subscriber.Quantity
		switch limit.Limit.Kind {
		case "seconds":
			used = after.LifetimeSeconds
		case "download":
			used = after.LifetimeDownload
		case "upload":
			used = after.LifetimeUpload
		}
		if limit.Used != used || limit.Remaining != limit.Limit.Amount-used || limit.Reserved != 0 {
			return errors.New("independent limit usage, balance or reservation is inconsistent")
		}
	}
	return nil
}
