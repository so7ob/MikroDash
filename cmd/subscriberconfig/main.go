// Command subscriberconfig seals one NAS secret into a new private configuration.
// It never connects to a router or changes an existing configuration file.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
)

func seal(data, input, output, router, network string, secretInput io.Reader) error {
	if input == "" || output == "" || router == "" || network == "" {
		return errors.New("input, output, router and network are required")
	}
	// Open only an initialized store: do not create a new key or seed legacy routers.
	if _, err := os.Stat(filepath.Join(data, "routers.json")); err != nil {
		return errors.New("an initialized MikroDash data directory is required")
	}
	if os.Getenv("DATA_SECRET") == "" {
		key, err := os.ReadFile(filepath.Join(data, ".secret"))
		if err != nil || len(bytes.TrimSpace(key)) == 0 {
			return errors.New("existing store key unavailable")
		}
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		return errors.New("cannot read configuration template")
	}
	cfg, err := subscriber.DecodeConfig(raw)
	if err != nil {
		return errors.New("invalid configuration template")
	}
	index := -1
	for i, n := range cfg.Clients {
		if n.RouterID == router && n.Network == network {
			if index != -1 {
				return errors.New("ambiguous NAS selection")
			}
			index = i
		}
	}
	if index == -1 {
		return errors.New("selected NAS is not in the template")
	}
	secret, err := io.ReadAll(io.LimitReader(secretInput, 258))
	if err != nil {
		return errors.New("cannot read NAS secret")
	}
	secret = bytes.TrimSuffix(secret, []byte("\n"))
	secret = bytes.TrimSuffix(secret, []byte("\r"))
	if len(secret) < 16 || len(secret) > 253 || strings.ContainsAny(string(secret), "\x00\r\n") {
		return errors.New("NAS secret must contain 16..253 bytes on one line")
	}
	st, err := store.Open(data)
	if err != nil {
		return errors.New("cannot open existing store")
	}
	cfg.Clients[index].SecretSealed, err = st.Encrypt(string(secret))
	clear(secret)
	if err != nil {
		return errors.New("cannot seal NAS secret")
	}
	raw, err = json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return errors.New("cannot encode sealed configuration")
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("output must be a new writable file")
	}
	defer f.Close()
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if err != nil {
		return errors.New("cannot persist sealed configuration; do not use the output")
	}
	return nil
}

func main() {
	data := flag.String("data", "/data", "existing MikroDash data directory")
	input := flag.String("in", "", "configuration template path")
	output := flag.String("out", "", "new private output path; existing files are refused")
	router := flag.String("router", "", "exact router ID in the template")
	network := flag.String("network", "", "exact network key in the template")
	flag.Parse()
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "Read the secret from a protected file or pipe through standard input.")
		os.Exit(1)
	}
	if err := seal(*data, *input, *output, *router, *network, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "Sealed configuration created. No runtime or router settings changed.")
}
