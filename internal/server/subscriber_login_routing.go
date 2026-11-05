package server

import (
	"context"
	"errors"
	"time"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

// subscriberReadLoginRouting is one bounded non-secret view per enrolled NAS,
// read through the same connection and limiter as accounting observations.
func (s *Server) subscriberReadLoginRouting(ctx context.Context, router string, legacy bool) (subscriber.LoginRoutingObservation, error) {
	out := subscriber.LoginRoutingObservation{Readiness: subscriber.Readiness{ObservedAt: time.Now(), Menus: map[string][]routeros.Reply{}, Unavailable: []string{}, Review: []string{}}, LegacyNames: []string{}, LegacyComplete: !legacy}
	rs := s.sessions.Live()[router]
	if rs == nil || !rs.Connected() {
		return out, errors.New("subscriber routing connection unavailable")
	}
	checks := []struct{ path, fields string }{
		{"/system/resource/print", "version"},
		{"/ip/hotspot/print", "name,profile,disabled"},
		{"/ip/hotspot/profile/print", "name,login-by,use-radius,radius-accounting,radius-default-domain,split-user-domain"},
		{"/radius/print", "address,authentication-port,accounting-port,domain,realm,service,disabled,protocol,require-message-auth"},
		{"/ip/hotspot/user/print", "name,server,disabled"},
	}
	if legacy {
		checks = append(checks, struct{ path, fields string }{"/user-manager/user/print", "name,disabled"})
	}
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		rows, err := rs.Exec(routeros.Cmd{Path: check.path, Args: []string{"=.proplist=" + check.fields}, Timeout: 2 * time.Second})
		if err != nil {
			return out, errors.New("subscriber routing metadata unavailable")
		}
		if rows == nil {
			rows = []routeros.Reply{}
		}
		out.Readiness.Menus[check.path] = rows
	}
	if rows := out.Readiness.Menus["/system/resource/print"]; len(rows) == 1 {
		out.Readiness.Version = rows[0]["version"]
	}
	if legacy {
		out.LegacyComplete = true
		for _, row := range out.Readiness.Menus["/user-manager/user/print"] {
			if row["name"] == "" || row["disabled"] != "true" && row["disabled"] != "false" {
				out.LegacyComplete = false
			}
			if row["disabled"] != "true" {
				out.LegacyNames = append(out.LegacyNames, row["name"])
			}
		}
	}
	return out, nil
}
