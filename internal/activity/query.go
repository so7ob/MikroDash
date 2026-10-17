package activity

import (
	"slices"
	"sort"
	"strings"
	"time"
)

type Filter struct {
	DNSCountry string `json:"dnsCountry"`
	Canonical  string `json:"canonical"`
	DeviceName string `json:"deviceName"`
	DNSIP      string `json:"dnsIP"`
	IPBasis    string `json:"ipBasis"`
	Server     string `json:"server"`
	QType      string `json:"qtype"`
	Match      string `json:"match"`
	Country    string `json:"country"`
	Day        string `json:"day"`
	Hour       string `json:"hour"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	Search     string `json:"search"`
	Client     string `json:"client"`
	Domain     string `json:"domain"`
	Service    string `json:"service"`
	Category   string `json:"category"`
	Kind       string `json:"kind"`
	View       string `json:"view"`
	Group      string `json:"group"`
	Sort       string `json:"sort"`
	Desc       bool   `json:"desc"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
}

type Totals struct {
	Answered    int `json:"answered"`
	AnswerIPs   int `json:"answerIPs"`
	DeviceNames int `json:"deviceNames"`
	Canonicals  int `json:"canonicals"`
	DNS         int `json:"dns"`
	Firewall    int `json:"firewall"`
	Domains     int `json:"domains"`
	Users       int `json:"users"`
	Unmapped    int `json:"unmapped"`
	Sources     int `json:"sources"`
}

