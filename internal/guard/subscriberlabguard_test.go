package guard

import (
	"slices"
	"testing"
)

func labProof() SubscriberLabProof {
	p := SubscriberLabProof{Identity: "test:subscriber-chr", PID: 123, Name: "mikrodash-subscriber-chr-0123abcd",
		Root: "/dev/shm/mikrodash-subscriber-chr-lab", Disk: "/dev/shm/mikrodash-subscriber-chr-lab/run-0123abcd/disk.img",
		Console: "/private/synthetic/console.sock", StartTicks: "12345", ActualStartTicks: "12345",
		APIPort: 18728, DisconnectPort: 13799, BridgePorts: []string{"md-lab-tap", "md-lab-host"},
		Version: "7.24.5 (stable)", Board: "CHR QEMU Standard PC (i440FX + PIIX, 1996)", Architecture: "x86_64"}
	p.Args = SubscriberLabArgs(p)
	p.ActualArgs = slices.Clone(p.Args)
	return p
}

func TestDisposableSubscriberLabRequiresEveryProof(t *testing.T) {
	if !CheckSubscriberLab(labProof()) {
		t.Fatal("owned synthetic appliance refused")
	}
	cases := map[string]func(*SubscriberLabProof){
		"service authority": func(p *SubscriberLabProof) { p.Identity = "service:subscriber" },
		"physical router":   func(p *SubscriberLabProof) { p.Board = "L009UiGS-2HaxD" },
		"other version":     func(p *SubscriberLabProof) { p.Version = "7.26 (stable)" },
		"other arch":        func(p *SubscriberLabProof) { p.Architecture = "arm" },
		"reused PID":        func(p *SubscriberLabProof) { p.ActualStartTicks = "99999" },
		"no start proof":    func(p *SubscriberLabProof) { p.StartTicks, p.ActualStartTicks = "", "" },
		"unowned process":   func(p *SubscriberLabProof) { p.Name = "operator-vm" },
		"operator disk":     func(p *SubscriberLabProof) { p.Disk = "/data/chr.img" },
		"path traversal":    func(p *SubscriberLabProof) { p.Disk = p.Root + "/../run-0123abcd/disk.img" },
		"wrong port":        func(p *SubscriberLabProof) { p.APIPort = 8728 },
		"physical bridge":   func(p *SubscriberLabProof) { p.BridgePorts = append(p.BridgePorts, "ens33") },
		"missing client":    func(p *SubscriberLabProof) { p.BridgePorts = nil },
		"host address":      func(p *SubscriberLabProof) { p.BridgeHasIPv4 = true },
		"changed argv":      func(p *SubscriberLabProof) { p.ActualArgs[2] = "operator-vm" },
		"extra device": func(p *SubscriberLabProof) {
			p.Args = append(p.Args, "-drive", "file=/data/operator.img")
			p.ActualArgs = slices.Clone(p.Args)
		},
		"public forward": func(p *SubscriberLabProof) {
			p.Args[26] = "user,id=wan,hostfwd=tcp:0.0.0.0:18728-:8728"
			p.ActualArgs = slices.Clone(p.Args)
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			p := labProof()
			alter(&p)
			if CheckSubscriberLab(p) {
				t.Fatal("unsafe/unproven lab accepted")
			}
		})
	}
}
