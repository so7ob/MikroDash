package subscriberlab

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"

	"layeh.com/radius"
	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

type domainFixture struct {
	rows                          []routeros.Reply
	profile                       routeros.Reply
	active, ambiguousAdd, lostSet bool
	writes                        int
}

func newDomainFixture() *domainFixture {
	return &domainFixture{rows: []routeros.Reply{{".id": "*primary", "comment": "md-lab-radius", "disabled": "false", "address": "10.0.2.2", "authentication-port": "12812", "accounting-port": "12813", "service": "hotspot"}}, profile: routeros.Reply{".id": "*profile", "name": "hsprof-lab", "login-by": "https", "use-radius": "true", "radius-accounting": "true", "radius-default-domain": "", "split-user-domain": "false"}}
}

func (f *domainFixture) Do(cmd routeros.Cmd) ([]routeros.Reply, error) {
	args := map[string]string{}
	for _, arg := range cmd.Args {
		if parts := strings.SplitN(strings.TrimLeft(arg, "=?"), "=", 2); len(parts) == 2 {
			args[parts[0]] = parts[1]
		}
	}
	switch cmd.Path {
	case "/ip/hotspot/print":
		return []routeros.Reply{{"profile": "hsprof-lab", "interface": "ether2"}}, nil
	case "/ip/hotspot/active/print":
		if f.active {
			return []routeros.Reply{{".id": "*active"}}, nil
		}
		return nil, nil
	case "/ip/hotspot/profile/print":
		return []routeros.Reply{maps.Clone(f.profile)}, nil
	case "/radius/print":
		rows := []routeros.Reply{}
		for _, row := range f.rows {
			rows = append(rows, maps.Clone(row))
		}
		return rows, nil
	case "/radius/add":
		f.writes++
		delete(args, "secret")
		args[".id"], args["disabled"] = "*secondary", "false"
		f.rows = append(f.rows, args)
		if f.ambiguousAdd {
			return nil, errors.New("synthetic lost add reply after NAS write")
		}
		return nil, nil
	case "/ip/hotspot/profile/set":
		f.writes++
		if args[".id"] != f.profile[".id"] {
			return nil, errors.New("wrong synthetic profile")
		}
		f.profile["split-user-domain"] = map[string]string{"yes": "true", "no": "false"}[args["split-user-domain"]]
		if value, ok := args["radius-default-domain"]; ok {
			f.profile["radius-default-domain"] = value
		}
		if f.lostSet && args["split-user-domain"] == "yes" {
			return nil, errors.New("synthetic lost profile reply after NAS write")
		}
		return nil, nil
	case "/radius/remove":
		f.writes++
		f.rows = slices.DeleteFunc(f.rows, func(row routeros.Reply) bool { return row[".id"] == args[".id"] })
		return nil, nil
	default:
		return nil, errors.New("unexpected domain fixture command")
	}
}

func TestDomainTrialRestoresUncertainMutationsAndInjectedFailure(t *testing.T) {
	for _, mode := range []string{"normal", "secondary-first", "lost-add", "lost-profile-set", "injected-failure", "exercise-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newDomainFixture()
			before, err := readDomainConfig(f)
			if err != nil {
				t.Fatal(err)
			}
			f.ambiguousAdd, f.lostSet = mode == "lost-add", mode == "lost-profile-set"
			var evidence bytes.Buffer
			calls := 0
			err = runDomainTrial(f, "synthetic-secret-for-unit-test-only", &recorder{w: &evidence}, mode == "injected-failure", func(config domainBaseline) error {
				calls++
				if config.Profile["split-user-domain"] != "true" || len(config.Radius) != 2 {
					t.Fatal("exercise ran without verified domain configuration")
				}
				if mode == "secondary-first" {
					slices.Reverse(f.rows)
					f.profile["radius-default-domain"] = domainProbeName
				}
				if mode == "exercise-failure" {
					return errors.New("synthetic exercise failure")
				}
				return nil
			})
			if (err == nil) != (mode == "normal" || mode == "secondary-first") || (calls == 1) != (mode == "normal" || mode == "secondary-first" || mode == "exercise-failure") {
				t.Fatal("wrong trial result/call count", err, calls)
			}
			after, readErr := readDomainConfig(f)
			if readErr != nil || !reflect.DeepEqual(before, after) || !strings.Contains(evidence.String(), `"verified":true`) || f.writes < 2 {
				t.Fatal("original configuration or rollback proof missing", after, evidence.String())
			}
		})
	}
}

