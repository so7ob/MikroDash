package cfgdeploy

import (
	"strings"
	"testing"

	"mikrodash/internal/cfgtpl"
	"mikrodash/internal/routeros"
)

// Exercise the same bind/prepare path as the preview and deploy. The second
// case models the operator's working rules, including unrelated logging: an
// addition must reuse them and must not reset the USB action or its files.
func TestActivityTemplateReusesExistingLogging(t *testing.T) {
	c, ok := cfgtpl.CannedByID("activity-logging")
	if !ok {
		t.Fatal("Activity is missing from the shipped library")
	}
	tpl, err := cfgtpl.Parse(c.Body)
	if err != nil {
		t.Fatal(err)
	}
	bound, vals, err := cfgtpl.Bind(tpl, c.Variables, map[string]string{
		"archive_path": "usb-test/Activity/netlog", "file_count": "42", "lines_per_file": "321",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new setup", true: "reapply"}[existing], func(t *testing.T) {
			live := map[string][]routeros.Reply{"/system/script/print": {}, "/system/logging/action/print": {}, "/system/logging/print": {}}
			if existing {
				live["/system/script/print"] = []routeros.Reply{{"name": "DNSUserLine", "source": ":log info old"}}
				live["/system/logging/action/print"] = []routeros.Reply{
					{"name": "DNSJoin", "target": "script", "script": "DNSUserLine"},
					{"name": "NetArchive", "target": "disk", "disk-file-name": "usb-test/Activity/netlog"},
					{"name": "unrelated", "target": "memory"},
				}
				live["/system/logging/print"] = []routeros.Reply{
					{"action": "DNSJoin", "topics": "dns", "regex": "query from [0-9.]+: #", "disabled": "false"},
					{"action": "NetArchive", "topics": "script", "regex": "^DNSUSER", "disabled": "false"},
					{"action": "NetArchive", "topics": "script,info", "regex": "^HS_", "disabled": "true"},
					{"action": "unrelated", "topics": "error", "regex": ""},
				}
			}
			reads := map[string]int{}
			do := func(cmd routeros.Cmd) ([]routeros.Reply, error) {
				rows, ok := live[cmd.Path]
				if !ok {
					t.Fatalf("preview sent unexpected command %s", cmd.Path)
				}
				reads[cmd.Path]++
				return rows, nil
			}
			plan := Plan{Template: bound, Values: vals, Live: lab, Shipped: true}
			prep, err := Prepare(do, plan)
			if err != nil {
				t.Fatal(err)
			}
			if len(reads) != 3 {
				t.Fatalf("only read %d setup menus", len(reads))
			}
			for path, n := range reads {
				if n != 1 {
					t.Errorf("%s read %d times", path, n)
				}
			}
			if cfgtpl.Worst(prep.Findings) != cfgtpl.Ack || LockClass(prep.Findings) {
				t.Fatalf("setup findings %+v", prep.Findings)
			}
			if len(prep.Ensure) != 6 {
				t.Fatalf("expected six independently reused rows, got %d", len(prep.Ensure))
			}
			for _, e := range prep.Ensure {
				if e.Added == existing {
					t.Errorf("line %d: added=%v on reapply=%v", e.Line, e.Added, existing)
				}
			}
			rendered, err := cfgtpl.Parse(prep.Rendered)
			if err != nil {
				t.Fatal(err)
			}
			additions, rules, scripts := 0, 0, 0
			values := map[string]string{}
			for _, line := range rendered.Lines {
				if line.Verb == "add" {
					additions++
				}
				if line.Verb != "add" && line.Verb != "set" {
					t.Fatalf("unexpected %s: may delete/recreate archive settings", line.Verb)
				}
				if _, ok := live[line.Path()+"/print"]; !ok {
					t.Fatalf("changes a menu outside logging setup: %s", line.Path())
				}
				for _, a := range line.Args {
					v, ok := a.Value.Literal()
					if !ok {
						t.Fatal("unfilled setup variable")
					}
					values[a.Name] = v
				}
				if line.Path() == "/system/logging" && line.Verb == "add" {
					rules++
				}
				if line.Path() == "/system/logging/action" && line.Verb == "set" {
					target, _ := line.Arg("target")
					got, _ := target.Literal()
					for _, a := range line.Find {
						name, _ := a.Value.Literal()
						if name == "NetArchive" && got != "disk" {
							t.Fatal("archive action does not persist to disk")
						}
					}
				}
				if line.Path() == "/system/script" && line.Verb == "set" {
					v, _ := line.Arg("source")
					src, _ := v.Literal()
					scripts++
					if !strings.Contains(src, `:tostr $message`) || !strings.Contains(src, `:log info ("DNSUSER " . $payload)`) {
						t.Fatal("source did not round-trip as executable script text")
					}
				}
			}
			wantAdds, wantRules := 6, 3
			if existing {
				wantAdds, wantRules = 0, 0
			}
			if additions != wantAdds || rules != wantRules || scripts != 1 {
				t.Fatalf("adds=%d rules=%d scripts=%d", additions, rules, scripts)
			}
			for k, want := range map[string]string{"disk-file-name": "usb-test/Activity/netlog", "disk-file-count": "42", "disk-lines-per-file": "321", "disk-stop-on-full": "no", "script": "DNSUserLine", "policy": "read,write,test", "dont-require-permissions": "yes"} {
				if values[k] != want {
					t.Errorf("%s=%q, want %q", k, values[k], want)
				}
			}
			// The library's existing code acknowledgements still gate this template;
			// copying the text into a custom template does not acquire shipped trust.
			a := Additions{Approved: prep.Hash, Acked: map[string]bool{}}
			if code, _ := judge(prep, a); code != "needs-ack" {
				t.Fatal("script deployed without acknowledgement")
			}
			for _, f := range prep.Findings {
				a.Acked[FindingKey(f)] = true
			}
			if code, _ := judge(prep, a); code != "" {
				t.Fatal("acknowledged shipped setup refused")
			}
			plan.Shipped = false
			clone, err := Prepare(do, plan)
			if err != nil {
				t.Fatal(err)
			}
			if cfgtpl.Worst(clone.Findings) != cfgtpl.Refuse {
				t.Fatal("clone bypassed the custom-script guard")
			}
		})
	}
}
