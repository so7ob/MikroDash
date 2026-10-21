// Command subscribercheck inspects readiness without writing to RouterOS or SQLite.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"mikrodash/internal/routeros"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
	"net"
	"os"
	"strconv"
)

func main() {
	data := flag.String("data", "/data", "existing MikroDash data directory")
	id := flag.String("router", "", "router ID or stored label")
	preview := flag.Bool("user-manager", false, "preview source definitions without importing")
	domain := flag.String("login-domain", "", "review optional HotSpot domain routing without applying it")
	hotspot := flag.String("hotspot", "", "HotSpot server name for domain routing review")
	username := flag.String("subscriber", "", "original subscriber username for local precedence review")
	endpoint := flag.String("radius-endpoint", "", "explicit router-visible RADIUS IPv4/IPv6 address:authentication-port")
	accountingPort := flag.Int("radius-accounting-port", 0, "explicit router-visible RADIUS accounting port")
	flag.Parse()
	var target subscriber.LoginRoutingTarget
	if *domain != "" {
		host, port, splitErr := net.SplitHostPort(*endpoint)
		authPort, portErr := strconv.Atoi(port)
		if *preview || *hotspot == "" || *username == "" || splitErr != nil || portErr != nil || net.ParseIP(host) == nil || authPort < 1 || authPort > 65535 || *accountingPort < 1 || *accountingPort > 65535 {
			fmt.Fprintln(os.Stderr, "domain review requires explicit HotSpot, subscriber and router-visible RADIUS address/ports; it cannot be combined with import preview")
			os.Exit(1)
		}
		target = subscriber.LoginRoutingTarget{Address: host, AuthPort: authPort, AccountingPort: *accountingPort}
	}
	st, err := store.Open(*data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read store")
		os.Exit(1)
	}
	routers, errs := st.Routers()
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "router credentials unavailable")
		os.Exit(1)
	}
	for _, r := range routers {
		if r.ID != *id && r.Label != *id {
			continue
		}
		c, err := routeros.Dial(routeros.Config{Host: r.Host, Port: r.Port, Username: r.Username, Password: r.Password, TLS: r.TLS, InsecureTLS: r.TLSInsecure})
		if err != nil {
			fmt.Fprintln(os.Stderr, "router connection unavailable")
			os.Exit(1)
		}
		defer c.Close()
		readiness := subscriber.Inspect(c)
		var report any = readiness
		if *preview {
			report = subscriber.PreviewUserManager(c)
		} else if *domain != "" {
			legacy := subscriber.PreviewUserManager(c)
			rows, complete := legacy.Rows["/user-manager/user/print"]
			names := []string{}
			for _, row := range rows {
				if row["name"] == "" {
					complete = false
				}
				names = append(names, row["name"])
			}
			review := subscriber.ReviewLoginRouting(readiness, subscriber.NAS{LoginDomain: *domain, CalledStationID: *hotspot}, target, *username, names, complete)
			report = struct {
				Readiness subscriber.Readiness          `json:"readiness"`
				Routing   subscriber.LoginRoutingReview `json:"routing"`
			}{readiness, review}
		}
		if err = json.NewEncoder(os.Stdout).Encode(report); err != nil {
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "router not found")
	os.Exit(1)
}
