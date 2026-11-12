package activity

import (
	"strings"
	"time"
)

// Enrich uses only addresses supplied in the event. DNS answers and connected
// destinations keep separate geography; no live DNS or temporal joins occur.
func Enrich(e *Event, zone string, lookup func(string) (string, string)) {
	NewEvaluator(zone, nil, lookup).Enrich(e)
}

// Evaluator shares bounded domain/IP work and a loaded location within one query.
// It is deliberately query scoped: rules and GeoIP can change between requests.
type Evaluator struct {
	Location *time.Location
	rules    []Rule
	lookup   func(string) (string, string)
	places   map[string][2]string
	classes  map[string][2]string
}

func NewEvaluator(zone string, rules []Rule, lookup func(string) (string, string)) *Evaluator {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return &Evaluator{Location: loc, rules: rules, lookup: lookup, places: map[string][2]string{}, classes: map[string][2]string{}}
}
func (v *Evaluator) Classify(e *Event) {
	if e.Kind != "dns" {
		return
	}
	value, ok := v.classes[e.Domain]
	if !ok {
		value[0], value[1] = Classify(e.Domain, v.rules)
		if len(v.classes) < 65536 {
			v.classes[e.Domain] = value
		}
	}
	e.Service, e.Category = value[0], value[1]
}
func (v *Evaluator) Enrich(e *Event) {
	if e.DNSIPs == nil {
		e.DNSIPs = []string{}
	}
	e.DNSAnswers = []DNSAnswer{}
	e.Country, e.City = "Unknown", ""
	locate := func(ip string) (string, string) {
		if found, ok := v.places[ip]; ok {
			return found[0], found[1]
		}
		value := [2]string{"Unknown", ""}
		if v.lookup != nil {
			if cc, city := v.lookup(ip); cc != "" {
				value = [2]string{cc, city}
			}
		}
		if len(v.places) < 65536 {
			v.places[ip] = value
		}
		return value[0], value[1]
	}
	if ip := endpointIP(e.Destination); ip != "" {
		e.Country, e.City = locate(ip)
	}
	if e.Kind == "dns" {
		seen := map[string]bool{}
		for _, ip := range e.DNSIPs {
			if seen[ip] {
				continue
			}
			seen[ip] = true
			cc, city := locate(ip)
			e.DNSAnswers = append(e.DNSAnswers, DNSAnswer{IP: ip, Country: cc, City: city})
		}
	}
	t := time.UnixMilli(e.At).In(v.Location)
	e.Day = t.Format("2006-01-02")
	e.Hour = t.Format("2006-01-02 15:00")
}

// SearchText is the exact literal, Unicode-folded search contract used by both
// the stream fallback and the indexed shape query.
func SearchText(e Event) string {
	return strings.ToLower(strings.Join([]string{e.Domain, e.Service, e.Category, e.User, e.IP, e.MAC, e.Server, e.Destination, e.Protocol, e.Canonical, e.DeviceName, e.IPBasis, e.QType, e.Match, strings.Join(e.DNSIPs, " ")}, " "))
}
