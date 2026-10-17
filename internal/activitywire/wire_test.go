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
	bigOffsets    []int64
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
		if n > 1000 {
			f.bigOffsets = append(f.bigOffsets, int64(off))
		}
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

func TestParserUpgradeResumesEnrichmentWithoutRecountingOrResurrection(t *testing.T) {
	w, f, d := fixture(t)
	name := activity.DefaultFolder + "/netlog.0.txt"
	line := archiveLine("chatgpt.com")
	line = strings.Replace(line, `"domain":"chatgpt.com"`, `"domain":"chatgpt.com","canonical":"cdn.example","dns_ips":["198.51.100.7","2001:db8::7"],"ip_basis":"dns-cache"`, 1)
	line = strings.Replace(line, `"user":"synthetic"`, `"device_name":"Synthetic-device","user":"synthetic"`, 1)
	const size = 180
	body := strings.Repeat(line, size) + "malformed DNSUSER JSON\n"
	f.files[name] = body
	now := time.Now().UnixMilli()
	originalAt, originalReceived := now-5000, now-7200000
	events := []activity.Event{}
	stamp, topics, msg, ok := activity.SplitArchiveLine(strings.TrimSuffix(line, "\n"))
	if !ok {
		t.Fatal("synthetic line not parsed")
	}
	for i := 0; i < size; i++ {
		e, _, err := activity.Parse("r1", "", stamp, topics, msg, now, "UTC")
		if err != nil {
			t.Fatal(err)
		}
		e.SourceKey = hash([]byte("legacy:" + strconv.Itoa(i*len(line))))
		e.Canonical, e.DeviceName, e.IPBasis, e.DNSIPs = "", "", "", nil
		e.At, e.Received, e.TimeCertain = originalAt, originalReceived, false
		if i == size-1 {
			e.Received = now - int64(31*24*time.Hour/time.Millisecond)
		}
		events = append(events, e)
	}
	cp := activity.Checkpoint{Generation: "legacy", Name: name, Offset: int64(len(body)), PrefixLen: 512, PrefixHash: hash([]byte(body[:512])), TailHash: hash([]byte(body[len(body)-128:])), Size: int64(len(body)), Modified: strconv.Itoa(len(body))}
	if err := d.PersistActivity(events, map[string]activity.Status{"r1": {Invalid: 1}}, nil, map[string]activity.Checkpoint{"r1": cp}); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneActivity(now); err != nil {
		t.Fatal(err)
	}
	w.status["r1"] = activity.Status{Invalid: 1}
	w.verified = map[string]string{"r1\x00" + name: "legacy:" + strconv.Itoa(len(body)) + ":" + cp.Modified}
	f.failNextChunk = true
	w.Sync("r1")
	checkpoints, err := d.ActivityCheckpoints("r1")
	if err != nil || len(checkpoints) != 1 || checkpoints[0].Offset <= 0 || checkpoints[0].Offset >= int64(len(body)) || checkpoints[0].ParserVersion != activity.ParserVersion || checkpoints[0].ReplayUntil != int64(len(body)) || w.Status("r1").Error == "" {
		t.Fatal("upgrade progress did not persist before disconnect", checkpoints, err)
	}
	resume := checkpoints[0].Offset
	largeReads := len(f.bigOffsets)
	f.failNextChunk = false
	state, err := d.ActivityState("r1")
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Wire{db: d, cfg: w.cfg, status: map[string]activity.Status{"r1": {Invalid: state.Invalid}}, source: w.source, stop: make(chan struct{})}
	restarted.Sync("r1")
	if len(f.bigOffsets) <= largeReads || f.bigOffsets[largeReads] < resume {
		t.Fatal("upgrade restarted from zero", resume, f.bigOffsets[largeReads:])
	}
	count := 0
	if err = d.WalkActivity(context.Background(), "r1", 0, now+10000, func(e activity.Event) error {
		count++
		if e.At != originalAt || e.Received != originalReceived || e.TimeCertain || e.User != "synthetic" || e.Canonical != "cdn.example" || e.DeviceName != "Synthetic-device" || e.IPBasis != "dns-cache" || len(e.DNSIPs) != 2 {
			t.Fatal("metadata backfill rewrote history or lost fields", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != size-1 || restarted.Status("r1").Invalid != 1 {
		t.Fatal("backfill recounted data, invalid rows, or resurrected pruned rows", count, restarted.Status("r1"))
	}
	checkpoints, _ = d.ActivityCheckpoints("r1")
	if checkpoints[0].Generation != "legacy" || checkpoints[0].ParserVersion != activity.ParserVersion || checkpoints[0].ReplayUntil != 0 || checkpoints[0].Offset != int64(len(body)) {
		t.Fatal("upgrade not completed", checkpoints)
	}
	reads := f.reads
	restarted.Sync("r1")
	if f.reads != reads {
		t.Fatal("completed upgrade reread unchanged files")
	}
	f.files[name] += line
	restarted.Sync("r1")
	if eventCount(t, d) != size {
		t.Fatal("new independent identical line was not appended")
	}
}
