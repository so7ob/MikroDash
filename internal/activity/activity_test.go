package activity

import (
	"strings"
	"testing"
	"time"
)

const synthetic = `DNSUSER {"Connection":{"server":"hs-test","qtype":"A","match":"active-now"},"Destination":{"domain":"AB.ChatGPT.com."},"Source":{"user":"مستخدم تجريبي","ip":"198.51.100.25","mac":"02:00:00:00:00:25"}}`

func TestStructuredRecordsAndEventIdentity(t *testing.T) {
	now := time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC).UnixMilli()
	a, yes, err := Parse("r1", "*1", "2026-10-16 21:48:14", "script,info", synthetic, now, "Asia/Aden")
	if err != nil || !yes {
		t.Fatal(err)
	}
	if a.Domain != "ab.chatgpt.com" || a.User != "مستخدم تجريبي" || a.Server != "hs-test" || !a.TimeCertain || a.At != time.Date(2026, 10, 16, 18, 48, 14, 0, time.UTC).UnixMilli() {
		t.Fatalf("parsed fields incorrect: %#v", a)
	}
	b, _, _ := Parse("r1", "*2", a.RouterTime, "script,info", synthetic, now, "Asia/Aden")
	replay, _, _ := Parse("r1", "*1", a.RouterTime, "script,info", synthetic, now+1000, "Asia/Aden")
	if a.SourceKey == b.SourceKey || a.SourceKey != replay.SourceKey {
		t.Fatal("source identity collapsed independent repeats or changed on replay")
	}
	unknown := strings.ReplaceAll(synthetic, `"مستخدم تجريبي"`, `"-"`)
	unknown = strings.ReplaceAll(unknown, `"02:00:00:00:00:25"`, `"-"`)
	unknown = strings.ReplaceAll(unknown, `"198.51.100.25"`, `"2001:db8::25"`)
	e, _, err := Parse("r1", "*3", a.RouterTime, "script,info", unknown, now, "Asia/Aden")
	if err != nil || e.User != "" || e.IP != "2001:db8::25" || !strings.Contains(e.Client, "ip") {
		t.Fatalf("unmapped IPv6: %#v %v", e, err)
	}
	if _, yes, err := Parse("r1", "*4", a.RouterTime, "script,info", "DNSUSER null", now, "Asia/Aden"); !yes || err == nil {
		t.Fatal("invalid DNSUSER accepted")
	}
	if _, yes, _ := Parse("r1", "*4", a.RouterTime, "system,info", "normal event", now, "Asia/Aden"); yes {
		t.Fatal("ordinary event counted")
	}
}
func TestFirewallIsMetadataWithoutInferredDomain(t *testing.T) {
	e, yes, err := Parse("r1", "*2", "bad time", "firewall,info", "srcnat: in:bridge out:ether1, connection-state:new src-mac 02:00:00:00:00:25, proto TCP (SYN), 198.51.100.25:1234->203.0.113.4:443, len 60", 1000, "UTC")
	if !yes || err != nil || e.IP != "198.51.100.25" || e.Domain != "" || e.User != "" || e.Length != "60" || e.Protocol != "tcp" || e.In != "bridge" || e.TimeCertain {
		t.Fatalf("wrong firewall semantics: %#v %v", e, err)
	}
}
func TestClassificationPrecedenceAndBoundaries(t *testing.T) {
	s, c := Classify("ab.chatgpt.com", nil)
	if s != "ChatGPT" || c != "AI" {
		t.Fatal(s, c)
	}
	if _, c = Classify("evilchatgpt.com", nil); c != "Unclassified" {
		t.Fatal("suffix crossed label boundary")
	}
	rules := []Rule{{Domain: "com", Subdomains: true, Service: "Custom", Category: "Local"}, {Domain: "chatgpt.com", Subdomains: true, Service: "Specific", Category: "Special"}}
	if s, c = Classify("ab.chatgpt.com", rules); s != "Specific" || c != "Special" {
		t.Fatal("specific rule lost")
	}
	if s, _ = Classify("facebook.com", rules); s != "Custom" {
		t.Fatal("built-in took precedence over override")
	}
	rules[1].Subdomains = false
	if s, _ = Classify("ab.chatgpt.com", rules); s != "Custom" {
		t.Fatal("exact match matched a child")
	}
	if _, err := ValidateRule(Rule{Domain: "x.com", Service: "=formula", Category: "Test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRule(Rule{Domain: "bad/host", Service: "x", Category: "x"}); err == nil {
		t.Fatal("bad domain accepted")
	}
}
func TestArchiveLineFormsAndMidnight(t *testing.T) {
	for _, line := range []string{"2026-10-16 21:48:14 script,info " + synthetic, "2026-10-16 21:48:14 [script,info] " + synthetic, "oct/16/2026 21:48:14 script,info " + synthetic} {
		stamp, topics, msg, ok := SplitArchiveLine(line)
		if !ok || stamp == "" || topics != "script,info" || msg != synthetic {
			t.Fatalf("line form not parsed: %q", line)
		}
	}
	now := time.Date(2026, 10, 17, 0, 0, 5, 0, time.UTC).UnixMilli()
	at, certain := EventTime("23:59:59", now, "UTC")
	if !certain || at != now-6000 {
		t.Fatal("midnight inferred into the future")
	}
	if !ValidFolder(DefaultFolder) || ValidFolder("../etc") || ValidFolder("usb1/../private") {
		t.Fatal("folder validation")
	}
}
func TestAnalyzeCountsFilteringAndHistoricalIdentity(t *testing.T) {
	from := int64(20000000)
	events := []Event{{ID: 1, At: from + 1000, Kind: "dns", Domain: "chatgpt.com", User: "test-user", Server: "s", IP: "198.51.100.1", Client: `["user","s","test-user"]`}, {ID: 2, At: from + 1000, Kind: "dns", Domain: "chatgpt.com", User: "test-user", Server: "s", IP: "198.51.100.1", Client: `["user","s","test-user"]`}, {ID: 3, At: from + 2000, Kind: "dns", Domain: "unknown.test", IP: "198.51.100.1", Client: `["ip","198.51.100.1"]`}}
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
	f := Filter{From: from, To: from + 100000, View: "clients", Limit: 50, Page: 1, Desc: true}
	out, err := Analyze(f, nil, "UTC", walk)
	if err != nil {
		t.Fatal(err)
	}
	if out.Totals.DNS != 3 || out.Totals.Users != 1 || out.Totals.Unmapped != 1 || len(out.Rows) != 2 || out.Rows[0].Count != 2 {
		t.Fatalf("counts incorrect: %#v", out)
	}
	f.Service = "ChatGPT"
	out, _ = Analyze(f, nil, "UTC", walk)
	if out.Totals.DNS != 2 || out.Totals.Unmapped != 0 {
		t.Fatal("filter failed")
	}
	if FullyCovered([]Coverage{{Start: from, End: from + 1000}, {Start: from + 10000, End: from + 20000}}, from, from+20000) {
		t.Fatal("gap counted as coverage")
	}
}

func TestGeographyUsesOnlyExplicitDestinationEvidenceAndTimeGroups(t *testing.T) {
	from := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC).UnixMilli()
	events := []Event{{ID: 1, At: from + 1000, Kind: "dns", Domain: "chatgpt.com", Client: "a", IP: "198.51.100.1"}, {ID: 2, At: from + 2000, Kind: "firewall", Destination: "203.0.113.4:443", Client: "b", IP: "198.51.100.1"}, {ID: 3, At: from + 3600000, Kind: "firewall", Destination: "[2001:db8::4]:443", Client: "b", IP: "198.51.100.1"}}
	lookup := func(ip string) (string, string) {
		if ip == "203.0.113.4" {
			return "YE", "Synthetic city"
		}
		return "", ""
	}
	walk := func(a, b int64, fn func(Event) error) error {
		for _, e := range events {
			if err := fn(e); err != nil {
				return err
			}
		}
		return nil
	}
	f := Filter{From: from, To: from + 86400000, View: "domains", Group: "country", Page: 1, Limit: 50, Desc: true}
	out, err := Analyze(f, nil, "UTC", walk, lookup)
	if err != nil || len(out.Rows) != 2 || out.Rows[1].Key != "YE" {
		t.Fatal("country groups", out.Rows, err)
	}
	e := events[0]
	Enrich(&e, "UTC", lookup)
	if e.Country != "Unknown" {
		t.Fatal("DNS-only domain acquired an invented country")
	}
	f.Group = "day"
	out, _ = Analyze(f, nil, "UTC", walk, lookup)
	if len(out.Rows) != 1 || out.Rows[0].Count != 3 {
		t.Fatal("day grouping")
	}
	f.Group = "hour"
	out, _ = Analyze(f, nil, "UTC", walk, lookup)
	if len(out.Rows) != 2 || out.Rows[0].Count != 2 {
		t.Fatal("hour grouping")
	}
	f.Hour = out.Rows[1].Key
	out, _ = Analyze(f, nil, "UTC", walk, lookup)
	if out.Totals.Firewall != 1 || out.Totals.DNS != 0 {
		t.Fatal("hour drilldown")
	}
}
