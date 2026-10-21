package subscriberlab

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

type landingReceipt struct {
	VM, StartTicks, ProfileID, Previous, Directory, ManifestSHA string
}

type landingRouter interface {
	Do(routeros.Cmd) ([]routeros.Reply, error)
}

// InstallLanding changes only the owned disposable CHR profile. Every command
// still passes through Client.Do and its process, topology and NAS guards.
func (c *Client) InstallLanding(privateDir, web string, rollback bool) error {
	if privateDir != filepath.Dir(c.state.Console) || !c.state.NativePortal {
		return errors.New("owned native-portal lab required")
	}
	if _, err := c.Identity(); err != nil {
		return err
	}
	if rollback {
		var receipt landingReceipt
		if err := privateJSON(filepath.Join(privateDir, "landing-review.json"), &receipt); err != nil {
			return err
		}
		if receipt.VM != c.state.Name || receipt.StartTicks != c.state.StartTicks || !strings.HasPrefix(receipt.Directory, "md-subscriber-") || len(receipt.ManifestSHA) != 64 || receipt.Directory != "md-subscriber-"+receipt.ManifestSHA[:16] {
			return errors.New("landing rollback ownership changed")
		}
		return restoreLanding(c, receipt)
	}
	raw, err := os.ReadFile(filepath.Join(privateDir, "runtime-config.json"))
	if err != nil {
		return errors.New("owned runtime configuration required")
	}
	cfg, err := subscriber.DecodeConfig(raw)
	if err != nil || cfg.PortalOrigin != "https://portal.example.test:9444" || len(cfg.Clients) != 1 || cfg.Clients[0].Network != "C" || cfg.Clients[0].CalledStationID != "hs-lab" || cfg.Clients[0].LoginURL != "https://login.example.test/login" {
		return errors.New("exact synthetic portal and C-like NAS required")
	}
	script, err := os.ReadFile(filepath.Join(web, "subscriber-handoff.js"))
	if err != nil {
		return errors.New("build the reviewed handoff asset first")
	}
	bundle, err := subscriber.RenderHotspotLanding(cfg, "C", script)
	if err != nil {
		return err
	}
	manifest, err := bundle.Manifest()
	if err != nil {
		return err
	}
	digest := subscriber.Digest(string(manifest))
	receipt := landingReceipt{VM: c.state.Name, StartTicks: c.state.StartTicks, Directory: "md-subscriber-" + digest[:16], ManifestSHA: digest}
	profile, err := landingProfile(c)
	if err != nil {
		return err
	}
	receipt.ProfileID, receipt.Previous = profile[".id"], profile["html-directory-override"]
	file := filepath.Join(privateDir, "landing-review.json")
	if _, err = os.Lstat(file); err == nil {
		var previous landingReceipt
		if err = privateJSON(file, &previous); err != nil {
			return err
		}
		if previous.VM != receipt.VM || previous.StartTicks != receipt.StartTicks || previous.ProfileID != receipt.ProfileID || previous.Directory != receipt.Directory || previous.ManifestSHA != receipt.ManifestSHA {
			return errors.New("existing landing review differs; inspect before replacing it")
		}
		receipt = previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		rows, err := landingRead(c, "/file/print", "?name="+receipt.Directory, "=.proplist=name")
		if err != nil || len(rows) != 0 || receipt.Previous != "" {
			return errors.New("new unclaimed lab directory and original profile required")
		}
		if err = writePrivateJSON(file, receipt); err != nil {
			return err
		}
	}
	return applyLanding(c, receipt, bundle)
}

func landingRead(c landingRouter, path string, args ...string) ([]routeros.Reply, error) {
	return c.Do(routeros.Cmd{Path: path, Args: args, Timeout: 5 * time.Second})
}

