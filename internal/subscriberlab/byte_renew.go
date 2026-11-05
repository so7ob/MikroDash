package subscriberlab

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc3576"
	"layeh.com/radius/vendors/mikrotik"
	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

func (c *Client) byteRenewProbe(ctx context.Context, secret, session string, store *subscriber.Store, account string, record *recorder) error {
	if err := c.rateProbe(ctx, secret, session, 128000, 512000, record); err != nil {
		return err
	}
	if err := c.trafficProbe(ctx, record, 1<<20); err != nil {
		return err
	}
	read := func(step string) (routeros.Reply, error) {
		rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user,bytes-in,bytes-out,limit-bytes-in,limit-bytes-out,limit-bytes-total,uptime,session-time-left"}, Timeout: 3 * time.Second})
		record.emit(step, rows)
		if err != nil || len(rows) != 1 || rows[0]["user"] != "chr-lab" {
			return nil, errors.New("owned byte-renewal session unavailable")
		}
		return rows[0], nil
	}
	before, err := read("before-byte-coa")
	if err != nil {
		return err
	}
	down, err := strconv.ParseUint(before["bytes-out"], 10, 32)
	if err != nil || down < 1<<20 {
		return errors.New("large cumulative byte baseline not proven")
	}
	up, err := strconv.ParseUint(before["bytes-in"], 10, 32)
	if err != nil {
		return err
	}
	if down > math.MaxUint32-65536 || up > math.MaxUint32-65536 {
		return errors.New("byte-renewal fixture exceeds supported 32-bit probe ceiling")
	}
	ceilingDown, ceilingUp := uint32(down)+65536, uint32(up)+65536
	packet := func() *radius.Packet {
		p := radius.New(radius.CodeCoARequest, []byte(secret))
		rfc2865.UserName_SetString(p, "chr-lab")
		rfc2866.AcctSessionID_SetString(p, session)
		rfc2865.CallingStationID_SetString(p, "02:00:00:00:54:02")
		rfc2865.CalledStationID_SetString(p, "hs-lab")
		rfc2865.FramedIPAddress_Set(p, net.IPv4(198, 51, 100, 2))
		return p
	}
	exchange := func(p *radius.Packet, step string) (*radius.Packet, error) {
		if _, err := c.Identity(); err != nil {
			return nil, err
		}
		deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		out, err := (&radius.Client{MaxPacketErrors: 10}).Exchange(deadline, p, "127.0.0.1:13799")
		if err != nil {
			return nil, errors.New("byte CoA response unavailable")
		}
		record.emit(step, map[string]string{"code": out.Code.String(), "errorCause": fmt.Sprint(rfc3576.ErrorCause_Get(out))})
		return out, nil
	}
	p := packet()
	if err := mikrotik.MikrotikXmitLimit_Set(p, mikrotik.MikrotikXmitLimit(ceilingDown)); err != nil {
		return err
	}
	if err := mikrotik.MikrotikRecvLimit_Set(p, mikrotik.MikrotikRecvLimit(ceilingUp)); err != nil {
		return err
	}
	out, err := exchange(p, "directional-byte-coa")
	if err != nil || out.Code != radius.CodeCoAACK {
		return errors.New("directional byte renewal not acknowledged")
	}
	after, err := read("after-byte-coa")
	if err != nil {
		return err
	}
	if err := verifyByteReadback(before, after, ceilingDown, ceilingUp); err != nil {
		return errors.New("byte CoA reset counters, changed identity or failed exact ceiling readback")
	}
	// This optional, much larger total does not constrain the directional test.
	// Readback proves attribute application only, not multi-gigabyte enforcement.
	totalPacket := packet()
	if err := mikrotik.MikrotikTotalLimit_Set(totalPacket, 3<<30); err != nil {
		return err
	}
	totalReply, err := exchange(totalPacket, "total-byte-coa")
	if err != nil {
		return err
	}
	totalReadback := false
	if totalReply.Code == radius.CodeCoAACK {
		row, err := read("after-total-byte-coa")
		if err != nil {
			return err
		}
		if err := verifyByteReadback(after, row, ceilingDown, ceilingUp); err != nil {
			return err
		}
		totalReadback = row["limit-bytes-total"] == "3221225472"
	}
	measurement, err := measurePayload(ctx, false)
	if err != nil {
		return err
	}
	record.emit("renewed-byte-transfer", measurement.evidence())
	if measurement.Downloaded == "1048576" {
		return errors.New("renewed ceiling did not constrain the remaining byte allowance")
	}
	var final subscriber.Snapshot
	for attempt := 0; attempt < 40; attempt++ {
		final, err = store.Snapshot(ctx, account, time.Now())
		if err != nil {
			return err
		}
		if len(final.Sessions) == 1 && final.Sessions[0].State == "stopped" {
			break
		}
		if err = pause(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	record.emit("byte-renew-final", final)
	if len(final.Sessions) != 1 || final.Sessions[0].State != "stopped" || final.Sessions[0].SessionID != session || final.PendingAccounting != 0 || final.LifetimeSeconds >= 60 || final.LifetimeDownload < subscriber.Quantity(ceilingDown) || final.LifetimeDownload >= subscriber.Quantity(down+uint64(ceilingDown)) {
		return errors.New("total-mode byte semantics not distinguished from a new remaining grant")
	}
	record.emit("byte-renew-semantics", map[string]any{"byteMode": "total", "totalAttributeReadback": totalReadback, "downloadOverrunBytes": final.LifetimeDownload - subscriber.Quantity(ceilingDown)})
	return record.Err()
}

// TCP acknowledgements may arrive between API snapshots. Only a decrease or
// changed session identity invalidates cumulative-counter continuity.
func verifyByteReadback(before, after routeros.Reply, down, up uint32) error {
	if before[".id"] == "" || after[".id"] != before[".id"] || after["limit-bytes-in"] != strconv.FormatUint(uint64(up), 10) || after["limit-bytes-out"] != strconv.FormatUint(uint64(down), 10) {
		return errors.New("byte CoA changed identity or failed exact ceiling readback")
	}
	for _, key := range []string{"bytes-in", "bytes-out"} {
		initial, initialErr := strconv.ParseUint(before[key], 10, 64)
		actual, actualErr := strconv.ParseUint(after[key], 10, 64)
		if initialErr != nil || actualErr != nil || actual < initial {
			return errors.New("byte CoA counter continuity not proven")
		}
	}
	return nil
}
