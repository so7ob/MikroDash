// Package activity interprets structured DNSUSER records without router I/O.
package activity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

const Retention = 30 * 24 * time.Hour

type Event struct {
	Country     string `json:"country"`
	City        string `json:"city"`
	Day         string `json:"day"`
	Hour        string `json:"hour"`
	ID          int64  `json:"id"`
	RouterID    string `json:"routerId"`
	SourceKey   string `json:"sourceKey"`
	At          int64  `json:"at"`
	Received    int64  `json:"received"`
	RouterTime  string `json:"routerTime"`
	TimeCertain bool   `json:"timeCertain"`
	Kind        string `json:"kind"`
	Domain      string `json:"domain"`
	User        string `json:"user"`
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Server      string `json:"server"`
	Match       string `json:"match"`
	QType       string `json:"qtype"`
	Client      string `json:"client"`
	Protocol    string `json:"protocol"`
	Destination string `json:"destination"`
	In          string `json:"in"`
	Out         string `json:"out"`
	Length      string `json:"length"`
	Service     string `json:"service"`
	Category    string `json:"category"`
	Count       int    `json:"count"`
}

type Rule struct {
	Domain     string `json:"domain"`
	Subdomains bool   `json:"subdomains"`
	Service    string `json:"service"`
	Category   string `json:"category"`
	Custom     bool   `json:"custom"`
}

type Settings struct {
	Folder   string `json:"folder"`
	Enabled  bool   `json:"enabled"`
	Timezone string `json:"timezone"`
}

type Status struct {
	LastSync      int64  `json:"lastSync"`
	Files         int    `json:"files"`
	BytesRead     int64  `json:"bytesRead"`
	Syncing       bool   `json:"syncing"`
	Enabled       bool   `json:"enabled"`
	Listening     bool   `json:"listening"`
	LastReceived  int64  `json:"lastReceived"`
	LastPersisted int64  `json:"lastPersisted"`
	Invalid       int64  `json:"invalid"`
	Dropped       int64  `json:"dropped"`
	Error         string `json:"error"`
}

type Coverage struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	if s == "-" {
		return ""
	}
	return s
}

// NormalizeDomain accepts DNS names, including IDNA ASCII labels and Unicode
// labels as supplied; no network lookup or domain ownership inference occurs.
func NormalizeDomain(s string) string {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if len(s) == 0 || len(s) > 253 || strings.ContainsAny(s, " /\\:@\n\r\t\x00") {
		return ""
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return ""
		}
	}
	return s
}

func ValidateRule(r Rule) (Rule, error) {
	r.Domain = NormalizeDomain(r.Domain)
	r.Service = strings.TrimSpace(r.Service)
	r.Category = strings.TrimSpace(r.Category)
	if r.Domain == "" || len(r.Service) == 0 || len(r.Service) > 80 || len(r.Category) == 0 || len(r.Category) > 80 || strings.ContainsAny(r.Service+r.Category, "\x00\r\n") {
		return r, errors.New("domain, service and category are required (80 characters maximum)")
	}
	r.Custom = true
	return r, nil
}

func Identity(e Event) string {
	var v []string
	if e.User != "" {
		v = []string{"user", e.Server, e.User}
	} else if e.MAC != "" {
		v = []string{"mac", e.MAC}
	} else {
		v = []string{"ip", e.IP}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// EventTime deliberately never calls process-observed TS the router's time.
// Date-less RouterOS stamps resolve relative to reception in the declared zone;
// unrecognised stamps use reception and remain explicitly uncertain.
func EventTime(raw string, received int64, zone string) (int64, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		return received, false
	}
	now := time.UnixMilli(received).In(loc)
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "Jan/02/2006 15:04:05", "jan/02/2006 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t.UnixMilli(), true
		}
	}
	for _, layout := range []string{"15:04:05.999999999", "15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			d := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
			if d.After(now.Add(time.Minute)) {
				d = d.AddDate(0, 0, -1)
			}
			return d.UnixMilli(), true
		}
	}
	for _, layout := range []string{"Jan/02 15:04:05", "jan/02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			d := time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
			if d.After(now.Add(time.Minute)) {
				d = d.AddDate(-1, 0, 0)
			}
			return d.UnixMilli(), true
		}
	}
	return received, false
}

