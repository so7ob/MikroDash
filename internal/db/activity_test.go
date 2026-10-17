package db

import (
	"context"
	"mikrodash/internal/activity"
	"testing"
	"time"
)

func TestActivityTransactionReplayRetentionAndIsolation(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.ActivityState("r1")
	if err != nil || s.Enabled || s.Folder != activity.DefaultFolder {
		t.Fatal("recording not off by default")
	}
	now := time.Now().UnixMilli()
	old := now - int64(31*24*time.Hour/time.Millisecond)
	events := []activity.Event{{RouterID: "r1", SourceKey: "one", At: now, Received: now, Kind: "dns", Domain: "one.test"}, {RouterID: "r1", SourceKey: "two", At: old, Received: old, Kind: "dns", Domain: "old.test"}, {RouterID: "r2", SourceKey: "one", At: now, Received: now, Kind: "dns", Domain: "two.test"}}
	cp := activity.Checkpoint{Generation: "generation", Offset: 1024}
	for i := 0; i < 2; i++ {
		if err = d.PersistActivity(events, nil, nil, map[string]activity.Checkpoint{"r1": cp}); err != nil {
			t.Fatal(err)
		}
	}
	counts := func(router string) int {
		n := 0
		if err := d.WalkActivity(context.Background(), router, 0, now+1000, func(activity.Event) error { n++; return nil }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if counts("r1") != 2 || counts("r2") != 1 {
		t.Fatal("replay or router isolation")
	}
	c, err := d.ActivityCheckpoints("r1")
	if err != nil || len(c) != 1 || c[0].Offset != 1024 {
		t.Fatal("checkpoint persistence")
	}
	if err = d.PruneActivity(now); err != nil || counts("r1") != 1 || counts("r2") != 1 {
		t.Fatal("retention deleted current data")
	}
	r := activity.Rule{Domain: "one.test", Service: "Synthetic", Category: "Testing", Custom: true}
	if err = d.SetActivityRule("r1", r); err != nil {
		t.Fatal(err)
	}
	rules, _ := d.ActivityRules("r2")
	if len(rules) != 0 {
		t.Fatal("rules crossed routers")
	}
}

func TestActivityCheckpointFailureRollsBackEventsAndCounters(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.SetActivitySettings("r1", activity.Settings{Enabled: true, Timezone: "UTC", Folder: activity.DefaultFolder}); err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`CREATE TRIGGER fail_activity_checkpoint BEFORE INSERT ON activity_checkpoints BEGIN SELECT RAISE(ABORT,'synthetic checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	e := activity.Event{RouterID: "r1", SourceKey: "one", At: now, Received: now, Kind: "dns"}
	err = d.PersistActivity([]activity.Event{e}, map[string]activity.Status{"r1": {Invalid: 3, LastPersisted: now}}, nil, map[string]activity.Checkpoint{"r1": {Generation: "one", Offset: 100}})
	if err == nil {
		t.Fatal("checkpoint failure was swallowed")
	}
	count := 0
	if err = d.WalkActivity(context.Background(), "r1", 0, now+1000, func(activity.Event) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	state, err := d.ActivityState("r1")
	if err != nil || count != 0 || state.Invalid != 0 || state.LastPersisted != 0 {
		t.Fatal("non-atomic archive commit", count, state, err)
	}
}
