package subscriberlab

import (
	"context"
	"errors"
	"io"
	"time"

	"mikrodash/internal/routeros"
)

func (c *Client) RuntimeStatus() ([]routeros.Reply, error) {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user,server,mac-address,address,uptime,session-time-left,bytes-in,bytes-out,limit-bytes-in,limit-bytes-out"}, Timeout: 3 * time.Second})
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		if _, err = ownedRuntimeClient(rows); err != nil {
			return nil, err
		}
	}
	if rows == nil {
		rows = []routeros.Reply{}
	}
	return rows, nil
}

// LogoutRuntime sends HTTPS logout from the owned client while its real
// accounting authority is still available. It never removes a different user.
func (c *Client) LogoutRuntime(ctx context.Context, privateDir string) error {
	return c.clearPreviousProbe(ctx, privateDir, true)
}

func ownedRuntimeClient(rows []routeros.Reply) (routeros.Reply, error) {
	if len(rows) != 1 || rows[0][".id"] == "" || rows[0]["user"] != "chr-lab" || rows[0]["server"] != "hs-lab" || rows[0]["mac-address"] != "02:00:00:00:54:02" || rows[0]["address"] != "198.51.100.2" {
		return nil, errors.New("exact owned runtime client required")
	}
	return rows[0], nil
}

// RuntimeExhaustProbe consumes only an explicitly prepared 64 KiB lab grant.
// The normal MikroDash runtime remains the sole accounting authority.
func (c *Client) RuntimeExhaustProbe(ctx context.Context, out io.Writer) error {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user,server,mac-address,address,limit-bytes-out"}, Timeout: 3 * time.Second})
	if err != nil {
		return err
	}
	row, err := ownedRuntimeClient(rows)
	if err != nil || row["limit-bytes-out"] != "65536" {
		return errors.New("explicit synthetic 65536-byte local grant required")
	}
	record := &recorder{w: out}
	measurement, err := measurePayload(ctx, false)
	if err != nil {
		return err
	}
	record.emit("runtime-exhaust-transfer", measurement.evidence())
	if measurement.Downloaded == "1048576" {
		return errors.New("hard byte cap did not interrupt the attempted payload")
	}
	for attempt := 0; attempt < 25; attempt++ {
		rows, err = c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user"}, Timeout: 3 * time.Second})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			record.emit("runtime-exhaust-api", map[string]string{"state": "absent", "localLimitBytes": "65536"})
			return record.Err()
		}
		if err = pause(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("hard byte cap did not prove session disappearance")
}