var fwEndpoints = regexp.MustCompile(`(?:^|\s)(\S+)->(\S+),`)
var fwProto = regexp.MustCompile(`\bproto ([A-Za-z0-9-]+)`)
var fwMAC = regexp.MustCompile(`\bsrc-mac ([0-9A-Fa-f:]+)`)
var fwLength = regexp.MustCompile(`\blen ([0-9]+)`)
var fwIf = regexp.MustCompile(`\bin:(.*?) out:([^,]+),`)

func endpointIP(s string) string {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String()
	}
	h, _, err := net.SplitHostPort(s)
	if err != nil {
		return ""
	}
	a, err := netip.ParseAddr(h)
	if err != nil {
		return ""
	}
	return a.String()
}

// Parse returns relevant=false for ordinary router logs. Invalid DNSUSER rows
// are distinguishable from those ordinary logs and count as source failures.
func Parse(routerID, sourceID, stamp, topics, message string, received int64, zone string) (e Event, relevant bool, err error) {
	e = Event{RouterID: routerID, Received: received, RouterTime: stamp, Count: 1}
	e.At, e.TimeCertain = EventTime(stamp, received, zone)
	if strings.HasPrefix(strings.TrimSpace(message), "DNSUSER ") {
		relevant = true
		e.Kind = "dns"
		var p struct {
			Connection struct {
				Server string `json:"server"`
				QType  string `json:"qtype"`
				Match  string `json:"match"`
			} `json:"Connection"`
			Destination struct {
				Domain string `json:"domain"`
				IP     string `json:"ip"`
			} `json:"Destination"`
			Source struct {
				User string `json:"user"`
				IP   string `json:"ip"`
				MAC  string `json:"mac"`
			} `json:"Source"`
		}
		msg := strings.TrimSpace(message)
		if len(msg) > 16384 || json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(msg, "DNSUSER "))), &p) != nil {
			return e, true, errors.New("invalid DNSUSER JSON")
		}
		e.Domain = NormalizeDomain(p.Destination.Domain)
		if a, x := netip.ParseAddr(p.Destination.IP); x == nil {
			e.Destination = a.String()
		}
		e.User = clean(p.Source.User)
		e.Server = clean(p.Connection.Server)
		e.Match = clean(p.Connection.Match)
		e.QType = strings.ToUpper(clean(p.Connection.QType))
		if a, x := netip.ParseAddr(clean(p.Source.IP)); x == nil {
			e.IP = a.String()
		}
		if a, x := net.ParseMAC(clean(p.Source.MAC)); x == nil {
			e.MAC = strings.ToUpper(a.String())
		}
		if e.Domain == "" || (e.IP == "" && e.User == "" && e.MAC == "") || len(e.User) > 256 || len(e.Server) > 256 || len(e.Match) > 80 || len(e.QType) > 16 {
			return e, true, errors.New("invalid DNSUSER fields")
		}
	} else if strings.Contains(","+topics+",", ",firewall,") {
		ep := fwEndpoints.FindStringSubmatch(message)
		if len(ep) != 3 {
			return e, false, nil
		}
		e.Kind = "firewall"
		e.IP = endpointIP(ep[1])
		e.Destination = ep[2]
		if e.IP == "" || endpointIP(ep[2]) == "" {
			return e, false, nil
		}
		if m := fwProto.FindStringSubmatch(message); len(m) == 2 {
			e.Protocol = strings.ToLower(m[1])
		}
		if m := fwMAC.FindStringSubmatch(message); len(m) == 2 {
			if a, x := net.ParseMAC(m[1]); x == nil {
				e.MAC = strings.ToUpper(a.String())
			}
		}
		if m := fwLength.FindStringSubmatch(message); len(m) == 2 {
			e.Length = m[1]
		}
		if m := fwIf.FindStringSubmatch(message); len(m) == 3 {
			e.In = m[1]
			e.Out = m[2]
		}
		relevant = true
	} else {
		return e, false, nil
	}
	e.Client = Identity(e)
	// Router ids are scoped by the database. Distinct source ids preserve two
	// identical messages, while replay of the same source row retains its key.
	if sourceID != "" && e.TimeCertain {
		sum := sha256.Sum256([]byte(sourceID + "\x00" + time.UnixMilli(e.At).UTC().Format(time.RFC3339Nano) + "\x00" + topics + "\x00" + message))
		e.SourceKey = hex.EncodeToString(sum[:])
	}
	return e, relevant, nil
}