func TestDomainTrialRefusesUnownedConfigurationBeforeWriting(t *testing.T) {
	for _, mode := range []string{"active", "primary-domain", "profile-domain", "split-already-enabled", "another-radius", "evidence-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newDomainFixture()
			var writer io.Writer = io.Discard
			switch mode {
			case "active":
				f.active = true
			case "primary-domain":
				f.rows[0]["domain"] = "other-domain"
			case "profile-domain":
				f.profile["radius-default-domain"] = "other-domain"
			case "split-already-enabled":
				f.profile["split-user-domain"] = "true"
			case "another-radius":
				f.rows = append(f.rows, maps.Clone(f.rows[0]))
			case "evidence-failure":
				writer = failingDomainWriter{}
			}
			err := runDomainTrial(f, "synthetic-test-only", &recorder{w: writer}, false, func(domainBaseline) error { t.Fatal("unowned preflight reached exercise"); return nil })
			if err == nil || f.writes != 0 {
				t.Fatal("unowned configuration mutated", err, f.writes)
			}
		})
	}
}

type failingDomainWriter struct{}

func (failingDomainWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic evidence disk error")
}

func TestDomainRollbackRefusesDriftBeforeAnyWrite(t *testing.T) {
	for _, mode := range []string{"profile", "primary", "secondary", "ambiguous-secondary"} {
		t.Run(mode, func(t *testing.T) {
			f := newDomainFixture()
			before, _ := domainPreflight(f)
			err := runDomainTrial(f, "synthetic-test-only", &recorder{w: io.Discard}, false, func(domainBaseline) error {
				switch mode {
				case "profile":
					f.profile["html-directory-override"] = "different-owner"
				case "primary":
					f.rows[0]["address"] = "198.51.100.20"
				case "secondary":
					f.rows[1]["domain"] = "different-owner"
				case "ambiguous-secondary":
					f.rows = append(f.rows, maps.Clone(f.rows[1]))
				}
				return nil
			})
			if err == nil || f.writes != 2 {
				t.Fatal("rollback changed drifted configuration", err, f.writes)
			}
			if reflect.DeepEqual(before.Profile, f.profile) {
				t.Fatal("fixture did not retain the changed split field")
			}
		})
	}
}

func TestDomainRoutingCounterEvidenceRejectsTransportFailuresAndResets(t *testing.T) {
	before := map[string]string{"requests": "20", "accepts": "0", "rejects": "20", "resends": "0", "timeouts": "0", "bad-replies": "0"}
	after := maps.Clone(before)
	after["requests"], after["rejects"] = "21", "21"
	if delta, err := domainCounterDelta(before, after); err != nil || delta["requests"] != 1 || delta["rejects"] != 1 {
		t.Fatal(delta, err)
	}
	for _, mutation := range []struct{ key, value string }{{"requests", ""}, {"requests", "19"}, {"rejects", "unknown"}, {"rejects", "22"}, {"accepts", "1"}, {"resends", "1"}, {"timeouts", "1"}, {"bad-replies", "1"}} {
		bad := maps.Clone(after)
		bad[mutation.key] = mutation.value
		if _, err := domainCounterDelta(before, bad); err == nil {
			t.Fatal("invalid counter evidence accepted", mutation)
		}
	}
}

func TestDomainRejectingObserverIgnoresAccountingWithoutALedger(t *testing.T) {
	capture := &domainCapture{}
	service := &subscriber.RadiusService{Mode: "shadow", Clients: []subscriber.NAS{{RouterID: "domain-lab", Address: "127.0.0.1", Network: "C", CalledStationID: "hs-lab"}}}
	for _, code := range []radius.Code{radius.CodeAccountingRequest, radius.CodeCoARequest, radius.CodeDisconnectRequest} {
		capture.serve(service, nil, &radius.Request{Packet: radius.New(code, nil), RemoteAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}})
	}
	if len(capture.snapshot()) != 0 {
		t.Fatal("non-authentication traffic entered domain identity evidence")
	}
}
