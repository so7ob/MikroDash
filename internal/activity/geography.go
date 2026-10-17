package activity

import "time"

// Enrich uses only addresses supplied in the event. DNS answers and connected
// destinations keep separate geography; no live DNS or temporal joins occur.
func Enrich(e *Event, zone string, lookup func(string) (string, string)) {
	if e.DNSIPs == nil {
		e.DNSIPs = []string{}
	}
	e.DNSAnswers = []DNSAnswer{}
	e.Country, e.City = "Unknown", ""
	locate := func(ip string) (string, string) {
		if lookup != nil {
			if cc, city := lookup(ip); cc != "" {
				return cc, city
			}
		}
		return "Unknown", ""
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
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	t := time.UnixMilli(e.At).In(loc)
	e.Day = t.Format("2006-01-02")
	e.Hour = t.Format("2006-01-02 15:00")
}
