package subscriber

import (
	"sort"
	"time"
)

type periodReservation struct {
	LimitID string
	Start   time.Time
	Amount  Quantity
	kind    string
}

func hardLimit(l Limit) bool {
	return l.State == "limited" && (l.Action == "deny" || l.Action == "disconnect")
}

// planDelegation is pure. Callers load all periods and commit these allocations
// with their lease/operation in the same immediate transaction. Time starts at
// the NAS high-water mark, including unobserved elapsed time, not API completion.
// A byte cap is reserved in every touched period because its split is unknown.
func planDelegation(p Plan, speed string, at, from, expires time.Time, usage map[string]Usage, slots Quantity) (Admission, []periodReservation, time.Time, error) {
	return boundedDelegation(p, speed, at, from, expires, usage, slots, 60)
}

func boundedDelegation(p Plan, speed string, at, from, expires time.Time, usage map[string]Usage, slots Quantity, span int64) (Admission, []periodReservation, time.Time, error) {
	a := Admission{Decision: evaluate(p, speed, at, expires, usage, false)}
	if !a.Decision.Allowed {
		return a, nil, time.Time{}, denyAdmission(a.Decision.Reasons...)
	}
	if slots < 1 || span < 1 || span > 60 || from.Unix() > at.Unix() || at.Unix()-from.Unix() > 60 {
		return a, nil, time.Time{}, denyAdmission("delegation-context-invalid")
	}
	end := min(at.Unix()+a.Decision.Timeout, from.Unix()+span)
	allocations := map[string]periodReservation{}
	cursor := from.Unix()
	for cursor < end {
		point := time.Unix(cursor, 0)
		d := evaluate(p, speed, point, expires, usage, false)
		if !d.Allowed {
			break
		}
		edge := min(end, cursor+d.Timeout)
		for _, v := range d.Limits {
			if v.Limit.State == "disabled" || v.Limit.State == "unlimited" {
				continue
			}
			edge = min(edge, v.NextReset.Unix())
			if !hardLimit(v.Limit) {
				continue
			}
			budget := v.Remaining / slots
			if budget <= 0 {
				edge = cursor
				break
			}
			if v.Limit.Kind == "seconds" {
				budget -= allocations[Key(v.Limit.ID, v.PeriodStart)].Amount
				edge = min(edge, cursor+int64(min(Quantity(60), max(Quantity(0), budget))))
			}
		}
		if edge <= cursor {
			break
		}
		a.Decision.Down = min(a.Decision.Down, d.Down)
		a.Decision.Up = min(a.Decision.Up, d.Up)
		for _, v := range d.Limits {
			l := v.Limit
			if !hardLimit(l) {
				continue
			}
			key := Key(l.ID, v.PeriodStart)
			r := allocations[key]
			r.LimitID, r.Start, r.kind = l.ID, v.PeriodStart, l.Kind
			switch l.Kind {
			case "seconds":
				r.Amount += Quantity(edge - cursor)
			case "download":
				a.Download = smallest(a.Download, v.Remaining/slots)
			case "upload":
				a.Upload = smallest(a.Upload, v.Remaining/slots)
			case "total":
				a.Total = smallest(a.Total, v.Remaining/slots)
			}
			allocations[key] = r
		}
		cursor = edge
	}
	// A counter sample at the exact reset belongs to an uncertain byte gap
	// touching both periods. Time is half-open; bytes need endpoint coverage.
	for _, policy := range p.Policies {
		for _, limit := range policy.Limits {
			if !hardLimit(limit) || limit.Kind == "seconds" {
				continue
			}
			start, _, err := limit.Period.Bounds(time.Unix(cursor, 0))
			if err != nil {
				return a, nil, time.Time{}, err
			}
			key := Key(limit.ID, start)
			if _, exists := allocations[key]; exists {
				continue
			}
			budget, known := limitBudget(limit, start, usage, slots)
			if !known || budget <= 0 {
				return boundedDelegation(p, speed, at, from, expires, usage, slots, span-1)
			}
			allocations[key] = periodReservation{LimitID: limit.ID, Start: start, kind: limit.Kind}
			switch limit.Kind {
			case "download":
				a.Download = smallest(a.Download, budget)
			case "upload":
				a.Upload = smallest(a.Upload, budget)
			case "total":
				a.Total = smallest(a.Total, budget)
			}
		}
	}
	a.Decision.Timeout = cursor - at.Unix()
	if a.Decision.Timeout <= 0 {
		return a, nil, time.Time{}, denyAdmission("insufficient-shared-budget")
	}
	// Rates are validated <= 1e12 bit/s and the interval is <= 60 seconds, so
	// these products fit signed 64-bit storage without floating-point arithmetic.
	a.Download = smallest(a.Download, Quantity(a.Decision.Timeout)*a.Decision.Down/8)
	a.Upload = smallest(a.Upload, Quantity(a.Decision.Timeout)*a.Decision.Up/8)
	if a.Download <= 0 || a.Upload <= 0 {
		return a, nil, time.Time{}, denyAdmission("delegation-unavailable")
	}
	keys := make([]string, 0, len(allocations))
	for key := range allocations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reservations := make([]periodReservation, 0, len(keys))
	for _, key := range keys {
		r := allocations[key]
		switch r.kind {
		case "download":
			r.Amount = a.Download
		case "upload":
			r.Amount = a.Upload
		case "total":
			r.Amount = min(a.Total, a.Download+a.Upload)
		}
		reservations = append(reservations, r)
	}
	return a, reservations, time.Unix(cursor, 0), nil
}

func limitBudget(limit Limit, start time.Time, usage map[string]Usage, slots Quantity) (Quantity, bool) {
	u := usage[Key(limit.ID, start)]
	reserved := max(u.Reserved, u.Provisional)
	if slots < 1 || limit.State == "unknown" || u.Quality == "unknown" || u.Used < 0 || reserved < 0 || u.Provisional < 0 {
		return 0, false
	}
	if u.Used >= limit.Amount || reserved >= limit.Amount-u.Used {
		return 0, true
	}
	return (limit.Amount - u.Used - reserved) / slots, true
}
