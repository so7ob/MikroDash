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

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriberlab"
)

func main() {
	state := flag.String("state", "", "private owned-VM runtime.json")
	action := flag.String("action", "inspect", "inspect, configure, configure-https, prepare-runtime, repair-runtime-admin, install-landing, rollback-landing, runtime-status, logout-runtime, probe, probe-https, probe-download, probe-upload, probe-byte-renew, probe-disconnect, probe-runtime-traffic, probe-runtime-exhaust, probe-radius-selection or probe-radius-selection-fault")
	web := flag.String("web", "web/dist", "built frontend directory for the owned landing installer")
	flag.Parse()
	if err := run(*state, *action, *web); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(state, action, web string) error {
	if !filepath.IsAbs(state) {
		return fmt.Errorf("absolute private lab state path required")
	}
	switch action {
	case "install-landing", "rollback-landing", "inspect", "configure", "configure-https", "prepare-runtime", "repair-runtime-admin", "runtime-status", "logout-runtime", "probe", "probe-https", "probe-download", "probe-upload", "probe-byte-renew", "probe-disconnect", "probe-runtime-traffic", "probe-runtime-exhaust", "probe-radius-selection", "probe-radius-selection-fault":
	default:
		return fmt.Errorf("unknown lab action")
	}
	c, err := subscriberlab.Open(state)
	if err != nil {
		return err
	}
	defer c.Close()
	if action == "install-landing" || action == "rollback-landing" {
		return c.InstallLanding(filepath.Dir(state), web, action == "rollback-landing")
	}
	if action == "runtime-status" {
		rows, err := c.RuntimeStatus()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Synthetic  bool             `json:"synthetic"`
			ObservedAt time.Time        `json:"observedAt"`
			Active     []routeros.Reply `json:"active"`
		}{true, time.Now().UTC(), rows})
	}
	if action == "logout-runtime" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return c.LogoutRuntime(ctx, filepath.Dir(state))
	}
	if action == "probe-radius-selection" || action == "probe-radius-selection-fault" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return c.RadiusSelectionProbe(ctx, filepath.Dir(state), os.Stdout, action == "probe-radius-selection-fault")
	}
	if action == "repair-runtime-admin" {
		return c.RepairRuntimeAdmin(filepath.Dir(state))
	}
	if action == "probe-runtime-traffic" || action == "probe-runtime-exhaust" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if action == "probe-runtime-exhaust" {
			return c.RuntimeExhaustProbe(ctx, os.Stdout)
		}
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
