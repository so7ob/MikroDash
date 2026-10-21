// Command subscriberlanding renders a reviewable public HotSpot directory.
// It never connects to a NAS, reads a store key or enables network admission.
package main

import (
	"flag"
	"fmt"
	"mikrodash/internal/subscriber"
	"os"
	"path/filepath"
)

func render(config, network, web, output string) error {
	raw, err := os.ReadFile(config)
	if err != nil {
		return fmt.Errorf("cannot read subscriber configuration")
	}
	cfg, err := subscriber.DecodeConfig(raw)
	if err != nil {
		return fmt.Errorf("invalid subscriber configuration")
	}
	script, err := os.ReadFile(filepath.Join(web, "subscriber-handoff.js"))
	if err != nil {
		return fmt.Errorf("build the frontend before rendering the HotSpot handoff")
	}
	bundle, err := subscriber.RenderHotspotLanding(cfg, network, script)
	if err != nil {
		return err
	}
	return subscriber.WriteHotspotLanding(output, bundle)
}

func main() {
	config := flag.String("subscriber-config", "", "explicit subscriber runtime configuration")
	network := flag.String("network", "", "exact enrolled network key")
	web := flag.String("web", "web/dist", "built frontend directory")
	output := flag.String("out", "", "new private review directory; identical output may repeat")
	flag.Parse()
	if err := render(*config, *network, *web, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("HotSpot handoff rendered for review. No router settings or runtime authority changed.")
}
