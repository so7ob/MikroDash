// Package subscriber owns commercial entitlement and accounting independently of
// display collectors. Quantities use signed 64-bit storage and decimal JSON strings.
package subscriber

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
	_ "time/tzdata"
)

type Quantity int64

func (q Quantity) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"%d"`, q)), nil }
func (q *Quantity) UnmarshalJSON(b []byte) error {
	var n int64
	if len(b) < 3 || b[0] != '"' || b[len(b)-1] != '"' {
		return errors.New("quantities must be decimal strings")
	}
	for _, c := range b[1 : len(b)-1] {
		if c < '0' || c > '9' {
			return errors.New("invalid quantity")
		}
	}
	if _, err := fmt.Sscan(string(b[1:len(b)-1]), &n); err != nil {
		return errors.New("quantity exceeds signed 64-bit range")
	}
	*q = Quantity(n)
	return nil
}

type Period struct {
	Kind   string    `json:"kind"` // none, hourly, daily, weekly, monthly
	Zone   string    `json:"zone"`
	Anchor time.Time `json:"anchor"`
}

type Limit struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`  // seconds, download, upload, total
	State  string   `json:"state"` // limited, unlimited, disabled, unknown
	Amount Quantity `json:"amount"`
	Period Period   `json:"period"`
	Action string   `json:"action"` // deny, disconnect, throttle, alert
	Down   Quantity `json:"down"`
	Up     Quantity `json:"up"`
}

type Window struct {
	Days  []int  `json:"days"`  // weekday of the opening edge, Sunday = 0
	From  int    `json:"from"`  // seconds from midnight, inclusive
	Until int    `json:"until"` // seconds from midnight, exclusive; 86400 allowed
	Zone  string `json:"zone"`
}

type Policy struct {
	ID      string   `json:"id"`
	Limits  []Limit  `json:"limits"`
	Windows []Window `json:"windows"`
}

type Speed struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Down Quantity `json:"down"`
	Up   Quantity `json:"up"`
}

type Plan struct {
	ID           string   `json:"id"`
	Version      int      `json:"version"`
	Name         string   `json:"name"`
	ValidSeconds Quantity `json:"validSeconds"`
	Start        string   `json:"start"` // assigned or first-login
	MaxSessions  int      `json:"maxSessions"`
	Networks     []string `json:"networks"`
	Policies     []Policy `json:"policies"`
	Speeds       []Speed  `json:"speeds"`
	Down         Quantity `json:"down"`
	Up           Quantity `json:"up"`
}

func (p Plan) Validate() error {
	if p.ID == "" || p.Name == "" || p.Version < 1 || p.MaxSessions < 1 || p.MaxSessions > 64 || p.ValidSeconds <= 0 || p.ValidSeconds > 315360000 || len(p.Networks) == 0 || len(p.Policies) == 0 || len(p.Speeds) == 0 || p.Down <= 0 || p.Up <= 0 || p.Down > 1_000_000_000_000 || p.Up > 1_000_000_000_000 {
		return errors.New("plan requires identity, validity, networks, policies, speeds and bounded sessions")
	}
	if p.Start != "assigned" && p.Start != "first-login" {
		return errors.New("invalid validity start")
	}
	ids := map[string]bool{}
	for _, po := range p.Policies {
		if po.ID == "" || ids["p:"+po.ID] {
			return errors.New("policy IDs must be unique")
		}
		ids["p:"+po.ID] = true
		for _, l := range po.Limits {
			if l.ID == "" || ids["l:"+l.ID] {
				return errors.New("limit IDs must be unique")
			}
			ids["l:"+l.ID] = true
			if !slices.Contains([]string{"seconds", "download", "upload", "total"}, l.Kind) || !slices.Contains([]string{"limited", "unlimited", "disabled", "unknown"}, l.State) || !slices.Contains([]string{"deny", "disconnect", "throttle", "alert"}, l.Action) || l.Amount < 0 {
				return errors.New("invalid limit")
			}
			if l.Action == "throttle" && (l.Down <= 0 || l.Up <= 0) {
				return errors.New("throttle requires positive rate ceilings")
			}
			if _, _, err := l.Period.Bounds(time.Now()); err != nil {
				return err
			}
		}
		for _, w := range po.Windows {
			if _, err := time.LoadLocation(w.Zone); err != nil || w.From < 0 || w.From >= 86400 || w.Until < 0 || w.Until > 86400 || w.From == w.Until || len(w.Days) == 0 {
				return errors.New("invalid access window")
			}
			for _, d := range w.Days {
				if d < 0 || d > 6 {
					return errors.New("invalid weekday")
				}
			}
		}
	}
	for _, s := range p.Speeds {
		if s.ID == "" || ids["s:"+s.ID] || s.Down <= 0 || s.Up <= 0 {
			return errors.New("invalid speed profile")
		}
		ids["s:"+s.ID] = true
	}
	return nil
}

