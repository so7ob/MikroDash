package subscriberlab

import (
	"context"
	"errors"
	"time"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

func (c *Client) localByteProbe(ctx context.Context, dir, password string, https bool, scenario string, store *subscriber.Store, account string, record *recorder) error {
	measurement, err := measurePayload(ctx, scenario == "upload-limit")
	if err != nil {
		return err
	}
	evidence := measurement.evidence()
	evidence["budgetBytes"] = "65536"
	record.emit(scenario+"-transfer", evidence)
	if scenario == "download-limit" && measurement.Downloaded == "1048576" || scenario == "upload-limit" && measurement.Received == 1<<20 {
		return errors.New("entire payload crossed the local byte budget; enforcement acceptance failed")
	}
	var snapshot subscriber.Snapshot
	for attempt := 0; attempt < 40; attempt++ {
		snapshot, err = store.Snapshot(ctx, account, time.Now())
		if err != nil {
			return err
		}
		if len(snapshot.Sessions) == 1 && snapshot.Sessions[0].State == "stopped" {
			break
		}
		if err = pause(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	record.emit(scenario+"-ledger", snapshot)
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].State != "stopped" || snapshot.PendingAccounting != 0 || snapshot.LifetimeSeconds >= 60 {
		return errors.New("early local byte-cap Stop was not durably confirmed")
	}
	used := snapshot.LifetimeDownload
	if scenario == "upload-limit" {
		used = snapshot.LifetimeUpload
	}
	if used < 65536 || snapshot.Decision.Allowed {
		return errors.New("exhausted byte balance did not deny admission")
	}
	record.emit(scenario+"-overrun", map[string]subscriber.Quantity{"confirmedBytes": used, "overrunBytes": used - 65536})
	if err := c.login(ctx, dir, password, https); err != nil {
		return err
	}
	if err := pause(ctx, time.Second); err != nil {
		return err
	}
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user"}, Timeout: 3 * time.Second})
	if err != nil || len(rows) != 0 {
		return errors.New("exhausted subscriber regained a HotSpot session")
	}
	return record.Err()
}
