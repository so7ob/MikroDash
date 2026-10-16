package activitywire

import (
	"context"
	"errors"
	"fmt"
	"mikrodash/internal/activity"
	"mikrodash/internal/db"
	"mikrodash/internal/routeros"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeArchive struct {
	failNextChunk bool
	files         map[string]string
	reads         int
	fail          bool
	rotate        bool
}

func (f *fakeArchive) Do(c routeros.Cmd) ([]routeros.Reply, error) {
	arg := func(key string) string {
		for _, a := range c.Args {
			if strings.HasPrefix(a, "="+key+"=") {
				return strings.TrimPrefix(a, "="+key+"=")
			}
		}
		return ""
	}
	switch c.Path {
	case "/file/print":
		rows := []routeros.Reply{}
		for name, b := range f.files {
			rows = append(rows, routeros.Reply{"name": name, "size": strconv.Itoa(len(b)), "last-modified": strconv.Itoa(len(b))})
		}
		return rows, nil
	case "/file/read":
		f.reads++
		if f.fail {
			return nil, errors.New("not enough permissions (9)")
		}
		b, ok := f.files[arg("file")]
		if !ok {
			return nil, errors.New("missing")
		}
		off, _ := strconv.Atoi(arg("offset"))
		n, _ := strconv.Atoi(arg("chunk-size"))
		if n > 32768 || n <= 0 {
			return nil, errors.New("bad chunk")
		}
		if off > len(b) {
			return nil, errors.New("rotated")
		}
		if f.failNextChunk && off > 0 && n > 1000 {
			return nil, errors.New("disconnected between chunks")
		}
		end := off + n
		if end > len(b) {
			end = len(b)
		}
		out := b[off:end]
		if f.rotate && off > 0 {
			f.files[arg("file")] = strings.ReplaceAll(b, "first.test", "other.test")
			f.rotate = false
		}
		return []routeros.Reply{{"data": out}}, nil
	}
	return nil, fmt.Errorf("unexpected command %s", c.Path)
}
func archiveLine(domain string) string {
	return time.Now().UTC().Format("2006-01-02 15:04:05") + ` script,info DNSUSER {"Connection":{"server":"hs-test","qtype":"A","match":"active-now"},"Destination":{"domain":"` + domain + `"},"Source":{"user":"synthetic","ip":"198.51.100.25","mac":"02:00:00:00:00:25"}}` + "\n"
}
func fixture(t *testing.T) (*Wire, *fakeArchive, *db.DB) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := activity.Settings{Enabled: true, Timezone: "UTC", Folder: activity.DefaultFolder}
	if err = d.SetActivitySettings("r1", cfg); err != nil {
		t.Fatal(err)
	}
	f := &fakeArchive{files: map[string]string{}}
	w := &Wire{db: d, cfg: map[string]activity.Settings{"r1": cfg}, status: map[string]activity.Status{}, source: func(string) Reader { return f }, stop: make(chan struct{})}
	t.Cleanup(func() { d.Close() })
	return w, f, d
}
func eventCount(t *testing.T, d *db.DB) int {
	t.Helper()
	count := 0
	err := d.WalkActivity(context.Background(), "r1", 0, time.Now().Add(time.Hour).UnixMilli(), func(activity.Event) error { count++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	return count
}
func TestIncrementalAppendRotationRestartAndPartialLines(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	line := archiveLine("first.test")
	f.files[name] = line + line
	w.Sync("r1")
	if count := eventCount(t, d); count != 2 {
		t.Fatal("identical independent source lines collapsed", count)
	}
	reads := f.reads
	w.Sync("r1")
	if f.reads != reads || eventCount(t, d) != 2 {
		t.Fatal("unchanged file was reread or duplicated")
	}
	more := archiveLine("next.test")
	f.files[name] += more[:len(more)-1]
	w.Sync("r1")
	if eventCount(t, d) != 2 {
		t.Fatal("partial final line committed")
	}
	f.files[name] += "\n"
	w.Sync("r1")
	if eventCount(t, d) != 3 {
		t.Fatal("completed partial line not resumed")
	}
	// Rotation changes filename, not content identity.
	f.files[activity.DefaultFolder+"/netlog.1.txt"] = f.files[name]
	f.files[name] = archiveLine("new-generation.test")
	w.Sync("r1")
	if eventCount(t, d) != 4 {
		t.Fatal("rotation double counted or skipped generation", eventCount(t, d))
	}
	// Fresh process state: only persisted checkpoints can prevent replay here.
	w2 := &Wire{db: d, cfg: w.cfg, status: map[string]activity.Status{}, source: w.source, stop: make(chan struct{})}
	w2.Sync("r1")
	if eventCount(t, d) != 4 {
		t.Fatal("restart duplicated history")
	}
}
func TestFailureLeavesCheckpointAndRecoversWithoutLoss(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	f.files[name] = archiveLine("first.test")
	f.fail = true
	w.Sync("r1")
	if eventCount(t, d) != 0 || !strings.Contains(w.Status("r1").Error, "permissions") {
		t.Fatal("permission failure hidden")
	}
	f.fail = false
	w.Sync("r1")
	if eventCount(t, d) != 1 {
		t.Fatal("failed pass advanced checkpoint")
	}
	cp, _ := d.ActivityCheckpoints("r1")
	if len(cp) != 1 || cp[0].Offset != int64(len(f.files[name])) {
		t.Fatal("checkpoint not committed with event")
	}
}
func TestOlderRecordsNotImportedAndDisabledRouterDoesNotRead(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	old := strings.Replace(archiveLine("old.test"), time.Now().UTC().Format("2006-01-02"), time.Now().Add(-31*24*time.Hour).UTC().Format("2006-01-02"), 1)
	f.files[name] = old + archiveLine("current.test")
	w.Sync("r1")
	if eventCount(t, d) != 1 {
		t.Fatal("retention import boundary")
	}
	reads := f.reads
	w.cfg["r1"] = activity.Settings{Enabled: false}
	w.Sync("r1")
	if f.reads != reads {
		t.Fatal("disabled router read files")
	}
}
func TestRotationDuringReadDoesNotAdvanceWrongGeneration(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	f.files[name] = strings.Repeat(archiveLine("first.test"), 200)
	f.rotate = true
	w.Sync("r1")
	if eventCount(t, d) != 0 {
		t.Fatal("mixed-generation chunk committed")
	}
	w.Sync("r1")
	if eventCount(t, d) != 200 {
		t.Fatal("rotated file did not recover", eventCount(t, d))
	}
}

func TestCheckpointAdvancesBeforeCompleteFile(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	f.files[name] = strings.Repeat(archiveLine("first.test"), 200)
	f.failNextChunk = true
	w.Sync("r1")
	cp, err := d.ActivityCheckpoints("r1")
	count := eventCount(t, d)
	if err != nil || len(cp) != 1 || cp[0].Offset <= 0 || cp[0].Offset >= int64(len(f.files[name])) || count <= 0 || count >= 200 {
		t.Fatalf("committed events lack an intermediate checkpoint: checkpoints=%v count=%d err=%v", cp, count, err)
	}
	f.failNextChunk = false
	w.Sync("r1")
	if eventCount(t, d) != 200 {
		t.Fatal("recovery duplicated the committed first chunk", eventCount(t, d))
	}
}