// Bounds uses calendar arithmetic. Monthly anchors keep their original day,
// clamping to the last day of a shorter month without drifting later months.
func (p Period) Bounds(t time.Time) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(p.Zone)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid period time zone")
	}
	a := p.Anchor.In(loc)
	t = t.In(loc)
	if p.Anchor.IsZero() {
		return time.Time{}, time.Time{}, errors.New("period anchor required")
	}
	var start, end time.Time
	switch p.Kind {
	case "none":
		return a, time.Date(9999, 1, 1, 0, 0, 0, 0, loc), nil
	case "hourly":
		start = t.Add(-time.Duration(t.Minute()*60+t.Second()) * time.Second).Truncate(time.Second).Add(time.Duration(a.Minute()*60+a.Second()) * time.Second)
		if start.After(t) {
			start = start.Add(-time.Hour)
		}
		end = start.Add(time.Hour)
	case "daily", "weekly":
		start = time.Date(t.Year(), t.Month(), t.Day(), a.Hour(), a.Minute(), a.Second(), 0, loc)
		if start.After(t) {
			start = start.AddDate(0, 0, -1)
		}
		days := 1
		if p.Kind == "weekly" {
			delta := (int(start.Weekday()) - int(a.Weekday()) + 7) % 7
			start = start.AddDate(0, 0, -delta)
			days = 7
		}
		end = start.AddDate(0, 0, days)
	case "monthly":
		edge := func(year int, month time.Month) time.Time {
			first := time.Date(year, month, 1, a.Hour(), a.Minute(), a.Second(), 0, loc)
			last := time.Date(first.Year(), first.Month()+1, 0, a.Hour(), a.Minute(), a.Second(), 0, loc).Day()
			return time.Date(first.Year(), first.Month(), min(a.Day(), last), a.Hour(), a.Minute(), a.Second(), 0, loc)
		}
		start = edge(t.Year(), t.Month())
		if start.After(t) {
			start = edge(t.Year(), t.Month()-1)
		}
		end = edge(start.Year(), start.Month()+1)
	default:
		return start, end, errors.New("invalid period kind")
	}
	return start, end, nil
}

func (w Window) Allows(t time.Time) bool {
	loc, err := time.LoadLocation(w.Zone)
	if err != nil {
		return false
	}
	t = t.In(loc)
	sec := t.Hour()*3600 + t.Minute()*60 + t.Second()
	day := int(t.Weekday())
	if w.From < w.Until {
		return slices.Contains(w.Days, day) && sec >= w.From && sec < w.Until
	}
	if sec >= w.From {
		return slices.Contains(w.Days, day)
	}
	return sec < w.Until && slices.Contains(w.Days, (day+6)%7)
}

type Usage struct {
	Provisional Quantity `json:"provisional"`
	Used        Quantity `json:"used"`
	Reserved    Quantity `json:"reserved"`
	Quality     string   `json:"quality"`
}

type LimitView struct {
	Provisional Quantity  `json:"provisional"`
	Limit       Limit     `json:"limit"`
	Used        Quantity  `json:"used"`
	Remaining   Quantity  `json:"remaining"`
	Reserved    Quantity  `json:"reserved"`
	PeriodStart time.Time `json:"periodStart"`
	NextReset   time.Time `json:"nextReset"`
	Quality     string    `json:"quality"`
}

