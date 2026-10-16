package activity

import (
	"reflect"
	"testing"
)

func TestDNSAnswerGeographyKeepsEvidenceAndUnitsSeparate(t *testing.T) {
	first := Event{At: 1000, Kind: "dns", Domain: "synthetic.example", Client: "synthetic", DNSIPs: []string{"198.51.100.7", "2001:db8::7", "203.0.113.8", "198.51.100.7"}}
	firewall := Event{At: 1000, Kind: "firewall", Destination: "203.0.113.4:443"}
	lookup := func(ip string) (string, string) {
		switch ip {
		case "198.51.100.7":
			return "YE", "Synthetic city"
		case "2001:db8::7":
			return "DE", "Other synthetic city"
		case "203.0.113.4":
			return "FR", "Firewall city"
		default:
			return "", ""
		}
	}
	events := []Event{first, first, {At: 1000, Kind: "dns", Domain: "legacy.example"}, firewall}
	walk := func(a, b int64, fn func(Event) error) error {
		for _, e := range events {
			if e.At >= a && e.At < b {
				if err := fn(e); err != nil {
					return err
				}
			}
		}
		return nil
	}
	f := Filter{From: 1000, To: 2000, View: "domains", Group: "dnsCountry", Page: 1, Limit: 50}
	out, err := Analyze(f, nil, "UTC", walk, lookup)
	if err != nil || !out.GeoAvailable || out.Totals.DNS != 3 || out.Totals.Firewall != 1 || len(out.DNSCountries) != 3 || len(out.Countries) != 1 || out.Countries[0].Key != "FR" || out.Countries[0].Count != 1 || len(out.Rows) != 3 {
		t.Fatal("geography source/units mixed", out, err)
	}
	for _, r := range out.DNSCountries {
		if r.Count != 2 {
			t.Fatal("answers were not deduplicated per event", r)
		}
	}
	Enrich(&first, "UTC", lookup)
	if first.Destination != "" || first.Country != "Unknown" || !reflect.DeepEqual(first.DNSAnswers, []DNSAnswer{{"198.51.100.7", "YE", "Synthetic city"}, {"2001:db8::7", "DE", "Other synthetic city"}, {"203.0.113.8", "Unknown", ""}}) {
		t.Fatal("answer geography invented a connected destination", first)
	}
	// All-known answers must also remain separate from the connected destination.
	known := first
	known.DNSIPs = known.DNSIPs[:2]
	Enrich(&known, "UTC", lookup)
	if known.Country != "Unknown" || known.City != "" || known.Destination != "" {
		t.Fatal("answer geography invented a connected destination", known)
	}
	for _, cc := range []string{"YE", "DE", "Unknown"} {
		f.DNSCountry = cc
		filtered, err := Analyze(f, nil, "UTC", walk, lookup)
		if err != nil || filtered.Totals.DNS != 2 || filtered.Totals.Firewall != 0 {
			t.Fatal("DNS country filter lost event semantics", cc, filtered, err)
		}
	}
	f.DNSCountry = "FR"
	filtered, err := Analyze(f, nil, "UTC", walk, lookup)
	if err != nil || filtered.Totals.DNS != 0 || filtered.Totals.Firewall != 0 {
		t.Fatal("DNS country filter matched a firewall destination", filtered, err)
	}
	f.DNSCountry = ""
	f.Country = "Unknown"
	filtered, err = Analyze(f, nil, "UTC", walk, lookup)
	if err != nil || filtered.Totals.DNS != 0 || filtered.Totals.Firewall != 0 {
		t.Fatal("Firewall Unknown filter matched DNS records", filtered, err)
	}
	f.Country = ""
	f.Kind = "firewall"
	filtered, err = Analyze(f, nil, "UTC", walk, lookup)
	if err != nil || len(filtered.DNSCountries) != 0 || filtered.Totals.Firewall != 1 {
		t.Fatal("source filter ignored", filtered, err)
	}
	f.Kind = ""
	missing, err := Analyze(f, nil, "UTC", walk)
	if err != nil || missing.GeoAvailable || len(missing.DNSCountries) != 1 || missing.DNSCountries[0].Key != "Unknown" || missing.DNSCountries[0].Count != 6 {
		t.Fatal("missing database hid recorded answer evidence", missing, err)
	}
	// Derived locations refresh at query time and never retain stale countries.
	Enrich(&first, "UTC", nil)
	if first.DNSAnswers[0].Country != "Unknown" || first.DNSAnswers[0].City != "" {
		t.Fatal("stale geography survived missing database", first)
	}
}
