package subscriber

import (
	"fmt"
	"mikrodash/internal/routeros"
	"strings"
	"time"
)

type Reader interface {
	Do(routeros.Cmd) ([]routeros.Reply, error)
}
type Readiness struct {
	ObservedAt  time.Time                   `json:"observedAt"`
	Version     string                      `json:"version"`
	Model       string                      `json:"model"`
	Menus       map[string][]routeros.Reply `json:"menus"`
	Unavailable []string                    `json:"unavailable"`
	Review      []string                    `json:"review"`
}

// Readiness reads explicitly named non-secret properties. Empty/denied menus
// never establish readiness, and this routine cannot issue a write command.
func Inspect(reader Reader) Readiness {
	r := Readiness{ObservedAt: time.Now(), Menus: map[string][]routeros.Reply{}, Unavailable: []string{}, Review: []string{}}
	checks := []struct{ path, fields string }{
		{"/system/resource/print", "version,board-name,cpu-load,free-memory,total-memory,free-hdd-space"},
		{"/system/clock/print", "time-zone-name,gmt-offset"},
		{"/ip/hotspot/print", "name,interface,profile,disabled"},
		{"/ip/hotspot/profile/print", "name,dns-name,hotspot-address,login-by,ssl-certificate,use-radius,radius-accounting,radius-interim-update"},
		{"/radius/print", "address,authentication-port,accounting-port,service,disabled,src-address,protocol,require-message-auth"},
		{"/radius/incoming/print", "accept,port"},
		{"/ip/address/print", "address,interface,disabled"},
		{"/ip/firewall/filter/print", "chain,action,in-interface,out-interface,src-address,dst-address,dst-port,protocol,disabled"},
		{"/ipv6/settings/print", "disable-ipv6,forward"},
		{"/ipv6/firewall/filter/print", "chain,action,in-interface,out-interface,disabled"},
		{"/ip/hotspot/walled-garden/print", "server,dst-host,dst-port,action,disabled"},
		{"/ip/hotspot/walled-garden/ip/print", "server,dst-address,dst-port,action,disabled"},
		{"/queue/simple/print", "name,target,max-limit,dynamic,disabled"},
		{"/ip/hotspot/user/print", "name,server,disabled"},
	}
	for _, c := range checks {
		rows, err := reader.Do(routeros.Cmd{Path: c.path, Args: []string{"=.proplist=" + c.fields}, Timeout: 5 * time.Second})
		if err != nil {
			r.Unavailable = append(r.Unavailable, c.path)
			continue
		}
		if rows == nil {
			rows = []routeros.Reply{}
		}
		r.Menus[c.path] = rows
	}
	if rows := r.Menus["/system/resource/print"]; len(rows) > 0 {
		r.Version = rows[0]["version"]
		r.Model = rows[0]["board-name"]
	}
	for _, row := range r.Menus["/ip/firewall/filter/print"] {
		if row["action"] == "fasttrack-connection" && row["disabled"] != "true" {
			r.Review = append(r.Review, "Review active FastTrack rules against enrolled HotSpot traffic")
		}
	}
	r.Review = append(r.Review, "Verify both walled garden forwarding and router input access; do not add the subscriber bridge to LAN", "Verify IPv6 cannot bypass the enrolled HotSpot", "Review local HotSpot account precedence and ordered RADIUS server selection", "Prove local time and directional byte limits before enabling admission", "CoA timeout semantics and rate application remain unverified until hardware acceptance")
	return r
}

type ImportPreview struct {
	Rows        map[string][]routeros.Reply `json:"rows"`
	Warnings    []string                    `json:"warnings"`
	Fingerprint string                      `json:"fingerprint"`
}

func PreviewUserManager(reader Reader) ImportPreview {
	p := ImportPreview{Rows: map[string][]routeros.Reply{}, Warnings: []string{"Preview only. Historical totals are not current-period opening balances.", "Credentials are not read; migrated subscribers need a new credential or an approved credential handover."}}
	menus := []struct{ path, fields string }{
		{"/user-manager/profile/print", ".id,name,validity,starts-when,override-shared-users"},
		{"/user-manager/limitation/print", ".id,name,uptime-limit,download-limit,upload-limit,transfer-limit,reset-counters-interval,reset-counters-start-time,rate-limit-rx,rate-limit-tx,rate-limit-priority"},
		{"/user-manager/profile-limitation/print", ".id,profile,limitation,from-time,till-time,weekdays"},
		{"/user-manager/user/print", ".id,name,disabled,shared-users"},
		{"/user-manager/user-profile/print", ".id,user,profile,state,end-time"},
	}
	material := ""
	for _, m := range menus {
		rows, err := reader.Do(routeros.Cmd{Path: m.path, Args: []string{"=.proplist=" + m.fields}, Timeout: 5 * time.Second})
		if err != nil {
			p.Warnings = append(p.Warnings, "Unavailable menu: "+m.path)
			continue
		}
		if rows == nil {
			rows = []routeros.Reply{}
		}
		p.Rows[m.path] = rows
		for _, row := range rows {
			material += fmt.Sprint(row)
			if m.path == "/user-manager/limitation/print" {
				name := row["name"]
				if strings.Contains(strings.ToLower(name), "1min") && row["uptime-limit"] != "1m" {
					p.Warnings = append(p.Warnings, "Name does not match uptime value: "+name)
				}
				if strings.Contains(strings.ToLower(name), "daily") && row["reset-counters-interval"] != "daily" {
					p.Warnings = append(p.Warnings, "Name does not match reset interval: "+name)
				}
			}
		}
	}
	p.Fingerprint = Digest(material)
	return p
}