type Decision struct {
	Allowed    bool        `json:"allowed"`
	Disconnect bool        `json:"disconnect"`
	Reasons    []string    `json:"reasons"`
	Limits     []LimitView `json:"limits"`
	Down       Quantity    `json:"down"`
	Up         Quantity    `json:"up"`
	Timeout    int64       `json:"timeout"`
}

func Key(id string, start time.Time) string { return fmt.Sprintf("%s:%d", id, start.Unix()) }

// Evaluate intersects every policy. An empty usage map means trusted zero from
// a successful ledger query; a failed query must never call this function.
func Evaluate(p Plan, speed string, at, expires time.Time, usage map[string]Usage) Decision {
	d := Decision{Allowed: true, Reasons: []string{}, Limits: []LimitView{}, Down: p.Down, Up: p.Up, Timeout: 60}
	if err := p.Validate(); err != nil {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "invalid-plan")
		return d
	}
	found := false
	for _, s := range p.Speeds {
		if s.ID == speed {
			found = true
			d.Down = min(d.Down, s.Down)
			d.Up = min(d.Up, s.Up)
		}
	}
	if !found {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "speed-not-entitled")
	}
	if !expires.IsZero() {
		d.Timeout = min(d.Timeout, int64(expires.Sub(at)/time.Second))
		if !at.Before(expires) {
			d.Allowed = false
			d.Disconnect = true
			d.Reasons = append(d.Reasons, "subscription-expired")
		}
	}
	for _, po := range p.Policies {
		if len(po.Windows) > 0 {
			allowed := false
			for _, w := range po.Windows {
				allowed = allowed || w.Allows(at)
			}
			if !allowed {
				d.Allowed = false
				d.Disconnect = true
				d.Reasons = append(d.Reasons, "schedule:"+po.ID)
			}
			// A delegation never crosses an access-window closing edge.
			for n := int64(1); n <= d.Timeout; n++ {
				ok := false
				for _, w := range po.Windows {
					ok = ok || w.Allows(at.Add(time.Duration(n)*time.Second))
				}
				if !ok {
					d.Timeout = n
					break
				}
			}
		}
		for _, l := range po.Limits {
			start, end, _ := l.Period.Bounds(at)
			u := usage[Key(l.ID, start)]
			reserved := max(u.Reserved, u.Provisional)
			remaining := Quantity(0)
			if u.Used < l.Amount && reserved < l.Amount-u.Used {
				remaining = l.Amount - u.Used - reserved
			}
			v := LimitView{Limit: l, Used: u.Used, Provisional: u.Provisional, Reserved: reserved, Remaining: remaining, PeriodStart: start, NextReset: end, Quality: u.Quality}
			if v.Quality == "" {
				v.Quality = "fresh"
			}
			d.Limits = append(d.Limits, v)
			if l.State == "disabled" || l.State == "unlimited" {
				continue
			}
			if l.State == "unknown" || at.Before(l.Period.Anchor) || u.Quality == "unknown" || u.Used < 0 || reserved < 0 || u.Provisional < 0 {
				d.Allowed = false
				d.Reasons = append(d.Reasons, "unknown:"+l.ID)
				continue
			}
			if l.Action == "deny" || l.Action == "disconnect" {
				d.Timeout = min(d.Timeout, int64(end.Sub(at)/time.Second))
				if l.Kind == "seconds" {
					d.Timeout = min(d.Timeout, int64(v.Remaining))
				}
			}
			if v.Remaining == 0 {
				d.Reasons = append(d.Reasons, "exhausted:"+l.ID)
				switch l.Action {
				case "deny":
					d.Allowed = false
				case "disconnect":
					d.Allowed = false
					d.Disconnect = true
				case "throttle":
					d.Down = min(d.Down, l.Down)
					d.Up = min(d.Up, l.Up)
				}
			}
		}
	}
	if d.Timeout <= 0 {
		d.Allowed = false
	}
	return d
}

func add(a, b Quantity) (Quantity, error) {
	if a < 0 || b < 0 || a > Quantity(math.MaxInt64)-b {
		return 0, errors.New("counter overflow")
	}
	return a + b, nil
}