type Ranking struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Service  string `json:"service"`
	Category string `json:"category"`
	User     string `json:"user"`
	Server   string `json:"server"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Count    int    `json:"count"`
	Domains  int    `json:"domains"`
	Devices  int    `json:"devices"`
	Last     int64  `json:"last"`
}
type Point struct {
	At    int64 `json:"at"`
	Count int   `json:"count"`
}
type Heat struct {
	Day   int `json:"day"`
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

// Result is the generated REST response. The invalidation event carries only
// RouterID, so large query results never fan out.
type Result struct {
	DNSCountries []Ranking  `json:"dnsCountries"`
	QueryTypes   []Ranking  `json:"queryTypes"`
	MatchMethods []Ranking  `json:"matchMethods"`
	IPBases      []Ranking  `json:"ipBases"`
	Countries    []Ranking  `json:"countries"`
	GeoAvailable bool       `json:"geoAvailable"`
	RouterID     string     `json:"routerId"`
	Filter       Filter     `json:"filter"`
	Totals       Totals     `json:"totals"`
	Rows         []Ranking  `json:"rows"`
	Events       []Event    `json:"events"`
	Services     []Ranking  `json:"services"`
	Clients      []Ranking  `json:"clients"`
	Categories   []Ranking  `json:"categories"`
	Series       []Point    `json:"series"`
	Heatmap      []Heat     `json:"heatmap"`
	Coverage     []Coverage `json:"coverage"`
	Rules        []Rule     `json:"rules"`
	TotalRows    int        `json:"totalRows"`
	Previous     int        `json:"previous"`
	Comparison   bool       `json:"comparison"`
	CanWrite     bool       `json:"canWrite"`
	Status       Status     `json:"status"`
	Settings     Settings   `json:"settings"`
}

func Matches(e Event, f Filter) bool {
	for _, value := range []struct{ wanted, actual string }{
		{f.Canonical, e.Canonical}, {f.DeviceName, e.DeviceName},
		{f.IPBasis, e.IPBasis}, {f.Server, e.Server}, {f.QType, e.QType}, {f.Match, e.Match},
	} {
		if value.wanted != "" && (e.Kind != "dns" || clean(value.wanted) != value.actual) {
			return false
		}
	}
	if f.DNSIP != "" && (e.Kind != "dns" || !slices.Contains(e.DNSIPs, f.DNSIP)) {
		return false
	}

	if f.DNSCountry != "" && (e.Kind != "dns" || !slices.ContainsFunc(e.DNSAnswers, func(a DNSAnswer) bool { return a.Country == f.DNSCountry })) {
		return false
	}
	if f.Country != "" && (e.Kind != "firewall" || f.Country != e.Country) {
		return false
	}
	if f.Day != "" && f.Day != e.Day {
		return false
	}
	if f.Hour != "" && f.Hour != e.Hour {
		return false
	}
	if f.Kind != "" && f.Kind != e.Kind {
		return false
	}
	if f.Client != "" && f.Client != e.Client {
		return false
	}
	if f.Domain != "" && f.Domain != e.Domain {
		return false
	}
	if f.Service != "" && f.Service != e.Service {
		return false
	}
	if f.Category != "" && f.Category != e.Category {
		return false
	}
	if f.Search != "" {
		hay := strings.ToLower(strings.Join([]string{e.Domain, e.Service, e.Category, e.User, e.IP, e.MAC, e.Server, e.Destination, e.Protocol, e.Canonical, e.DeviceName, e.IPBasis, e.QType, e.Match, strings.Join(e.DNSIPs, " ")}, " "))
		if !strings.Contains(hay, strings.ToLower(f.Search)) {
			return false
		}
	}
	return true
}

type group struct {
	row              Ranking
	domains, devices map[string]bool
}

func addGroup(m map[string]*group, key string, e Event) {
	g := m[key]
	if g == nil {
		g = &group{row: Ranking{Key: key, Label: key, Category: e.Category, Service: e.Service, User: e.User, Server: e.Server, IP: e.IP, MAC: e.MAC}, domains: map[string]bool{}, devices: map[string]bool{}}
		m[key] = g
	}
	if g.row.Category != e.Category {
		g.row.Category = "Mixed"
	}
	if g.row.Service != e.Service {
		g.row.Service = ""
	}
	g.row.Count++
	if e.At > g.row.Last {
		g.row.Last = e.At
	}
	if e.Domain != "" {
		g.domains[e.Domain] = true
	}
	device := e.MAC
	if device == "" {
		device = e.IP
	}
	if device != "" {
		g.devices[device] = true
	}
}
func rankings(m map[string]*group) []Ranking {
	out := make([]Ranking, 0, len(m))
	for _, g := range m {
		r := g.row
		r.Domains = len(g.domains)
		r.Devices = len(g.devices)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}
func top(rows []Ranking, n int) []Ranking {
	if len(rows) > n {
		return rows[:n]
	}
	return rows
}
func SortRankings(rows []Ranking, f Filter) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		cmp := 0
		switch f.Sort {
		case "label":
			cmp = strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
		case "category":
			cmp = strings.Compare(a.Category, b.Category)
		case "domains":
			cmp = a.Domains - b.Domains
		case "devices":
			cmp = a.Devices - b.Devices
		case "last":
			if a.Last < b.Last {
				cmp = -1
			} else if a.Last > b.Last {
				cmp = 1
			}
		default:
			cmp = a.Count - b.Count
		}
		if cmp == 0 {
			return a.Key < b.Key
		}
		if f.Desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// Analyze streams rows, retaining only aggregate indexes and the current events
// page. No raw history slice grows with the number of stored events.
func Analyze(f Filter, rules []Rule, zone string, walk func(int64, int64, func(Event) error) error, lookups ...func(string) (string, string)) (Result, error) {
	out := Result{DNSCountries: []Ranking{}, QueryTypes: []Ranking{}, MatchMethods: []Ranking{}, IPBases: []Ranking{}, Countries: []Ranking{}, Filter: f, Rows: []Ranking{}, Events: []Event{}, Services: []Ranking{}, Clients: []Ranking{}, Categories: []Ranking{}, Series: []Point{}, Heatmap: []Heat{}, Coverage: []Coverage{}, Rules: []Rule{}}
	domains := map[string]*group{}
	services := map[string]*group{}
	clients := map[string]*group{}
	categories := map[string]*group{}
	countries := map[string]*group{}
	dnsCountries := map[string]*group{}
	dnsGroups := map[string]map[string]*group{}
	for _, key := range []string{"canonical", "deviceName", "dnsIP", "ipBasis", "server", "qtype", "match"} {
		dnsGroups[key] = map[string]*group{}
	}
	dates := map[string]*group{}
	hours := map[string]*group{}
	var lookup func(string) (string, string)
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	out.GeoAvailable = lookup != nil
	known := map[string]bool{}
	unmapped := map[string]bool{}
	points := map[int64]int{}
	heats := map[int]int{}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	bucket := int64(time.Hour / time.Millisecond)
	if f.To-f.From > 7*24*bucket {
		bucket *= 6
	}
	previousFrom := f.From - (f.To - f.From)
	err = walk(previousFrom, f.To, func(e Event) error {
		if e.Kind == "dns" {
			e.Service, e.Category = Classify(e.Domain, rules)
		}
		Enrich(&e, zone, lookup)
		if !Matches(e, f) {
			return nil
		}
		if e.At < f.From {
			if e.Kind == "dns" {
				out.Previous++
			}
			return nil
		}
		addGroup(dates, e.Day, e)
		addGroup(hours, e.Hour, e)
		if e.Kind == "firewall" {
			addGroup(countries, e.Country, e)
		}
		if e.Kind == "dns" {
			out.Totals.DNS++
			for _, answer := range e.DNSAnswers {
				addGroup(dnsCountries, answer.Country, e)
				addGroup(dnsGroups["dnsIP"], answer.IP, e)
			}
			for key, value := range map[string]string{"canonical": e.Canonical, "deviceName": e.DeviceName, "ipBasis": e.IPBasis, "server": e.Server, "qtype": e.QType, "match": e.Match} {
				if value == "" {
					if key == "canonical" || key == "deviceName" {
						continue
					}
					value = "-"
				}
				addGroup(dnsGroups[key], value, e)
			}
			if len(e.DNSAnswers) > 0 {
				out.Totals.Answered++
			}
			addGroup(domains, e.Domain, e)
			addGroup(services, e.Service, e)
			addGroup(clients, e.Client, e)
			addGroup(categories, e.Category, e)
			if e.User != "" {
				known[e.Client] = true
			} else {
				unmapped[e.Client] = true
			}
			points[e.At/bucket*bucket]++
			d := time.UnixMilli(e.At).In(loc)
			heats[int(d.Weekday())*24+d.Hour()]++
		} else {
			out.Totals.Firewall++
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	out.Totals.AnswerIPs = len(dnsGroups["dnsIP"])
	out.Totals.DeviceNames = len(dnsGroups["deviceName"])
	out.Totals.Canonicals = len(dnsGroups["canonical"])
	for _, key := range []string{"ipBasis", "server", "qtype", "match"} {
		if missing := dnsGroups[key]["-"]; missing != nil {
			missing.row.Label = "Not provided"
		}
	}
	out.QueryTypes = rankings(dnsGroups["qtype"])
	out.MatchMethods = rankings(dnsGroups["match"])
	out.IPBases = rankings(dnsGroups["ipBasis"])
	out.Totals.Domains = len(domains)
	out.Totals.Users = len(known)
	out.Totals.Unmapped = len(unmapped)
	out.Totals.Sources = len(clients)
	for _, g := range clients {
		if g.row.User != "" {
			g.row.Label = g.row.User
		} else if g.row.MAC != "" {
			g.row.Label = g.row.MAC
		} else {
			g.row.Label = g.row.IP
		}
		g.row.Service = ""
		g.row.Category = ""
	}
	out.Services = top(rankings(services), 10)
	out.Clients = top(rankings(clients), 10)
	out.Categories = rankings(categories)
	out.Countries = rankings(countries)
	out.DNSCountries = rankings(dnsCountries)
	for at := f.From / bucket * bucket; at < f.To; at += bucket {
		out.Series = append(out.Series, Point{At: at, Count: points[at]})
	}
	for day := 0; day < 7; day++ {
		for hour := 0; hour < 24; hour++ {
			out.Heatmap = append(out.Heatmap, Heat{Day: day, Hour: hour, Count: heats[day*24+hour]})
		}
	}
	var rows []Ranking
	switch f.View {
	case "summary":
		if f.Group == "domain" {
			rows = rankings(domains)
		} else {
			rows = rankings(dnsGroups[f.Group])
		}
	case "clients":
		rows = rankings(clients)
	case "domains":
		switch f.Group {
		case "domain":
			rows = rankings(domains)
		case "country":
			rows = rankings(countries)
		case "dnsCountry":
			rows = rankings(dnsCountries)
		case "category":
			rows = rankings(categories)
		case "client":
			rows = rankings(clients)
		case "day":
			rows = rankings(dates)
		case "hour":
			rows = rankings(hours)
		default:
			if group, ok := dnsGroups[f.Group]; ok {
				rows = rankings(group)
			} else {
				rows = rankings(services)
			}
		}
	default:
		rows = []Ranking{}
	}
	if rows != nil {
		SortRankings(rows, f)
		out.TotalRows = len(rows)
		start := (f.Page - 1) * f.Limit
		if start < len(rows) {
			end := start + f.Limit
			if end > len(rows) {
				end = len(rows)
			}
			out.Rows = rows[start:end]
		}
	}
	return out, nil
}

func FullyCovered(runs []Coverage, from, to int64) bool {
	cursor := from
	for _, c := range runs {
		if c.End <= cursor {
			continue
		}
		if c.Start > cursor+2000 {
			return false
		}
		if c.End > cursor {
			cursor = c.End
		}
		if cursor >= to-2000 {
			return true
		}
	}
	return false
}
