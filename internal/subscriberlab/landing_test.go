package subscriberlab

import (
	"errors"
	"strings"
	"testing"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

type landingFixtureRouter struct {
	files                                  map[string]routeros.Reply
	profile, iface                         string
	active, ambiguous, corrupt, failUpload bool
	writes, switches                       int
}

func (f *landingFixtureRouter) Do(cmd routeros.Cmd) ([]routeros.Reply, error) {
	args := map[string]string{}
	for _, arg := range cmd.Args {
		if parts := strings.SplitN(strings.TrimLeft(arg, "=?"), "=", 2); len(parts) == 2 {
			args[parts[0]] = parts[1]
		}
	}
	switch cmd.Path {
	case "/ip/hotspot/print":
		return []routeros.Reply{{"profile": "hsprof-lab", "interface": f.iface}}, nil
	case "/ip/hotspot/active/print":
		if f.active {
			return []routeros.Reply{{".id": "*active"}}, nil
		}
		return nil, nil
	case "/ip/hotspot/profile/print":
		row := routeros.Reply{".id": "*profile", "login-by": "https", "html-directory-override": f.profile}
		if f.ambiguous {
			return []routeros.Reply{row, row}, nil
		}
		return []routeros.Reply{row}, nil
	case "/file/print":
		out := []routeros.Reply{}
		for name, row := range f.files {
			if args["name"] == "" || args["name"] == name {
				copy := routeros.Reply{}
				for key, value := range row {
					copy[key] = value
				}
				if f.corrupt && strings.HasSuffix(name, "/login.html") {
					copy["contents"] = "corrupt"
				}
				out = append(out, copy)
			}
		}
		return out, nil
	case "/file/add":
		f.writes++
		if f.failUpload && args["type"] == "file" {
			return nil, errors.New("synthetic upload failure")
		}
		if _, ok := f.files[args["name"]]; ok {
			return nil, errors.New("duplicate synthetic file")
		}
		f.files[args["name"]] = routeros.Reply{"name": args["name"], "type": args["type"], "contents": args["contents"]}
		return nil, nil
	case "/ip/hotspot/profile/set":
		f.writes++
		f.switches++
		f.profile = args["html-directory-override"]
		return nil, nil
	default:
		return nil, errors.New("unexpected synthetic NAS command")
	}
}

func TestLandingInstallerVerifiesFilesBeforeSwitchAndResumesFailures(t *testing.T) {
	receipt := landingReceipt{ProfileID: "*profile", Directory: "md-subscriber-synthetic"}
	bundle := subscriber.LandingBundle{Files: map[string][]byte{"login.html": []byte("synthetic login"), "alogin.html": []byte("synthetic success")}}
	f := &landingFixtureRouter{files: map[string]routeros.Reply{}, iface: "ether2", failUpload: true}
	if err := applyLanding(f, receipt, bundle); err == nil || f.profile != "" || f.switches != 0 {
		t.Fatal("partial upload enabled or accepted", err)
	}
	f.failUpload = false
	if err := applyLanding(f, receipt, bundle); err != nil {
		t.Fatal("resumable upload failed", err)
	}
	if f.profile != receipt.Directory || f.switches != 1 {
		t.Fatal("complete readback did not apply")
	}
	writes := f.writes
	if err := applyLanding(f, receipt, bundle); err != nil || f.writes != writes {
		t.Fatal("repeat rewrote a reviewed asset", err)
	}
	if err := restoreLanding(f, receipt); err != nil || f.profile != "" {
		t.Fatal("original override not restored", err)
	}
	writes = f.writes
	if err := restoreLanding(f, receipt); err != nil || f.writes != writes {
		t.Fatal("rollback not idempotent", err)
	}
	if len(f.files) != 3 {
		t.Fatal("rollback deleted evidence")
	}
}

func TestLandingInstallerRefusesDriftActiveSessionsAndReadbackFailure(t *testing.T) {
	receipt := landingReceipt{ProfileID: "*profile", Directory: "md-subscriber-synthetic"}
	bundle := subscriber.LandingBundle{Files: map[string][]byte{"login.html": []byte("synthetic login")}}
	for _, mode := range []string{"active", "interface", "ambiguous", "profile-drift", "unknown-file", "corrupt-readback"} {
		t.Run(mode, func(t *testing.T) {
			f := &landingFixtureRouter{files: map[string]routeros.Reply{}, iface: "ether2"}
			switch mode {
			case "active":
				f.active = true
			case "interface":
				f.iface = "ether1"
			case "ambiguous":
				f.ambiguous = true
			case "profile-drift":
				f.profile = "other-review"
			case "unknown-file":
				f.files[receipt.Directory+"/operator.txt"] = routeros.Reply{"name": receipt.Directory + "/operator.txt", "contents": "retain"}
			case "corrupt-readback":
				f.corrupt = true
			}
			if err := applyLanding(f, receipt, bundle); err == nil || f.switches != 0 {
				t.Fatal("unsafe install accepted or profile changed", err)
			}
			if mode != "corrupt-readback" && f.writes != 0 {
				t.Fatal("refused preflight wrote to NAS")
			}
		})
	}
	f := &landingFixtureRouter{files: map[string]routeros.Reply{}, iface: "ether2", profile: "another-change"}
	if err := restoreLanding(f, receipt); err == nil || f.writes != 0 {
		t.Fatal("rollback overwrote drift", err)
	}
}
