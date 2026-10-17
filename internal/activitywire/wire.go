// Package activitywire incrementally synchronizes rotating RouterOS log files.
// Only reads are issued; file offsets and parsed events commit atomically.
package activitywire

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mikrodash/internal/activity"
	"mikrodash/internal/db"
	"mikrodash/internal/routeros"
)

const chunkSize = 32768
const maxCycleBytes = 2 << 20

type Reader interface {
	Do(routeros.Cmd) ([]routeros.Reply, error)
}
type file struct {
	Name, Modified string
	Size           int64
}
type Wire struct {
	verified map[string]string
	runs     map[string]activity.Coverage
	mu       sync.Mutex
	db       *db.DB
	cfg      map[string]activity.Settings
	status   map[string]activity.Status
	source   func(string) Reader
	changed  func(string)
	stop     chan struct{}
	done     chan struct{}
	wake     chan struct{}
	once     sync.Once
}

func New(d *db.DB, source func(string) Reader, changed func(string)) (*Wire, error) {
	if d == nil {
		return nil, nil
	}
	cfg, err := d.ActivityEnabled()
	if err != nil {
		return nil, err
	}
	w := &Wire{db: d, cfg: cfg, status: map[string]activity.Status{}, source: source, changed: changed, stop: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1)}
	for id, c := range cfg {
		s, err := d.ActivityState(id)
		if err != nil {
			return nil, err
		}
		w.status[id] = activity.Status{Enabled: c.Enabled, Invalid: s.Invalid, Dropped: s.Dropped, LastReceived: s.LastReceived, LastPersisted: s.LastPersisted}
	}
	go w.loop()
	return w, nil
}
func (w *Wire) Enabled(id string) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg[id].Enabled
}
func (w *Wire) Configure(id string, c activity.Settings) {
	if w == nil {
		return
	}
	stored, _ := w.db.ActivityState(id)
	w.mu.Lock()
	w.cfg[id] = c
	s := w.status[id]
	if s.LastPersisted == 0 {
		s.Invalid = stored.Invalid
		s.Dropped = stored.Dropped
		s.LastReceived = stored.LastReceived
		s.LastPersisted = stored.LastPersisted
	}
	s.Enabled = c.Enabled
	w.status[id] = s
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (w *Wire) Status(id string) activity.Status {
	if w == nil {
		return activity.Status{Error: "Activity storage unavailable"}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status[id]
}
func (w *Wire) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.stop); <-w.done })
}
func (w *Wire) stopping() bool {
	select {
	case <-w.stop:
		return true
	default:
		return false
	}
}
func (w *Wire) loop() {
	defer close(w.done)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	lastPrune := int64(0)
	for {
		w.mu.Lock()
		ids := []string{}
		for id, c := range w.cfg {
			if c.Enabled {
				ids = append(ids, id)
			}
		}
		w.mu.Unlock()
		sort.Strings(ids)
		for _, id := range ids {
			if w.stopping() {
				return
			}
			w.Sync(id)
		}
		if now := time.Now().UnixMilli(); now-lastPrune > int64(time.Hour/time.Millisecond) {
			if err := w.db.PruneActivity(now); err == nil {
				lastPrune = now
			}
		}
		select {
		case <-w.stop:
			return
		case <-tick.C:
		case <-w.wake:
		}
	}
}
func hash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func read(r Reader, name string, offset int64, n int) ([]byte, error) {
	if n <= 0 {
		return []byte{}, nil
	}
	if n > chunkSize {
		n = chunkSize
	}
	rows, err := r.Do(routeros.Cmd{Path: "/file/read", Args: []string{"=file=" + name, "=offset=" + strconv.FormatInt(offset, 10), "=chunk-size=" + strconv.Itoa(n)}, Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	var data []byte
	for _, row := range rows {
		data = append(data, []byte(row["data"])...)
	}
	if len(data) > n {
		return nil, errors.New("invalid file chunk")
	}
	return data, nil
}
func list(r Reader, folder string) ([]file, error) {
	rows, err := r.Do(routeros.Cmd{Path: "/file/print", Args: []string{"=.proplist=name,size,type,last-modified,creation-time"}, Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	out := []file{}
	for _, row := range rows {
		n := row["name"]
		if path.Dir(n) != folder || !strings.HasSuffix(strings.ToLower(n), ".txt") {
			continue
		}
		size, err := strconv.ParseInt(row["size"], 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("invalid archive size")
		}
		mod := row["last-modified"]
		if mod == "" {
			mod = row["creation-time"]
		}
		out = append(out, file{Name: n, Size: size, Modified: mod})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Modified != out[j].Modified {
			return out[i].Modified < out[j].Modified
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Sync is serialized by the worker. Exported for replay tests against a fake
// RouterOS connection, including crash/restart and rotation during reads.
func (w *Wire) Sync(id string) {
	w.mu.Lock()
	cfg := w.cfg[id]
	s := w.status[id]
	if !cfg.Enabled {
		w.mu.Unlock()
		return
	}
	s.Enabled = cfg.Enabled
	s.Syncing = true
	w.status[id] = s
	w.mu.Unlock()
	var syncErr error
	defer func() {
		w.mu.Lock()
		s := w.status[id]
		s.Syncing = false
		s.Enabled = w.cfg[id].Enabled
		if syncErr != nil {
			s.Listening = false
			s.Error = "Archive synchronization failed; the next pass will resume from its checkpoint"
			if strings.Contains(syncErr.Error(), "not enough permissions") {
				s.Error = "RouterOS denied file reads for the API account; check its file-read permissions"
			}
		} else {
			s.LastSync = time.Now().UnixMilli()
			s.Listening = s.Enabled
			s.Error = ""
			if s.Enabled {
				if w.runs == nil {
					w.runs = map[string]activity.Coverage{}
				}
				run := w.runs[id]
				if run.Start == 0 || s.LastSync-run.End > 15000 {
					run.Start = s.LastSync
				}
				run.End = s.LastSync
				w.runs[id] = run
				if err := w.db.PersistActivity(nil, nil, map[string]activity.Coverage{id: run}, nil); err != nil {
					s.Error = "Activity coverage storage failed"
				}
			}
		}
		w.status[id] = s
		w.mu.Unlock()
		if w.changed != nil {
			w.changed(id)
		}
	}()
	if w.source == nil {
		syncErr = errors.New("no source")
		return
	}
	r := w.source(id)
	if r == nil {
		syncErr = errors.New("disconnected")
		return
	}
	if cfg.Folder == "" {
		cfg.Folder = activity.DefaultFolder
	}
	files, err := list(r, cfg.Folder)
	if err != nil {
		syncErr = err
		return
	}
	if len(files) == 0 {
		syncErr = errors.New("no archive files")
		return
	}
	w.mu.Lock()
	s = w.status[id]
	s.Files = len(files)
	w.status[id] = s
	w.mu.Unlock()
	checkpoints, err := w.db.ActivityCheckpoints(id)
	if err != nil {
		syncErr = err
		return
	}
	budget := maxCycleBytes
	for _, f := range files {
		if w.stopping() || !w.Enabled(id) {
			return
		}
		if budget <= 0 {
			return
		}
		if f.Size == 0 {
			continue
		}
		unchanged := false
		manifestKey := id + "\x00" + f.Name
		w.mu.Lock()
		if w.verified == nil {
			w.verified = map[string]string{}
		}
		verified := w.verified[manifestKey]
		w.mu.Unlock()
		for _, c := range checkpoints {
			if c.ParserVersion == activity.ParserVersion && c.Name == f.Name && c.Size == f.Size && c.Modified == f.Modified && c.Offset == f.Size && verified == c.Generation+":"+strconv.FormatInt(f.Size, 10)+":"+f.Modified {
				unchanged = true
				break
			}
		}
		if unchanged {
			continue
		}
		n := 512
		if f.Size < int64(n) {
			n = int(f.Size)
		}
		head, err := read(r, f.Name, 0, n)
		if err != nil || len(head) != n {
			syncErr = err
			if syncErr == nil {
				syncErr = errors.New("head unavailable")
			}
			return
		}
		budget -= len(head)
		var cp activity.Checkpoint
		for _, candidate := range checkpoints {
			if candidate.PrefixLen > len(head) || candidate.Offset > f.Size || hash(head[:candidate.PrefixLen]) != candidate.PrefixHash {
				continue
			}
			tailStart := candidate.Offset - 128
			if tailStart < 0 {
				tailStart = 0
			}
			tail, err := read(r, f.Name, tailStart, int(candidate.Offset-tailStart))
			if err != nil {
				syncErr = err
				return
			}
			budget -= len(tail)
			if hash(tail) == candidate.TailHash && candidate.Offset >= cp.Offset {
				cp = candidate
			}
		}
		if cp.Generation == "" {
			nonce := make([]byte, 16)
			if _, err = rand.Read(nonce); err != nil {
				syncErr = err
				return
			}
			cp = activity.Checkpoint{ParserVersion: activity.ParserVersion, Generation: hex.EncodeToString(nonce), PrefixLen: len(head), PrefixHash: hash(head), TailHash: hash(nil)}
		}
		if cp.ParserVersion < activity.ParserVersion {
			// Retain the generation/byte identities while revisiting old records.
			// Replay progress is committed with metadata, so restart is incremental.
			cp.ReplayUntil = cp.Offset
			cp.Offset, cp.TailHash = 0, hash(nil)
			cp.ParserVersion = activity.ParserVersion
		}
		cp.Name = f.Name
		for cp.Offset < f.Size {
			if w.stopping() || !w.Enabled(id) {
				return
			}
			if budget <= 0 {
				return
			}
			start := cp.Offset
			want := chunkSize
			if f.Size-start < int64(want) {
				want = int(f.Size - start)
			}
			data, err := read(r, f.Name, start, want)
			if err != nil {
				syncErr = err
				return
			}
			budget -= len(data)
			end := bytes.LastIndexByte(data, '\n')
			if end < 0 {
				if len(data) == chunkSize {
					syncErr = errors.New("oversized log line")
				}
				break
			}
			data = data[:end+1]
			// Verify the generation still occupies this name before committing. A
			// rotation between chunks cannot file another generation under this cursor.
			check, err := read(r, f.Name, 0, cp.PrefixLen)
			if err != nil || hash(check) != cp.PrefixHash {
				syncErr = errors.New("file rotated during read")
				return
			}
			budget -= len(check)
			now := time.Now().UnixMilli()
			events := []activity.Event{}
			invalid := int64(0)
			offset := start
			for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				stamp, topics, msg, ok := activity.SplitArchiveLine(strings.TrimSuffix(string(line), "\n"))
				if !ok {
					if offset >= cp.ReplayUntil && bytes.Contains(line, []byte("DNSUSER")) {
						invalid++
					}
					offset += int64(len(line))
					continue
				}
				e, relevant, err := activity.Parse(id, "", stamp, topics, msg, now, cfg.Timezone)
				if relevant && err != nil && offset >= cp.ReplayUntil {
					invalid++
				}
				if relevant && err == nil && e.At >= now-int64(activity.Retention/time.Millisecond) {
					e.SourceKey = hash([]byte(cp.Generation + ":" + strconv.FormatInt(offset, 10)))
					e.Replay = offset < cp.ReplayUntil
					events = append(events, e)
				}
				offset += int64(len(line))
			}
			next := cp
			next.Offset = start + int64(len(data))
			next.Size = f.Size
			next.Modified = f.Modified
			tailStart := next.Offset - 128
			if tailStart < 0 {
				tailStart = 0
			}
			tail, err := read(r, f.Name, tailStart, int(next.Offset-tailStart))
			if err != nil {
				syncErr = err
				return
			}
			budget -= len(tail)
			next.TailHash = hash(tail)
			// Guard the final boundary too: no offset advances across a mid-read replace.
			verify, err := read(r, f.Name, start, len(data))
			if err != nil || !bytes.Equal(verify, data) {
				syncErr = errors.New("archive changed during read")
				return
			}
			budget -= len(verify)
			w.mu.Lock()
			s = w.status[id]
			s.Invalid += invalid
			s.LastReceived = now
			s.LastPersisted = now
			s.BytesRead += int64(len(data))
			err = w.db.PersistActivity(events, map[string]activity.Status{id: s}, nil, map[string]activity.Checkpoint{id: next})
			if err == nil {
				w.status[id] = s
			}
			w.mu.Unlock()
			if err != nil {
				syncErr = err
				return
			}
			cp = next
		}
		// A renamed but already-complete file still needs its new manifest recorded.
		if cp.Offset == f.Size {
			cp.ReplayUntil = 0
			cp.Size = f.Size
			cp.Modified = f.Modified
			if err = w.db.PersistActivity(nil, nil, nil, map[string]activity.Checkpoint{id: cp}); err != nil {
				syncErr = err
				return
			}
			w.mu.Lock()
			w.verified[manifestKey] = cp.Generation + ":" + strconv.FormatInt(f.Size, 10) + ":" + f.Modified
			w.mu.Unlock()

		}
		found := false
		for i, c := range checkpoints {
			if c.Generation == cp.Generation {
				checkpoints[i] = cp
				found = true
				break
			}
		}
		if !found {
			checkpoints = append(checkpoints, cp)
		}
	}
}
