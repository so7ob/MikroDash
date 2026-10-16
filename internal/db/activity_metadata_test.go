package db

import (
	"context"
	"mikrodash/internal/activity"
	"testing"
	"time"
)

func TestActivityMetadataGroupingAndReplayOnlyEnrichment(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UnixMilli()
	base := activity.Event{RouterID: "r1", At: now - 1000, Received: now - 2000, Kind: "dns", Domain: "chatgpt.com", User: "Historical user", Client: "historical", DNSIPs: []string{}}
	events := []activity.Event{}
	for i, key := range []string{"base", "repeat", "canonical", "device", "basis", "answers"} {
		e := base
		e.SourceKey = key
		switch i {
		case 2:
			e.Canonical = "cdn.example"
		case 3:
			e.DeviceName = "جهاز تجريبي"
		case 4:
			e.IPBasis = "dns-cache"
		case 5:
			e.DNSIPs = []string{"198.51.100.7", "2001:db8::7"}
		}
		events = append(events, e)
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Missing fields from an older installation group like explicitly empty fields.
	if _, err = d.sql.Exec(`UPDATE activity_events SET payload=json_remove(payload,'$.canonical','$.dnsIPs','$.deviceName','$.ipBasis') WHERE source_key='base'`); err != nil {
		t.Fatal(err)
	}
	f := activity.Filter{From: now - 10000, To: now + 1000, View: "events", Page: 1, Limit: 50, Sort: "count", Desc: true}
	rows, total, err := d.ActivityEventPage(context.Background(), "r1", f, nil)
	if err != nil || total != 5 || len(rows) != 5 || rows[0].Count != 2 {
		t.Fatal("different displayed metadata collapsed", total, rows, err)
	}
	for _, e := range rows {
		if e.DNSIPs == nil || e.DNSAnswers == nil {
			t.Fatal("legacy answer list sent null")
		}
	}
	f.DNSIP = "2001:db8::7"
	rows, total, err = d.ActivityEventPage(context.Background(), "r1", f, nil)
	if err != nil || total != 1 || rows[0].SourceKey != "answers" {
		t.Fatal("answer filter ignored", rows, total, err)
	}
	// Country selection uses derived answers, not the destination column or stale stored metadata.
	f.DNSIP = ""
	f.DNSCountry = "DE"
	rows, total, err = d.ActivityEventPage(context.Background(), "r1", f, nil, func(ip string) (string, string) {
		if ip == "2001:db8::7" {
			return "DE", "Synthetic city"
		}
		return "", ""
	})
	if err != nil || total != 1 || len(rows[0].DNSAnswers) != 2 || rows[0].Country != "Unknown" || rows[0].DNSAnswers[1].Country != "DE" {
		t.Fatal("event page ignored DNS geography", rows, total, err)
	}
	// Replays may fill metadata only, never replace stored historical identity/time.
	replay := activity.Event{RouterID: "r1", SourceKey: "base", Replay: true, At: now, Received: now, Kind: "dns", User: "Wrong new user", Client: "wrong", Canonical: "recovered.example", DeviceName: "Recovered device", DNSIPs: []string{"2001:db8::8"}, IPBasis: "dns-cache"}
	missing := replay
	missing.SourceKey = "pruned-source"
	other := replay
	other.RouterID = "r2"
	if err = d.PersistActivity([]activity.Event{replay, missing, other}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err = d.WalkActivity(context.Background(), "r1", 0, now+1000, func(e activity.Event) error {
		count++
		if e.SourceKey == "base" && (e.At != base.At || e.Received != base.Received || e.User != base.User || e.Client != base.Client || e.Canonical != "recovered.example" || len(e.DNSIPs) != 1) {
			t.Fatal("replay overwrote history or lost metadata", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatal("replay resurrected or recounted a source", count)
	}
	count = 0
	_ = d.WalkActivity(context.Background(), "r2", 0, now+1000, func(activity.Event) error { count++; return nil })
	if count != 0 {
		t.Fatal("replay crossed router scope")
	}
	// Metadata and the parser cursor must roll back together on a failed commit.
	if _, err = d.sql.Exec(`CREATE TRIGGER fail_metadata_cursor BEFORE INSERT ON activity_checkpoints BEGIN SELECT RAISE(ABORT,'synthetic cursor failure'); END`); err != nil {
		t.Fatal(err)
	}
	replay.DeviceName = "Must not commit"
	if err = d.PersistActivity([]activity.Event{replay}, nil, nil, map[string]activity.Checkpoint{"r1": {Generation: "failed"}}); err == nil {
		t.Fatal("cursor failure was ignored")
	}
	if err = d.WalkActivity(context.Background(), "r1", 0, now+1000, func(e activity.Event) error {
		if e.SourceKey == "base" && e.DeviceName != "Recovered device" {
			t.Fatal("metadata escaped a rolled back cursor", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

}
