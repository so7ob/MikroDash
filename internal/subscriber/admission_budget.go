package subscriber

import (
	"math"
	"time"
)

// These are the existing accepted accounting-start bounds. A relative NAS
// timeout can begin anywhere in this interval; reserve its worst exposure in
// every possible period until a trusted Start and final accounting settle it.
const admissionStartLead = int64(5)
const admissionStartLag = int64(60)
const admissionTimingBasis = "nas-start-window-v1"

func planAdmission(p Plan, speed string, at, expires time.Time, usage map[string]Usage, slots Quantity) (Admission, []periodReservation, time.Time, error) {
	a := Admission{Decision: evaluate(p, speed, at, expires, usage, false), TimingBasis: admissionTimingBasis}
	if !a.Decision.Allowed || slots < 1 || at.Unix() > math.MaxInt64-120 || at.Unix() < math.MinInt64+admissionStartLead {
		return a, nil, time.Time{}, denyAdmission(a.Decision.Reasons...)
	}
	seconds := int64(60)
	latest := at.Unix() + admissionStartLag
	earliest := at.Unix() - admissionStartLead
	if !expires.IsZero() {
		seconds = min(seconds, max(int64(0), expires.Unix()-latest))
	}
	// Without an absolute, verified NAS termination attribute, every possible
	// start must still end before a hard window closes. Never promise a strict
	// wall-clock cutoff using a relative timeout and an assumed immediate start.
	for n := int64(0); n < admissionStartLag+seconds; n++ {
		point := at.Add(time.Duration(n) * time.Second)
		for _, policy := range p.Policies {
			if len(policy.Windows) == 0 {
				continue
			}
			allowed := false
			for _, window := range policy.Windows {
				allowed = allowed || window.Allows(point)
			}
			if !allowed {
				seconds = min(seconds, max(int64(0), n-admissionStartLag))
			}
		}
	}
	// The duration only decreases. Recompute after shortening so unreachable
	// periods cannot leave an unnecessary byte reservation or rate reduction.
	for attempts := 0; attempts < 61 && seconds > 0; attempts++ {
		before := seconds
		candidate := a
		candidate.Download, candidate.Upload, candidate.Total = 0, 0, 0
		reservations := []periodReservation{}
		for _, policy := range p.Policies {
			for _, limit := range policy.Limits {
				if limit.State == "disabled" || limit.State == "unlimited" {
					continue
				}
				for cursor := earliest; cursor < latest+seconds || (limit.Kind != "seconds" && cursor == latest+seconds); {
					start, end, err := limit.Period.Bounds(time.Unix(cursor, 0))
					if err != nil || end.Unix() <= cursor {
						return a, nil, time.Time{}, denyAdmission("invalid-delegation-period")
					}
					overlap := max(int64(0), min(seconds, end.Unix()-start.Unix(), end.Unix()-earliest, latest+seconds-start.Unix()))
					if overlap > 0 || (limit.Kind != "seconds" && cursor == latest+seconds) {
						u := usage[Key(limit.ID, start)]
						reserved := max(u.Reserved, u.Provisional)
						unknown := limit.State == "unknown" || u.Quality == "unknown" || u.Used < 0 || reserved < 0 || u.Provisional < 0
						remaining := Quantity(0)
						if !unknown && u.Used < limit.Amount && reserved < limit.Amount-u.Used {
							remaining = limit.Amount - u.Used - reserved
							if hardLimit(limit) {
								remaining /= slots
							}
						}
						if unknown || (hardLimit(limit) && remaining == 0) {
							cutoff := start.Unix() - latest
							if limit.Kind != "seconds" {
								cutoff--
							}
							seconds = min(seconds, max(int64(0), cutoff))
						} else if hardLimit(limit) {
							amount := remaining
							if limit.Kind == "seconds" {
								if Quantity(overlap) > remaining {
									// overlap <= budget iff duration is no larger than
									// budget plus any distance beyond the latest start.
									seconds = min(seconds, int64(min(remaining, Quantity(60)))+max(int64(0), start.Unix()-latest))
								}
								amount = Quantity(overlap)
							} else {
								switch limit.Kind {
								case "download":
									candidate.Download = smallest(candidate.Download, remaining)
								case "upload":
									candidate.Upload = smallest(candidate.Upload, remaining)
								case "total":
									candidate.Total = smallest(candidate.Total, remaining)
								}
							}
							reservations = append(reservations, periodReservation{LimitID: limit.ID, Start: start, Amount: amount, kind: limit.Kind})
						} else if limit.Action == "throttle" && remaining == 0 && end.Unix() > at.Unix() {
							candidate.Decision.Down = min(candidate.Decision.Down, limit.Down)
							candidate.Decision.Up = min(candidate.Decision.Up, limit.Up)
						}
					}
					cursor = end.Unix()
				}
			}
		}
		if seconds != before {
			continue
		}
		candidate.Decision.Timeout = seconds
		candidate.Download = smallest(candidate.Download, Quantity(seconds)*candidate.Decision.Down/8)
		candidate.Upload = smallest(candidate.Upload, Quantity(seconds)*candidate.Decision.Up/8)
		if candidate.Download <= 0 || candidate.Upload <= 0 {
			break
		}
		for i := range reservations {
			switch reservations[i].kind {
			case "download":
				reservations[i].Amount = candidate.Download
			case "upload":
				reservations[i].Amount = candidate.Upload
			case "total":
				reservations[i].Amount = min(candidate.Total, candidate.Download+candidate.Upload)
			}
		}
		return candidate, reservations, time.Unix(latest+seconds, 0), nil
	}
	return a, nil, time.Time{}, denyAdmission("initial-timing-budget-unavailable")
}
