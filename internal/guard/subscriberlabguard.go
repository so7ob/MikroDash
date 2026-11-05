package guard

import (
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
)

// SubscriberLabProof describes an owned, disposable QEMU appliance. It never
// grants the production subscriber service authority over another router.
type SubscriberLabProof struct {
	Identity, Name, Root, Disk, Console, StartTicks, ActualStartTicks string
	PID, APIPort, DisconnectPort                                      int
	Args, ActualArgs, BridgePorts                                     []string
	BridgeHasIPv4                                                     bool
	NativePortal                                                      bool
	BridgeIPv4                                                        []string
	NativeFirewallSHA, ActualFirewallSHA                              string
	Version, Board, Architecture                                      string
}

var labName = regexp.MustCompile(`^mikrodash-subscriber-chr-[0-9a-f]{8}$`)
var labRun = regexp.MustCompile(`^run-[0-9a-f]{8}$`)
var labFirewallDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SubscriberLabArgs is the entire permitted VM command, including two isolated
// NICs and management forwards bound only to loopback. No host physical NIC is
// attached, and no additional disk, forwarding rule or QEMU option is accepted.
func SubscriberLabArgs(p SubscriberLabProof) []string {
	return []string{
		filepath.Join(p.Root, "root/usr/bin/qemu-system-x86_64"),
		"-name", p.Name, "-machine", "pc,accel=tcg", "-cpu", "max",
		"-m", "768", "-smp", "1", "-rtc", "base=utc", "-display", "none", "-monitor", "none",
		"-serial", "unix:" + p.Console + ",server=on,wait=off",
		"-L", filepath.Join(p.Root, "root/usr/share/qemu"),
		"-bios", filepath.Join(p.Root, "root/usr/share/seabios/bios-256k.bin"),
		"-drive", "file=" + p.Disk + ",format=raw,if=ide",
		"-netdev", "user,id=wan,hostfwd=tcp:127.0.0.1:" + strconv.Itoa(p.APIPort) + "-:8728,hostfwd=udp:127.0.0.1:" + strconv.Itoa(p.DisconnectPort) + "-:3799",
		"-device", "e1000,netdev=wan,mac=02:00:00:00:54:11,romfile=",
		"-netdev", "tap,id=client,ifname=md-lab-tap,script=no,downscript=no",
		"-device", "e1000,netdev=client,mac=02:00:00:00:54:12,romfile=",
	}
}

// CheckSubscriberLab is pure. Its caller must collect current process, topology
// and RouterOS identity evidence again immediately before each lab write.
func CheckSubscriberLab(p SubscriberLabProof) bool {
	return CheckSubscriberLabProcess(p) && p.Version == "7.24.5 (stable)" && p.Board == "CHR QEMU Standard PC (i440FX + PIIX, 1996)" && p.Architecture == "x86_64"
}

// CheckSubscriberLabProcess is the local ownership check required before even
// opening the loopback API connection. It grants no configuration authority.
func CheckSubscriberLabProcess(p SubscriberLabProof) bool {
	if p.Identity != "test:subscriber-chr" || p.PID <= 1 || !labName.MatchString(p.Name) ||
		p.Root != "/dev/shm/mikrodash-subscriber-chr-lab" ||
		filepath.Dir(filepath.Dir(p.Disk)) != p.Root || !labRun.MatchString(filepath.Base(filepath.Dir(p.Disk))) || filepath.Base(p.Disk) != "disk.img" ||
		!filepath.IsAbs(p.Console) || filepath.Base(p.Console) != "console.sock" ||
		p.APIPort != 18728 || p.DisconnectPort != 13799 ||
		p.StartTicks == "" || p.StartTicks != p.ActualStartTicks {
		return false
	}
	if p.NativePortal {
		if !p.BridgeHasIPv4 || !slices.Equal(p.BridgeIPv4, []string{"198.51.100.10/24"}) || !labFirewallDigest.MatchString(p.NativeFirewallSHA) || p.NativeFirewallSHA != p.ActualFirewallSHA {
			return false
		}
	} else if p.BridgeHasIPv4 || p.NativeFirewallSHA != "" {
		return false
	}
	ports := slices.Clone(p.BridgePorts)
	slices.Sort(ports)
	return slices.Equal(ports, []string{"md-lab-host", "md-lab-tap"}) &&
		reflect.DeepEqual(p.Args, SubscriberLabArgs(p)) && reflect.DeepEqual(p.ActualArgs, p.Args)
}
