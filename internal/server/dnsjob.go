package server

import (
	"context"

	"mikrodash/internal/store"
)

// dnsJobActor keeps the initiating identity and feature for the whole job.
// A scheduled list is an explicit system task, never a username convention.
type dnsJobActor struct {
	userID, username, feature string
	system                    bool
	ctx                       context.Context
}

func (s *Server) dnsActor(sess *Session, feature string) dnsJobActor {
	return dnsJobActor{userID: s.userIDFor(sess.Username), username: sess.Username, feature: feature}
}

// dnsJobStop is checked inside the router's write queue, immediately before
// each command, and while waiting for the job allowance. Empty means proceed.
func (s *Server) dnsJobStop(a dnsJobActor, routerID string) string {
	if a.ctx != nil && a.ctx.Err() != nil {
		return "stopped: task cancelled"
	}
	select {
	case <-s.featuresDone():
		return "stopped: MikroDash shut down"
	default:
	}
	if !s.featureOn(a.feature) {
		return "stopped: feature switched off"
	}
	if s.auditDB == nil || s.store == nil {
		return "stopped: authorization unavailable"
	}
	routers, problems := s.store.Routers()
	if len(problems) != 0 {
		return "stopped: router unavailable"
	}
	enabled := false
	for _, r := range routers {
		if r.ID == routerID && !r.Disabled {
			enabled = true
			break
		}
	}
	if !enabled {
		return "stopped: router unavailable"
	}
	if a.system {
		if a.feature == store.FeatureDNSLists && a.userID == "" {
			return ""
		}
		return "stopped: invalid system task"
	}
	users, err := s.store.Users()
	exists := false
	if err == nil && a.userID != "" {
		for _, u := range users {
			if u.ID == a.userID {
				exists = true
				break
			}
		}
	}
	if !exists || !permitted(s.rbac.CanPage(a.userID, "dns", "write", routerID)) {
		return "stopped: permission no longer granted"
	}
	if a.feature == store.FeatureDNSLists && !permitted(s.rbac.Can(a.userID, "system:principals", "")) {
		return "stopped: administrator access no longer granted"
	}
	return ""
}
