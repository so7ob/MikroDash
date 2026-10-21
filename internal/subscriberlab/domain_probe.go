package subscriberlab

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/microsoft"
	"layeh.com/radius/vendors/mikrotik"
	"mikrodash/internal/subscriber"
)

type domainIdentity struct {
	Username string `json:"username"`
	Domain   string `json:"domain"`
	Realm    string `json:"realm"`
}

type domainCapture struct {
	mu   sync.Mutex
	rows []domainIdentity
}

func (d *domainCapture) snapshot() []domainIdentity {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]domainIdentity{}, d.rows...)
}

// This rejecting observer has no accounting ledger. Ignore every other packet
// before invoking the production service, including a valid misplaced NAS marker.
func (d *domainCapture) serve(service *subscriber.RadiusService, w radius.ResponseWriter, r *radius.Request) {
	if r.Packet.Code != radius.CodeAccessRequest {
		return
	}
	d.mu.Lock()
	d.rows = append(d.rows, domainIdentity{rfc2865.UserName_GetString(r.Packet), microsoft.MSCHAPDomain_GetString(r.Packet), mikrotik.MikrotikRealm_GetString(r.Packet)})
	d.mu.Unlock()
	service.ServeRADIUS(w, r)
}

// RadiusDomainProbe measures same-profile domain selection on the owned CHR.
// The primary runtime stays running; every synthetic login is rejected. A
// completed measurement is not a claim of target or User Manager qualification.
func (c *Client) RadiusDomainProbe(ctx context.Context, privateDir string, out io.Writer, fault bool) error {
	if _, err := domainPreflight(c); err != nil {
		return err
	}
	var fixture FixtureCredentials
	if err := privateJSON(filepath.Join(privateDir, "fixture.json"), &fixture); err != nil {
		return err
	}
	if len(fixture.Secret) < 32 || len(fixture.Password) < 24 {
		return errors.New("random private domain probe credentials required")
	}
	capture := &domainCapture{}
	// Shadow validates the normal NAS/Message-Authenticator path and returns a
	// signed rejection. No credentials, tickets or accounting grants are issued.
	service := &subscriber.RadiusService{Mode: "shadow", Clients: []subscriber.NAS{{RouterID: "domain-selection", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", Secret: fixture.Secret}}}
	conn, err := net.ListenPacket("udp", "127.0.0.1:12816")
	if err != nil {
		return err
	}
	server := &radius.PacketServer{SecretSource: service, ErrorLog: log.New(io.Discard, "", 0), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		capture.serve(service, w, r)
	})}
	go server.Serve(conn)
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(stop)
	}()
	record := &recorder{w: out}
	return runDomainTrial(c, fixture.Secret, record, fault, func(config domainBaseline) error {
		qualified := []string{}
		for _, order := range []string{"last", "first"} {
			if order == "first" {
				if _, err = landingRead(c, "/radius/move", "=numbers="+config.Radius[1][".id"], "=destination="+config.Radius[0][".id"]); err != nil {
					return err
				}
				readback, err := readDomainConfig(c)
				if err != nil || len(readback.Radius) != 2 || readback.Radius[0][".id"] != config.Radius[1][".id"] || readback.Radius[1][".id"] != config.Radius[0][".id"] {
					return errors.New("domain entry order not verified")
				}
			}
			for _, split := range []string{"no", "yes"} {
				if _, err = landingRead(c, "/ip/hotspot/profile/set", "=.id="+config.Profile[".id"], "=split-user-domain="+split); err != nil {
					return err
				}
				actual, err := readDomainConfig(c)
				expectedSplit := map[string]string{"yes": "true", "no": "false"}[split]
				if err != nil || actual.Profile["split-user-domain"] != expectedSplit || actual.Profile["radius-default-domain"] != "" {
					return errors.New("domain matrix profile state not verified")
				}
				for _, username := range []string{"legacy-lab", `md-pilot\legacy-lab`, "legacy-lab@md-pilot"} {
					before, err := domainCounters(c, config.Radius[0][".id"])
					if err != nil {
						return err
					}
					secondaryBefore, err := domainCounters(c, config.Radius[1][".id"])
					if err != nil {
						return err
					}
					count := len(capture.snapshot())
					if err := c.loginAs(ctx, privateDir, username, fixture.Password, true); err != nil {
						return err
					}
					after, err := domainCounters(c, config.Radius[0][".id"])
					if err != nil {
						return err
					}
					secondaryAfter, err := domainCounters(c, config.Radius[1][".id"])
					if err != nil {
						return err
					}
					active, err := landingRead(c, "/ip/hotspot/active/print", "=.proplist=.id,user")
					if err != nil || len(active) != 0 {
						return errors.New("rejecting domain probe unexpectedly admitted a session")
					}
					primary, err := domainCounterDelta(before, after)
					if err != nil {
						return err
					}
					secondary, err := domainCounterDelta(secondaryBefore, secondaryAfter)
					if err != nil {
						return err
					}
					identities := capture.snapshot()[count:]
					record.emit("domain-routing-attempt", map[string]any{"secondaryOrder": order, "splitUserDomain": split, "submittedUsername": username, "primary": primary, "secondary": secondary, "receivedBySecondary": identities, "activeSessions": 0})
					if split == "yes" && username != "legacy-lab" && primary["requests"] == 0 && secondary["requests"] == 1 && secondary["rejects"] == 1 && len(identities) == 1 && identities[0].Username == "legacy-lab" {
						qualified = append(qualified, order+":"+username)
					}
					if username == "legacy-lab" && (primary["rejects"] != 1 || secondary["requests"] != 0 || len(identities) != 0) {
						return errors.New("domain endpoint intercepted plain legacy login or primary rejection was not confirmed")
					}
				}
			}
		}
		// A zero-request negative measurement is meaningful only after proving
		// this endpoint receives and signs a real NAS request when selected by
		// the documented profile default-domain mechanism.
		if _, err = landingRead(c, "/ip/hotspot/profile/set", "=.id="+config.Profile[".id"], "=radius-default-domain="+domainProbeName); err != nil {
			return err
		}
		readback, err := readDomainConfig(c)
		if err != nil || readback.Profile["radius-default-domain"] != domainProbeName {
			return errors.New("domain positive-control profile not verified")
		}
		primaryBefore, err := domainCounters(c, config.Radius[0][".id"])
		if err != nil {
			return err
		}
		secondaryBefore, err := domainCounters(c, config.Radius[1][".id"])
		if err != nil {
			return err
		}
		count := len(capture.snapshot())
		if err = c.loginAs(ctx, privateDir, "legacy-lab", fixture.Password, true); err != nil {
			return err
		}
		primaryAfter, err := domainCounters(c, config.Radius[0][".id"])
		if err != nil {
			return err
		}
		secondaryAfter, err := domainCounters(c, config.Radius[1][".id"])
		if err != nil {
			return err
		}
		primary, err := domainCounterDelta(primaryBefore, primaryAfter)
		if err != nil {
			return err
		}
		secondary, err := domainCounterDelta(secondaryBefore, secondaryAfter)
		if err != nil {
			return err
		}
		identities := capture.snapshot()[count:]
		active, err := landingRead(c, "/ip/hotspot/active/print", "=.proplist=.id")
		verified := err == nil && len(active) == 0 && primary["requests"] == 0 && secondary["requests"] == 1 && secondary["rejects"] == 1 && len(identities) == 1 && identities[0].Username == "legacy-lab"
		record.emit("domain-positive-control", map[string]any{"verified": verified, "primary": primary, "secondary": secondary, "receivedBySecondary": identities})
		if !verified {
			return errors.New("real domain endpoint positive control failed")
		}
		record.emit("domain-qualification", map[string]any{"normalizedIsolatedForms": qualified, "targetVerified": false, "userManagerVerified": false})
		return record.Err()
	})
}

func domainCounterDelta(before, after map[string]string) (map[string]uint64, error) {
	out := map[string]uint64{}
	for _, key := range []string{"requests", "accepts", "rejects", "resends", "timeouts", "bad-replies"} {
		a, err := strconv.ParseUint(before[key], 10, 64)
		if err != nil {
			return nil, errors.New("missing initial RADIUS counter")
		}
		b, err := strconv.ParseUint(after[key], 10, 64)
		if err != nil || b < a {
			return nil, errors.New("RADIUS counter unavailable or reset")
		}
		out[key] = b - a
	}
	if out["requests"] != out["rejects"] || out["accepts"] != 0 || out["bad-replies"] != 0 || out["timeouts"] != 0 || out["resends"] != 0 {
		return nil, errors.New("domain probe admission or transport failure; routing not qualified")
	}
	return out, nil
}
