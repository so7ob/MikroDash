// Command subscriberchr inspects or bootstraps an owned disposable CHR. It cannot
// select an operator router or read MikroDash /data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mikrodash/internal/subscriberlab"
)

func main() {
	state := flag.String("state", "", "private owned-VM runtime.json")
	configure := flag.Bool("configure", false, "bootstrap only the owned disposable CHR")
	probe := flag.Bool("probe", false, "run isolated PAP/RADIUS timeout probes, not HTTPS portal acceptance")
	flag.Parse()
	if err := run(*state, *configure, *probe); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(state string, configure, probe bool) error {
	if !filepath.IsAbs(state) {
		return fmt.Errorf("absolute private lab state path required")
	}
	c, err := subscriberlab.Open(state)
	if err != nil {
		return err
	}
	defer c.Close()
	if probe {
		if configure {
			return fmt.Errorf("configure and probe are separate explicit operations")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		return c.ProtocolProbe(ctx, filepath.Dir(state), os.Stdout)
	}
	if configure {
		if err := c.Bootstrap(filepath.Dir(state)); err != nil {
			return err
		}
	}
	identity, err := c.Identity()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Synthetic  bool              `json:"synthetic"`
		Configured bool              `json:"configured"`
		Identity   map[string]string `json:"identity"`
	}{true, configure, identity})
}
