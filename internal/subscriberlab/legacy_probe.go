package subscriberlab

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"time"

	"mikrodash/internal/routeros"
)

// RadiusSelectionProbe measures the appliance's ordered NAS selection. The
// normal MikroDash primary must remain running; this is not User Manager proof.
func (c *Client) RadiusSelectionProbe(ctx context.Context, privateDir string, out io.Writer, injectFailure bool) (retErr error) {
	if _, err := c.Identity(); err != nil {
		return err
	}
	active := func() ([]routeros.Reply, error) {
		return c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user,server,mac-address,address,uptime,session-time-left"}, Timeout: 3 * time.Second})
	}
	rows, err := active()
	if err != nil || len(rows) != 0 {
		return errors.New("empty owned HotSpot required for selection probe")
	}
	entries, err := c.Do(routeros.Cmd{Path: "/radius/print", Args: []string{"=.proplist=.id,comment,disabled,address,authentication-port,accounting-port,service"}, Timeout: 3 * time.Second})
	if err != nil {
		return err
	}
	primary, err := selectionPrimary(entries)
	if err != nil {
		return err
	}
	var fixture FixtureCredentials
	if err := privateJSON(filepath.Join(privateDir, "fixture.json"), &fixture); err != nil {
		return err
	}
	ledger, account, requests, closeAuthority, err := legacyAuthority(ctx, privateDir, fixture)
	if err != nil {
		return err
	}
	defer closeAuthority()
	if err = c.ensure("/radius", "comment", "md-lab-legacy-secondary", map[string]string{"address": "10.0.2.2", "authentication-port": "12814", "accounting-port": "12815", "service": "hotspot", "secret": fixture.Secret, "disabled": "no"}); err != nil {
		return err
	}
	secondary, err := c.Do(routeros.Cmd{Path: "/radius/print", Args: []string{"?comment=md-lab-legacy-secondary", "=.proplist=.id"}, Timeout: 3 * time.Second})
	if err != nil || len(secondary) != 1 || secondary[0][".id"] == "" {
		return errors.New("secondary RADIUS identity unavailable; inspect owned lab")
	}
	secondaryID := secondary[0][".id"]
	record := &recorder{w: out}
	primaryDisabled := false
	defer func() {
		// Keep accounting alive until the owned legacy client has logged out.
		retErr = errors.Join(retErr, c.logoutClient(context.Background(), privateDir, "legacy-lab", true))
		if primaryDisabled {
			_, restoreErr := c.Do(routeros.Cmd{Path: "/radius/set", Args: []string{"=.id=" + primary, "=disabled=no"}, Timeout: 3 * time.Second})
			retErr = errors.Join(retErr, restoreErr)
		}
		_, removeErr := c.Do(routeros.Cmd{Path: "/radius/remove", Args: []string{"=.id=" + secondaryID}, Timeout: 3 * time.Second})
		retErr = errors.Join(retErr, removeErr)
		current, readErr := c.Do(routeros.Cmd{Path: "/radius/print", Args: []string{"=.proplist=.id,comment,disabled,address,authentication-port,accounting-port,service"}, Timeout: 3 * time.Second})
		actual, proofErr := selectionPrimary(current)
		verified := readErr == nil && proofErr == nil && actual == primary
		if !verified {
			retErr = errors.Join(retErr, errors.New("selection rollback was not verified by fresh RADIUS configuration"))
		}
		record.emit("selection-rollback", map[string]bool{"verified": verified, "primaryEnabled": verified, "secondaryRemoved": verified})
		retErr = errors.Join(retErr, record.Err())
	}()
	refused := func(step string, expected int64) error {
		if err := c.loginAs(ctx, privateDir, "legacy-lab", fixture.Password, true); err != nil {
			return err
		}
		if err := pause(ctx, time.Second); err != nil {
			return err
		}
		rows, err := active()
		count := requests.Load()
		record.emit(step, map[string]any{"secondaryRequests": count, "activeSessions": len(rows)})
		if err != nil || len(rows) != 0 || count != expected {
			return errors.New("primary rejection unexpectedly selected the secondary authority")
		}
		return nil
	}
	if err = refused("primary-reject-no-fallback", 0); err != nil {
		return err
	}
	if _, err = c.Do(routeros.Cmd{Path: "/radius/set", Args: []string{"=.id=" + primary, "=disabled=yes"}, Timeout: 3 * time.Second}); err != nil {
		return err
	}
	primaryDisabled = true
	if injectFailure {
		return errors.New("synthetic injected failure after primary disable")
	}
	if err = c.loginAs(ctx, privateDir, "legacy-lab", fixture.Password, true); err != nil {
		return err
	}
	rows, err = active()
	if err != nil || len(rows) != 1 || rows[0]["user"] != "legacy-lab" || rows[0]["server"] != "hs-lab" || rows[0]["mac-address"] != "02:00:00:00:54:02" || requests.Load() == 0 {
		return errors.New("secondary authority did not admit the actual legacy client")
	}
	if err = verifyTotalTimeout(rows[0], 10); err != nil {
		return err
	}
	record.emit("secondary-when-primary-disabled", rows)
	if err = c.logoutClient(ctx, privateDir, "legacy-lab", true); err != nil {
		return err
	}
	final, err := ledger.Snapshot(ctx, account, time.Now())
	if err != nil || len(final.Sessions) != 1 || final.Sessions[0].State != "stopped" || final.PendingAccounting != 0 {
		return errors.New("secondary accounting Stop not durably settled")
	}
	record.emit("secondary-durable-stop", final)
	if _, err = c.Do(routeros.Cmd{Path: "/radius/set", Args: []string{"=.id=" + primary, "=disabled=no"}, Timeout: 3 * time.Second}); err != nil {
		return err
	}
	primaryDisabled = false
	return refused("primary-restored-reject", requests.Load())
}

func selectionPrimary(entries []routeros.Reply) (string, error) {
	if len(entries) != 1 || entries[0]["comment"] != "md-lab-radius" || entries[0][".id"] == "" || entries[0]["disabled"] != "false" || entries[0]["authentication-port"] != "12812" || entries[0]["accounting-port"] != "12813" || entries[0]["address"] != "10.0.2.2" || entries[0]["service"] != "hotspot" {
		return "", errors.New("sole enabled synthetic primary RADIUS entry required")
	}
	return entries[0][".id"], nil
}
