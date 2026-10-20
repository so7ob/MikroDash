package subscriberlab

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc3576"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

func (c *Client) disconnectProbe(ctx context.Context, service *subscriber.RadiusService, store *subscriber.Store, account string, record *recorder) error {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,server,user,mac-address,address,uptime"}, Timeout: 3 * time.Second})
	if err != nil || len(rows) != 1 {
		return errors.New("unique live session required for Disconnect probe")
	}
	at := time.Now()
	row := rows[0]
	view := subscriber.RouterObservation{RouterID: "chr-lab", Version: "7.24.5 (stable)", At: at, Complete: true,
		Sessions: []subscriber.ObservedSession{{ID: row[".id"], Server: row["server"], Username: row["user"], Device: row["mac-address"], Address: row["address"], Seconds: subscriber.Quantity(routeros.DurationSeconds(row["uptime"]))}}}
	if err := store.ObserveRouter(ctx, view, service.Clients); err != nil {
		return err
	}
	service.VerifySession = func(ctx context.Context, e subscriber.Enforcement) bool {
		return store.VerifyObservedSession(ctx, e, "hs-lab", time.Now())
	}
	before, err := store.Snapshot(ctx, account, at)
	if err != nil || len(before.Sessions) != 1 {
		return errors.New("owned ledger lease required for Disconnect probe")
	}
	lease := before.Sessions[0]
	e := subscriber.Enforcement{AccountID: account, LeaseID: lease.ID, RouterID: "chr-lab", Network: "C", Device: lease.Device, SessionID: lease.SessionID, Reason: "isolated real CHR Disconnect acceptance"}
	started := time.Now()
	if err = service.Disconnect(ctx, e, true); err != nil {
		record.emit("production-disconnect", map[string]string{"state": "failed", "error": err.Error()})
		// Keep production failure as failure; this extra packet diagnoses only
		// whether the API-confirmed IPv4 is also required for Disconnect.
		p := radius.New(radius.CodeDisconnectRequest, []byte(service.Clients[0].Secret))
		for _, setErr := range []error{rfc2865.UserName_SetString(p, row["user"]), rfc2865.CallingStationID_SetString(p, row["mac-address"]), rfc2866.AcctSessionID_SetString(p, lease.SessionID), rfc2865.FramedIPAddress_Set(p, net.ParseIP(row["address"]))} {
			if setErr != nil {
				return setErr
			}
		}
		if _, proofErr := c.Identity(); proofErr != nil {
			return proofErr
		}
		deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
		reply, probeErr := (&radius.Client{MaxPacketErrors: 10}).Exchange(deadline, p, "127.0.0.1:13799")
		cancel()
		if probeErr == nil {
			record.emit("diagnostic-disconnect-with-ip", map[string]string{"code": reply.Code.String(), "errorCause": fmt.Sprint(rfc3576.ErrorCause_Get(reply))})
		}
		return err
	}
	for attempt := 0; attempt < 40; attempt++ {
		final, err := store.Snapshot(ctx, account, time.Now())
		if err != nil {
			return err
		}
		if len(final.Sessions) == 1 && final.Sessions[0].State == "stopped" {
			active, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user"}, Timeout: 3 * time.Second})
			if err != nil || len(active) != 0 {
				return errors.New("Disconnect ACK did not verify disappearance")
			}
			record.emit("production-disconnect", map[string]any{"state": "verified", "elapsed": time.Since(started).String(), "final": final})
			return record.Err()
		}
		if err = pause(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("Disconnect ACK lacked covering Stop")
}
