// Package subscriberlab exercises a disposable, isolated RouterOS CHR. It never
// reads an operator store or enrolls an existing router.
package subscriberlab

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mikrodash/internal/guard"
	"mikrodash/internal/routeros"
)

// State is written privately by the lab launcher, never copied from /data.
type State struct {
	PID            int      `json:"pid"`
	Name           string   `json:"name"`
	Disk           string   `json:"disk"`
	Console        string   `json:"console"`
	Namespace      string   `json:"namespace"`
	Interfaces     []string `json:"interfaces"`
	APIPort        int      `json:"apiPort"`
	DisconnectPort int      `json:"disconnectPort"`
	Args           []string `json:"args"`
	StartTicks     string   `json:"startTicks"`
}

type credentials struct{ Username, Password string }

// Client retains one patched binary API connection. Every write recollects the
// process/topology proof and reads the exact appliance identity on that connection.
type Client struct {
	state  State
	client *routeros.Client
}

func privateJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 65536 {
		return errors.New("lab state and credentials must be private regular files")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return errors.New("lab directory must be private")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.New("cannot read private lab file")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("invalid lab file")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("lab file must contain one JSON value")
	}
	return nil
}

func (s State) proof() (guard.SubscriberLabProof, error) {
	p := guard.SubscriberLabProof{Identity: "test:subscriber-chr", PID: s.PID, Name: s.Name,
		Root: "/dev/shm/mikrodash-subscriber-chr-lab", Disk: s.Disk, Console: s.Console,
		APIPort: s.APIPort, DisconnectPort: s.DisconnectPort, Args: s.Args, StartTicks: s.StartTicks}
	if s.Namespace != "md-subscriber-lab" || strings.Join(s.Interfaces, ",") != "md-lab-br,md-lab-tap,md-lab-host" {
		return p, errors.New("unexpected lab network")
	}
	proc := filepath.Join("/proc", strconv.Itoa(s.PID))
	cmd, err := os.ReadFile(filepath.Join(proc, "cmdline"))
	if err != nil {
		return p, errors.New("owned lab process is unavailable")
	}
	p.ActualArgs = strings.Split(strings.TrimSuffix(string(cmd), "\x00"), "\x00")
	stat, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return p, errors.New("owned lab process identity unavailable")
	}
	fields := strings.Fields(string(stat)[strings.LastIndexByte(string(stat), ')')+1:])
	if len(fields) < 20 {
		return p, errors.New("invalid process identity")
	}
	p.ActualStartTicks = fields[19]
	ports, err := os.ReadDir("/sys/class/net/md-lab-br/brif")
	if err != nil {
		return p, errors.New("isolated lab bridge unavailable")
	}
	for _, port := range ports {
		p.BridgePorts = append(p.BridgePorts, port.Name())
	}
	bridge, err := net.InterfaceByName("md-lab-br")
	if err != nil {
		return p, errors.New("isolated lab bridge unavailable")
	}
	addresses, err := bridge.Addrs()
	if err != nil {
		return p, errors.New("lab bridge addresses unavailable")
	}
	for _, addr := range addresses {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || ip.To4() != nil {
			p.BridgeHasIPv4 = true
		}
	}
	if !guard.CheckSubscriberLabProcess(p) {
		return p, errors.New("disposable lab ownership proof refused")
	}
	return p, nil
}

func Open(path string) (*Client, error) {
	var state State
	if err := privateJSON(path, &state); err != nil {
		return nil, err
	}
	if _, err := state.proof(); err != nil {
		return nil, err
	}
	var auth credentials
	if err := privateJSON(filepath.Join(filepath.Dir(path), "credentials.json"), &auth); err != nil {
		return nil, err
	}
	if auth.Username != "admin" || len(auth.Password) < 24 {
		return nil, errors.New("separate synthetic lab credentials required")
	}
	c, err := routeros.Dial(routeros.Config{Host: "127.0.0.1", Port: state.APIPort,
		Username: auth.Username, Password: auth.Password, DialTimeout: 5 * time.Second})
	if err != nil {
		return nil, errors.New("owned CHR API unavailable")
	}
	lab := &Client{state: state, client: c}
	if _, err = lab.Identity(); err != nil {
		c.Close()
		return nil, err
	}
	return lab, nil
}

func (c *Client) Close() error { return c.client.Close() }

func (c *Client) Identity() (routeros.Reply, error) {
	p, err := c.state.proof()
	if err != nil {
		return nil, err
	}
	rows, err := c.client.Do(routeros.Cmd{Path: "/system/resource/print",
		Args: []string{"=.proplist=version,board-name,architecture-name,cpu-load,total-memory,free-memory,uptime"}, Timeout: 5 * time.Second})
	if err != nil || len(rows) != 1 {
		return nil, errors.New("CHR identity read failed")
	}
	p.Version, p.Board, p.Architecture = rows[0]["version"], rows[0]["board-name"], rows[0]["architecture-name"]
	if !guard.CheckSubscriberLab(p) {
		return nil, errors.New("pinned disposable CHR identity refused")
	}
	return rows[0], nil
}

// Do accepts only an explicit bounded lab command. RouterOS output stays private;
// the public report selects fields, rather than exporting configuration wholesale.
func (c *Client) Do(cmd routeros.Cmd) ([]routeros.Reply, error) {
	if cmd.Timeout <= 0 || cmd.Timeout > 10*time.Second {
		return nil, errors.New("bounded lab command required")
	}
	if _, err := c.Identity(); err != nil {
		return nil, err
	}
	return c.client.Do(cmd)
}
