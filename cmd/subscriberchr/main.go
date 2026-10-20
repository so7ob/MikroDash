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
	action := flag.String("action", "inspect", "inspect, configure, configure-https, prepare-runtime, repair-runtime-admin, probe, probe-https, probe-download, probe-upload, probe-byte-renew, probe-disconnect or probe-runtime-traffic")
	flag.Parse()
	if err := run(*state, *action); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(state, action string) error {
	if !filepath.IsAbs(state) {
		return fmt.Errorf("absolute private lab state path required")
	}
	switch action {
	case "inspect", "configure", "configure-https", "prepare-runtime", "repair-runtime-admin", "probe", "probe-https", "probe-download", "probe-upload", "probe-byte-renew", "probe-disconnect", "probe-runtime-traffic":
	default:
		return fmt.Errorf("unknown lab action")
	}
	c, err := subscriberlab.Open(state)
	if err != nil {
		return err
	}
	defer c.Close()
	if action == "repair-runtime-admin" {
		return c.RepairRuntimeAdmin(filepath.Dir(state))
	}
	if action == "probe-runtime-traffic" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return c.RuntimeTrafficProbe(ctx, os.Stdout)
	}
	if action == "prepare-runtime" {
		return c.PrepareRuntime(filepath.Dir(state))
	}
	if action == "configure-https" {
		return c.BootstrapHTTPS(filepath.Dir(state))
	}
	if action == "probe" || action == "probe-https" || action == "probe-download" || action == "probe-upload" || action == "probe-byte-renew" || action == "probe-disconnect" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		scenario := "timeout"
		if action == "probe-download" {
			scenario = "download-limit"
		} else if action == "probe-upload" {
			scenario = "upload-limit"
		} else if action == "probe-byte-renew" {
			scenario = "byte-renew"
		} else if action == "probe-disconnect" {
			scenario = "disconnect"
		}
		return c.ProtocolProbe(ctx, filepath.Dir(state), os.Stdout, action != "probe", scenario)
	}
	if action == "configure" {
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
	}{true, action == "configure", identity})
}
