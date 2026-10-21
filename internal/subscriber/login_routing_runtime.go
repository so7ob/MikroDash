package subscriber

import (
	"context"
	"errors"
	"sync"
	"time"
)

// LoginRoutingObservation is one immutable non-secret NAS configuration view.
// Legacy inventory is optional only when no transition authority is configured.
type LoginRoutingObservation struct {
	Readiness      Readiness
	LegacyNames    []string
	LegacyComplete bool
}

type loginRoutingState struct {
	Observation LoginRoutingObservation
	Valid       bool
}

type LoginRoutingStatus struct {
	RouterID   string    `json:"routerId"`
	Network    string    `json:"network"`
	Domain     string    `json:"domain"`
	ObservedAt time.Time `json:"observedAt"`
	Quality    string    `json:"quality"`
}

func (state loginRoutingState) freshFor(n NAS, at time.Time) bool {
	r := state.Observation.Readiness
	return state.Valid && n.Capabilities.Recorded() && r.Version == n.Capabilities.Version && !r.ObservedAt.IsZero() && at.Sub(r.ObservedAt) <= loginRoutingFreshness && !r.ObservedAt.After(at.Add(time.Second))
}

// LoginRoutingStatus reports metadata freshness, not permission for an account.
func (s *RadiusService) LoginRoutingStatus(n NAS, at time.Time) LoginRoutingStatus {
	out := LoginRoutingStatus{RouterID: n.RouterID, Network: n.Network, Domain: n.LoginDomain, Quality: "not-required"}
	if n.LoginDomain == "" {
		return out
	}
	s.mu.Lock()
	state, known := s.loginRoutes[n.RouterID]
	s.mu.Unlock()
	out.ObservedAt = state.Observation.Readiness.ObservedAt
	out.Quality = "unavailable"
	if known && state.Valid {
		out.Quality = "stale"
		if state.freshFor(n, at) {
			out.Quality = "fresh"
		}
	}
	return out
}

const loginRoutingFreshness = 15 * time.Second

// LoginRoutingAllows uses the shared background view, never a per-user NAS read.
// Cold, failed, stale or differently versioned observations cannot grant access.
func (s *RadiusService) LoginRoutingAllows(n NAS, username string, at time.Time) bool {
	if n.LoginDomain == "" {
		return true
	}
	if n.LoginRouting == nil || !n.Capabilities.Recorded() {
		return false
	}
	s.mu.Lock()
	state := s.loginRoutes[n.RouterID]
	s.mu.Unlock()
	r := state.Observation
	if !state.freshFor(n, at) {
		return false
	}
	return ReviewLoginRouting(r.Readiness, n, *n.LoginRouting, username, r.LegacyNames, r.LegacyComplete).ConfigurationMatches
}

// RefreshLoginRouting is called once per enrolled router by the independent
// routing loop. Failed reads retain the last view but immediately revoke its use.
func (s *RadiusService) RefreshLoginRouting(ctx context.Context, router string) error {
	legacy := false
	needed := false
	for _, n := range s.Clients {
		if n.RouterID == router && n.LoginDomain != "" {
			needed = true
			legacy = legacy || n.LoginRouting != nil && n.LoginRouting.LegacyDomain != ""
		}
	}
	if !needed {
		return nil
	}
	var observed LoginRoutingObservation
	err := errors.New("login routing metadata source unavailable")
	if s.ReadLoginRouting != nil {
		observed, err = s.ReadLoginRouting(ctx, router, legacy)
	}
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	if s.loginRoutes == nil {
		s.loginRoutes = map[string]loginRoutingState{}
	}
	state := s.loginRoutes[router]
	state.Valid = err == nil
	if err == nil {
		state.Observation = observed
	}
	s.loginRoutes[router] = state
	s.mu.Unlock()
	return err
}

// startLoginRouting owns its lifecycle separately from display collectors and
// the accounting/renewal loop. Slow configuration reads cannot delay enforcement.
func (s *RadiusService) startLoginRouting(ctx context.Context, report func(error)) func() {
	routers := map[string]bool{}
	for _, n := range s.Clients {
		if n.LoginDomain != "" {
			routers[n.RouterID] = true
		}
	}
	var group sync.WaitGroup
	for router := range routers {
		group.Add(1)
		go func(router string) {
			defer group.Done()
			refresh := func() {
				deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := s.RefreshLoginRouting(deadline, router); err != nil && ctx.Err() == nil {
					report(err)
				}
			}
			refresh()
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refresh()
				}
			}
		}(router)
	}
	return group.Wait
}