func landingProfile(c landingRouter) (routeros.Reply, error) {
	rows, err := landingRead(c, "/ip/hotspot/print", "?name=hs-lab", "=.proplist=name,profile,interface")
	if err != nil || len(rows) != 1 || rows[0]["profile"] != "hsprof-lab" || rows[0]["interface"] != "ether2" {
		return nil, errors.New("exact isolated HotSpot required")
	}
	rows, err = landingRead(c, "/ip/hotspot/active/print", "=.proplist=.id")
	if err != nil || len(rows) != 0 {
		return nil, errors.New("landing changes require an idle disposable HotSpot")
	}
	rows, err = landingRead(c, "/ip/hotspot/profile/print", "?name=hsprof-lab", "=.proplist=.id,name,login-by,html-directory-override")
	if err != nil || len(rows) != 1 || rows[0][".id"] == "" || rows[0]["login-by"] != "https" {
		return nil, errors.New("unique HTTPS lab profile required")
	}
	return rows[0], nil
}

func applyLanding(c landingRouter, receipt landingReceipt, bundle subscriber.LandingBundle) error {
	profile, err := landingProfile(c)
	if err != nil {
		return err
	}
	if profile[".id"] != receipt.ProfileID || profile["html-directory-override"] != receipt.Previous && profile["html-directory-override"] != receipt.Directory {
		return errors.New("lab profile changed after landing review")
	}
	rows, err := landingRead(c, "/file/print", "=.proplist=name,type,contents")
	if err != nil {
		return errors.New("cannot inspect disposable files")
	}
	files := map[string]bool{}
	folder := false
	for _, row := range rows {
		if row["name"] == receipt.Directory {
			if folder || row["type"] != "directory" {
				return errors.New("reviewed directory identity changed")
			}
			folder = true
		} else if strings.HasPrefix(row["name"], receipt.Directory+"/") {
			name := strings.TrimPrefix(row["name"], receipt.Directory+"/")
			body, ok := bundle.Files[name]
			if !ok || files[name] || row["contents"] != string(body) {
				return errors.New("existing reviewed asset differs or is unowned")
			}
			files[name] = true
		}
	}
	if !folder {
		if _, err = landingRead(c, "/file/add", "=name="+receipt.Directory, "=type=directory"); err != nil {
			return errors.New("cannot create disposable landing directory")
		}
	}
	names := make([]string, 0, len(bundle.Files))
	for name := range bundle.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !files[name] {
			if _, err = landingRead(c, "/file/add", "=name="+receipt.Directory+"/"+name, "=type=file", "=contents="+string(bundle.Files[name])); err != nil {
				return errors.New("landing upload incomplete; original profile retained")
			}
		}
		rows, err = landingRead(c, "/file/print", "?name="+receipt.Directory+"/"+name, "=.proplist=name,contents")
		if err != nil || len(rows) != 1 || rows[0]["contents"] != string(bundle.Files[name]) {
			return errors.New("landing asset readback failed; profile not changed")
		}
	}
	profile, err = landingProfile(c)
	if err != nil || profile[".id"] != receipt.ProfileID || profile["html-directory-override"] != receipt.Previous && profile["html-directory-override"] != receipt.Directory {
		return errors.New("lab profile drifted during landing upload")
	}
	if profile["html-directory-override"] != receipt.Directory {
		if _, err = landingRead(c, "/ip/hotspot/profile/set", "=numbers="+receipt.ProfileID, "=html-directory-override="+receipt.Directory); err != nil {
			return errors.New("landing profile update unconfirmed; retain review and inspect")
		}
	}
	profile, err = landingProfile(c)
	if err != nil || profile["html-directory-override"] != receipt.Directory {
		return errors.New("landing profile readback failed")
	}
	return nil
}

func restoreLanding(c landingRouter, receipt landingReceipt) error {
	profile, err := landingProfile(c)
	if err != nil || profile[".id"] != receipt.ProfileID {
		return errors.New("cannot verify idle original lab profile")
	}
	current := profile["html-directory-override"]
	if current == receipt.Previous {
		return nil
	}
	if current != receipt.Directory {
		return errors.New("refusing rollback over another profile change")
	}
	if _, err = landingRead(c, "/ip/hotspot/profile/set", "=numbers="+receipt.ProfileID, "=html-directory-override="+receipt.Previous); err != nil {
		return errors.New("landing rollback unconfirmed; retain review and inspect")
	}
	profile, err = landingProfile(c)
	if err != nil || profile["html-directory-override"] != receipt.Previous {
		return errors.New("landing rollback readback failed")
	}
	return nil
}