var Builtins = []Rule{
	{Domain: "chatgpt.com", Subdomains: true, Service: "ChatGPT", Category: "AI"},
	{Domain: "openai.com", Subdomains: true, Service: "OpenAI", Category: "AI"},
	{Domain: "claude.ai", Subdomains: true, Service: "Claude", Category: "AI"},
	{Domain: "facebook.com", Subdomains: true, Service: "Facebook", Category: "Social"},
	{Domain: "fbcdn.net", Subdomains: true, Service: "Facebook", Category: "Social"},
	{Domain: "instagram.com", Subdomains: true, Service: "Instagram", Category: "Social"},
	{Domain: "whatsapp.net", Subdomains: true, Service: "WhatsApp", Category: "Messaging"},
	{Domain: "whatsapp.com", Subdomains: true, Service: "WhatsApp", Category: "Messaging"},
	{Domain: "telegram.org", Subdomains: true, Service: "Telegram", Category: "Messaging"},
	{Domain: "youtube.com", Subdomains: true, Service: "YouTube", Category: "Video"},
	{Domain: "googlevideo.com", Subdomains: true, Service: "YouTube", Category: "Video"},
	{Domain: "youtubei.googleapis.com", Subdomains: true, Service: "YouTube", Category: "Video"},
	{Domain: "ytimg.com", Subdomains: true, Service: "YouTube", Category: "Video"},
	{Domain: "netflix.com", Subdomains: true, Service: "Netflix", Category: "Video"},
	{Domain: "tiktok.com", Subdomains: true, Service: "TikTok", Category: "Social"},
	{Domain: "capcutapi.com", Subdomains: true, Service: "CapCut", Category: "Content creation"},
	{Domain: "capcut.com", Subdomains: true, Service: "CapCut", Category: "Content creation"},
	{Domain: "steampowered.com", Subdomains: true, Service: "Steam", Category: "Gaming"},
	{Domain: "roblox.com", Subdomains: true, Service: "Roblox", Category: "Gaming"},
	{Domain: "coursera.org", Subdomains: true, Service: "Coursera", Category: "Education"},
	{Domain: "khanacademy.org", Subdomains: true, Service: "Khan Academy", Category: "Education"},
	{Domain: "doubleclick.net", Subdomains: true, Service: "DoubleClick", Category: "Advertising"},
	{Domain: "windowsupdate.com", Subdomains: true, Service: "Windows Update", Category: "Updates"},
}

func Classify(domain string, custom []Rule) (string, string) {
	for _, rules := range [][]Rule{custom, Builtins} {
		var best *Rule
		for i := range rules {
			r := &rules[i]
			if domain == r.Domain || (r.Subdomains && strings.HasSuffix(domain, "."+r.Domain)) {
				if best == nil || len(r.Domain) > len(best.Domain) || (r.Domain == best.Domain && !r.Subdomains) {
					best = r
				}
			}
		}
		if best != nil {
			return best.Service, best.Category
		}
	}
	return domain, "Unclassified"
}
