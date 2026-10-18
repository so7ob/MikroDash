// Command subscribercheck inspects readiness without writing to RouterOS or SQLite.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"mikrodash/internal/routeros"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
	"os"
)

func main() {
	data := flag.String("data", "/data", "existing MikroDash data directory")
	id := flag.String("router", "", "router ID or stored label")
	preview := flag.Bool("user-manager", false, "preview source definitions without importing")
	flag.Parse()
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
		var report any = subscriber.Inspect(c)
		if *preview {
			report = subscriber.PreviewUserManager(c)
		}
		if err = json.NewEncoder(os.Stdout).Encode(report); err != nil {
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "router not found")
	os.Exit(1)
}
