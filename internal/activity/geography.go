package activity

import (
	"time"
)

// Enrich uses only the explicit destination address in the record. It never
// resolves a domain now or associates adjacent DNS/firewall messages.
func Enrich(e *Event, zone string, lookup func(string) (string, string)) {
	e.Country = "Unknown"
	e.City = ""
	if ip := endpointIP(e.Destination); ip != "" && lookup != nil {
		if cc, city := lookup(ip); cc != "" {
			e.Country = cc
			e.City = city
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
