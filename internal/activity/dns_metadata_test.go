package activity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func metadataMessage(t *testing.T, change func(map[string]any)) string {
	t.Helper()
	p := map[string]any{
		"Connection":  map[string]any{"server": "hs-test", "qtype": "A", "match": "manual-mac"},
		"Destination": map[string]any{"domain": "chatgpt.com", "canonical": "CDN.Example.", "dns_ips": []string{"198.51.100.7", "2001:0db8:0:0::7", "198.51.100.7"}, "ip_basis": "dns-cache"},
		"Source":      map[string]any{"user": "Synthetic PC", "ip": "198.51.100.25", "mac": "02:00:00:00:00:25", "device_name": "جهاز تجريبي"},
	}
	if change != nil {
		change(p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return "DNSUSER " + string(b)
}
func TestDNSMetadataPreservesAnswersAndHistoricalIdentity(t *testing.T) {
	now := time.Now().UnixMilli()
	e, yes, err := Parse("r1", "", "", "script,info", metadataMessage(t, nil), now, "UTC")
	if !yes || err != nil {
		t.Fatal(err)
	}
	if e.Domain != "chatgpt.com" || e.Canonical != "cdn.example" || e.IPBasis != "dns-cache" || e.DeviceName != "جهاز تجريبي" || !reflect.DeepEqual(e.DNSIPs, []string{"198.51.100.7", "2001:db8::7"}) {
		t.Fatalf("metadata lost: %#v", e)
	}
	if e.Destination != "" || e.QType != "A" {
		t.Fatal("answers invented a connection or query type", e)
	}
	changed, _, err := Parse("r1", "", "", "script,info", metadataMessage(t, func(p map[string]any) { p["Source"].(map[string]any)["device_name"] = "Renamed device" }), now, "UTC")
	if err != nil || changed.Client != e.Client {
		t.Fatal("device metadata reassigned the historical user")
	}
	calls := 0
	Enrich(&e, "UTC", func(string) (string, string) { calls++; return "YE", "Synthetic city" })
	if calls != 2 || e.Country != "Unknown" || len(e.DNSAnswers) != 2 || e.DNSAnswers[0].Country != "YE" || e.DNSAnswers[1].Country != "YE" {
		t.Fatal("DNS answer was misrepresented as a connected destination")
	}
	legacy, _, err := Parse("r1", "", "", "script,info", synthetic, now, "UTC")
	if err != nil || legacy.DNSIPs == nil || len(legacy.DNSIPs) != 0 || legacy.Canonical != "" || legacy.DeviceName != "" {
		t.Fatal("legacy compatibility", legacy, err)
	}
}
func TestDNSMetadataMissingAndInvalidFields(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(p map[string]any) { p["Destination"].(map[string]any)["dns_ips"] = []string{"not-an-ip"} },
		func(p map[string]any) { p["Destination"].(map[string]any)["dns_ips"] = []string{"fe80::1%test"} },
		func(p map[string]any) { p["Destination"].(map[string]any)["dns_ips"] = "bad-shape" },
		func(p map[string]any) { p["Destination"].(map[string]any)["dns_ips"] = make([]string, 129) },
		func(p map[string]any) { p["Destination"].(map[string]any)["canonical"] = "bad/name" },
		func(p map[string]any) { p["Source"].(map[string]any)["device_name"] = strings.Repeat("x", 257) },
		func(p map[string]any) { p["Source"].(map[string]any)["device_name"] = "bad\nname" },
	} {
		if _, yes, err := Parse("r1", "", "", "script,info", metadataMessage(t, change), 1, "UTC"); !yes || err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	e, _, err := Parse("r1", "", "", "script,info", metadataMessage(t, func(p map[string]any) {
		d := p["Destination"].(map[string]any)
		d["canonical"] = "-"
		d["dns_ips"] = nil
		d["ip_basis"] = "-"
		p["Source"].(map[string]any)["device_name"] = "-"
	}), 1, "UTC")
	if err != nil || e.Canonical != "" || e.IPBasis != "" || e.DeviceName != "" || e.DNSIPs == nil || len(e.DNSIPs) != 0 {
		t.Fatal("missing values became supplied metadata", e, err)
	}
}
func TestDNSMetadataAggregationFiltersAndAddressUnits(t *testing.T) {
	first, _, err := Parse("r1", "", "", "script,info", metadataMessage(t, nil), 1000000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	first.DNSIPs = append(first.DNSIPs, first.DNSIPs[0]) // Persisted/manual callers cannot inflate per-event address counts.
	legacy := Event{At: first.At, Kind: "dns", Domain: "legacy.example", Client: "legacy"}
	firewall := Event{At: first.At, Kind: "firewall", Destination: "203.0.113.4:443"}
	events := []Event{first, first, legacy, firewall}
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
	f := Filter{From: 1000000, To: 1001000, View: "summary", Group: "dnsIP", Page: 1, Limit: 50, Desc: true}
	out, err := Analyze(f, nil, "UTC", walk)
	if err != nil || out.Totals.DNS != 3 || out.Totals.Firewall != 1 || out.Totals.Answered != 2 || out.Totals.AnswerIPs != 2 || out.Totals.DeviceNames != 1 || out.Totals.Canonicals != 1 || len(out.Rows) != 2 {
		t.Fatal("wrong evidence totals", out, err)
	}
	for _, r := range out.Rows {
		if r.Count != 2 {
			t.Fatal("address occurrences counted more than once per event", r)
		}
	}
	for _, rows := range [][]Ranking{out.QueryTypes, out.MatchMethods, out.IPBases} {
		total := 0
		for _, r := range rows {
			total += r.Count
		}
		if total != 3 {
			t.Fatal("distribution lost legacy/missing records", rows)
		}
	}
	for _, filter := range []Filter{{DNSIP: "2001:db8::7"}, {DeviceName: first.DeviceName}, {Canonical: first.Canonical}, {IPBasis: "dns-cache"}, {Server: "hs-test"}, {QType: "A"}, {Match: "manual-mac"}, {Search: "2001:db8::7"}, {Search: first.DeviceName}} {
		filter.From, filter.To, filter.View, filter.Group, filter.Page, filter.Limit = f.From, f.To, "summary", "dnsIP", 1, 50
		got, err := Analyze(filter, nil, "UTC", walk)
		if err != nil || got.Totals.DNS != 2 || got.Totals.Firewall != 0 {
			t.Fatal("metadata filter ignored", filter, got, err)
		}
	}
	f.Server = "-"
	out, err = Analyze(f, nil, "UTC", walk)
	if err != nil || out.Totals.DNS != 1 || out.Totals.Firewall != 0 {
		t.Fatal("missing DNS server included firewall metadata", out, err)
	}
	f.Server = ""
	f.View = "domains"
	for _, key := range []string{"canonical", "deviceName", "dnsIP", "ipBasis", "server", "qtype", "match"} {
		f.Group = key
		out, err = Analyze(f, nil, "UTC", walk)
		if err != nil || len(out.Rows) == 0 {
			t.Fatal("new grouping unavailable", key, err)
		}
	}
}
