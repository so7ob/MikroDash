package db

import (
	"context"
	"fmt"
	"mikrodash/internal/activity"
	"reflect"
	"testing"
	"time"
)

func TestActivityStagedSnapshotIsCompleteDuringImports(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UnixMilli() - 1000
	var events []activity.Event
	for i := 0; i < 4200; i++ {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - int64(i)*1000, Received: now, Kind: "dns", Domain: fmt.Sprintf("d%d.example", i), User: "اسم محفوظ", Client: "synthetic", DNSIPs: []string{"2001:db8::8"}, TimeCertain: true})
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	f := activity.Filter{From: now - 86400000, To: now + 1, View: "summary", Group: "domain", Parts: "overview", Page: 1, Limit: 10, Desc: true}
	want, err := activity.Analyze(f, nil, "UTC", func(from, to int64, fn func(activity.Event) error) error {
		return d.WalkActivity(t.Context(), "r1", from, to, fn)
	})
	if err != nil {
		t.Fatal(err)
	}
	imported := false
	stages := map[int]bool{}
	var got activity.Result
	for attempt := 0; attempt < 30; attempt++ {
		got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Preparing {
			break
		}
		if got.Totals.DNS != 0 || len(got.Rows) != 0 || len(got.Series) != 0 || len(got.Events) != 0 || len(got.Categories) != 0 {
			t.Fatal("partial analytical output disclosed")
		}
		if d.activityJob != nil {
			if stages[d.activityJob.stage] {
				t.Fatal("stage did not advance")
			}
			stages[d.activityJob.stage] = true
			if !imported {
				// Ingestion must be able to acquire the DB between stages.
				e := activity.Event{RouterID: "r1", SourceKey: "late", At: now - 1, Received: now, Kind: "dns", Domain: "late.example", Client: "new", DNSIPs: []string{"198.51.100.9"}}
				if err = d.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				imported = true
			}
		}
	}
	if got.Preparing || !imported || len(stages) != 7 || d.activityJob != nil {
		t.Fatal("staged query did not complete", len(stages), got.Preparing)
	}
	if got.TotalStored != 4200 || got.DataRevision == 0 || got.ComputedAt == 0 {
		t.Fatal("snapshot metadata changed", got.TotalStored, got.DataRevision)
	}
	got.TotalStored, got.DataRevision, got.ComputedAt, got.RefreshAt, got.ResultVersion = 0, 0, 0, 0, ""
	if !reflect.DeepEqual(want, got) {
		t.Fatal("snapshot differs from original source", want.Totals, got.Totals, len(want.Series), len(got.Series))
	}
	// The next query must include the arrival, rather than reuse an old result
	// stored under its newer revision.
	for attempt := 0; attempt < 10; attempt++ {
		got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Preparing {
			break
		}
	}
	if got.Preparing || got.TotalStored != 4201 || got.Totals.DNS != 4201 {
		t.Fatal("new revision reused old snapshot", got.Preparing, got.TotalStored, got.Totals.DNS)
	}
	// Cancel a real retained stage; no cancelled result/workspace is cached.
	f.Sort = "label"
	got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
	if err != nil || !got.Preparing || d.activityJob == nil {
		t.Fatal("cancellation fixture not staged", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err = d.ActivityIndexedQuery(ctx, "r1", f, nil, "UTC", "", nil); err == nil {
		t.Fatal("caller cancellation hidden")
	}
	f.Search = "__missing_109__"
	got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
	if err != nil || got.Preparing || got.Totals.DNS != 0 || d.activityJob != nil {
		t.Fatal("new filter reused retained work", err)
	}
	f.Search = ""
	got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
	if err != nil || !got.Preparing || d.activityJob == nil {
		t.Fatal("retention fixture not staged", err)
	}
	if err = d.PruneActivity(now + int64(activity.Retention/time.Millisecond) + 1); err != nil {
		t.Fatal(err)
	}
	got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
	if err != nil || !got.Preparing || d.activityJob != nil || got.Totals.DNS != 0 {
		t.Fatal("retention exposed an incomplete snapshot", err)
	}
	got, _, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "", nil)
	if err != nil || got.Preparing || got.Totals.DNS != 0 || got.TotalStored != 0 {
		t.Fatal("retention did not restart cleanly", err)
	}
}
