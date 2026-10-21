package subscriberlab

import (
	"errors"
	"maps"
	"reflect"
	"time"

	"mikrodash/internal/routeros"
)

const domainProbeComment = "md-lab-domain-probe"
const domainProbeName = "md-pilot"

const domainRadiusFields = ".id,comment,disabled,address,authentication-port,accounting-port,service,domain,realm,src-address,protocol,timeout,require-message-auth"
const domainProfileFields = ".id,name,login-by,use-radius,radius-accounting,radius-default-domain,split-user-domain,html-directory-override,ssl-certificate"

type domainBaseline struct {
	Radius  []routeros.Reply
	Profile routeros.Reply
}

func readDomainConfig(r landingRouter) (domainBaseline, error) {
	rows, err := landingRead(r, "/radius/print", "=.proplist="+domainRadiusFields)
	if err != nil {
		return domainBaseline{}, err
	}
	profiles, err := landingRead(r, "/ip/hotspot/profile/print", "?name=hsprof-lab", "=.proplist="+domainProfileFields)
	if err != nil || len(profiles) != 1 || profiles[0][".id"] == "" {
		return domainBaseline{}, errors.New("unique owned domain probe profile required")
	}
	return domainBaseline{rows, profiles[0]}, nil
}

func domainPreflight(r landingRouter) (domainBaseline, error) {
	if _, err := landingProfile(r); err != nil {
		return domainBaseline{}, err
	}
	before, err := readDomainConfig(r)
	if err != nil {
		return before, err
	}
	if _, err = selectionPrimary(before.Radius); err != nil {
		return before, err
	}
	p := before.Profile
	if before.Radius[0]["domain"] != "" || before.Radius[0]["realm"] != "" || p["name"] != "hsprof-lab" || p["use-radius"] != "true" || p["radius-accounting"] != "true" || p["radius-default-domain"] != "" || p["split-user-domain"] != "false" {
		return before, errors.New("original blank-domain HTTPS RADIUS profile required")
	}
	return before, nil
}

func ownedDomainEntry(row routeros.Reply) bool {
	return row[".id"] != "" && row["comment"] == domainProbeComment && row["address"] == "10.0.2.2" && row["authentication-port"] == "12816" && row["accounting-port"] == "12817" && row["service"] == "hotspot" && row["domain"] == domainProbeName && row["realm"] == "" && row["disabled"] == "false" && row["protocol"] == "udp" && (row["src-address"] == "" || row["src-address"] == "0.0.0.0")
}

// Restore after uncertain add/set outcomes too. Never overwrite a drifted field
// or remove an entry whose public configuration no longer matches this probe.
func restoreDomainConfig(r landingRouter, before domainBaseline) error {
	current, err := readDomainConfig(r)
	if err != nil {
		return err
	}
	retained := []routeros.Reply{}
	owned := []routeros.Reply{}
	for _, row := range current.Radius {
		if row["comment"] == domainProbeComment {
			if !ownedDomainEntry(row) {
				return errors.New("domain rollback refused changed secondary")
			}
			owned = append(owned, row)
		} else {
			retained = append(retained, row)
		}
	}
	if len(owned) > 1 || !reflect.DeepEqual(retained, before.Radius) {
		return errors.New("domain rollback refused ambiguous or changed RADIUS entries")
	}
	profile := maps.Clone(current.Profile)
	profile["split-user-domain"] = before.Profile["split-user-domain"]
	profile["radius-default-domain"] = before.Profile["radius-default-domain"]
	if !reflect.DeepEqual(profile, before.Profile) || current.Profile["split-user-domain"] != "false" && current.Profile["split-user-domain"] != "true" || current.Profile["radius-default-domain"] != "" && current.Profile["radius-default-domain"] != domainProbeName {
		return errors.New("domain rollback refused changed profile")
	}
	if current.Profile["split-user-domain"] != before.Profile["split-user-domain"] || current.Profile["radius-default-domain"] != before.Profile["radius-default-domain"] {
		if _, err = landingRead(r, "/ip/hotspot/profile/set", "=.id="+before.Profile[".id"], "=split-user-domain=no", "=radius-default-domain="); err != nil {
			return err
		}
	}
	for _, row := range owned {
		if _, err = landingRead(r, "/radius/remove", "=.id="+row[".id"]); err != nil {
			return err
		}
	}

	after, err := readDomainConfig(r)
	if err != nil || !reflect.DeepEqual(after, before) {
		return errors.New("exact domain rollback configuration not verified")
	}
	return nil
}

func runDomainTrial(r landingRouter, secret string, record *recorder, fault bool, exercise func(domainBaseline) error) (retErr error) {
	before, err := domainPreflight(r)
	if err != nil {
		return err
	}
	record.emit("domain-before", before)
	if err := record.Err(); err != nil {
		return err
	}
	// Install cleanup before add: a timeout can follow a successful NAS write.
	defer func() {
		err := restoreDomainConfig(r, before)
		record.emit("domain-rollback", map[string]bool{"verified": err == nil})
		retErr = errors.Join(retErr, err, record.Err())
	}()
	if _, err = landingRead(r, "/radius/add", "=comment="+domainProbeComment, "=domain="+domainProbeName, "=address=10.0.2.2", "=authentication-port=12816", "=accounting-port=12817", "=service=hotspot", "=protocol=udp", "=secret="+secret); err != nil {
		return err
	}
	current, err := readDomainConfig(r)
	if err != nil || len(current.Radius) != 2 || !reflect.DeepEqual(current.Radius[0], before.Radius[0]) || !ownedDomainEntry(current.Radius[1]) || !reflect.DeepEqual(current.Profile, before.Profile) {
		return errors.New("domain secondary readback failed")
	}
	if _, err = landingRead(r, "/ip/hotspot/profile/set", "=.id="+before.Profile[".id"], "=split-user-domain=yes"); err != nil {
		return err
	}
	current, err = readDomainConfig(r)
	if err != nil || current.Profile["split-user-domain"] != "true" {
		return errors.New("domain split readback failed")
	}
	if fault {
		return errors.New("synthetic injected failure after domain profile change")
	}
	return exercise(current)
}

func domainCounters(r landingRouter, id string) (routeros.Reply, error) {
	rows, err := r.Do(routeros.Cmd{Path: "/radius/monitor", Args: []string{"=numbers=" + id, "=once="}, Timeout: 3 * time.Second})
	if err != nil || len(rows) != 1 {
		return nil, errors.New("domain endpoint counters unavailable")
	}
	return rows[0], nil
}
