package server

import (
	"regexp"
	"strconv"

	"mikrodash/internal/subscriber"
)

var (
	subscriberDecimalSeconds = regexp.MustCompile(`^[0-9]+$`)
	subscriberClockSeconds   = regexp.MustCompile(`^(?:([0-9]+)w)?(?:([0-9]+)d)?([0-9]+):([0-5]?[0-9]):([0-5]?[0-9])(?:\.[0-9]{1,9})?$`)
	subscriberUnitSeconds    = regexp.MustCompile(`^(?:([0-9]+)w)?(?:([0-9]+)d)?(?:([0-9]+)h)?(?:([0-9]+)m)?(?:([0-9]+)s)?(?:([0-9]+)ms)?$`)
)

// subscriberDuration validates the complete field before returning integer
// seconds. Missing/unsupported/overflowing samples remain unknown. The bound
// matches the ledger's supported session duration; no architecture-sized int or
// nanosecond multiplication can wrap an observation's session identity.
// Millisecond/clock fractions are floored only after validation.
func subscriberDuration(raw string) (subscriber.Quantity, bool) {
	const maximum uint64 = 315360000
	if raw == "" {
		return 0, false
	}
	var groups []string
	var scales []uint64
	if subscriberDecimalSeconds.MatchString(raw) {
		groups = []string{raw}
		scales = []uint64{1}
	} else if match := subscriberClockSeconds.FindStringSubmatch(raw); match != nil {
		groups = match[1:]
		scales = []uint64{604800, 86400, 3600, 60, 1}
	} else if match := subscriberUnitSeconds.FindStringSubmatch(raw); match != nil {
		groups = match[1:]
		scales = []uint64{604800, 86400, 3600, 60, 1, 0}
	} else {
		return 0, false
	}
	var seconds uint64
	for i, group := range groups {
		if group == "" {
			continue
		}
		value, err := strconv.ParseUint(group, 10, 64)
		if err != nil {
			return 0, false
		}
		scale := scales[i]
		if scale == 0 {
			value /= 1000
			scale = 1
		}
		if value > maximum/scale {
			return 0, false
		}
		amount := value * scale
		if amount > maximum-seconds {
			return 0, false
		}
		seconds += amount
	}
	return subscriber.Quantity(seconds), true
}
