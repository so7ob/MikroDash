package subscriberlab

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mikrodash/internal/routeros"
)

type failedEvidence struct{}

func (failedEvidence) Write([]byte) (int, error) {
	return 0, errors.New("synthetic evidence disk fault")
}

func TestEvidenceFailureCannotReportSuccessfulProbe(t *testing.T) {
	r := &recorder{w: failedEvidence{}}
	r.emit("synthetic", "sample")
	if r.Err() == nil {
		t.Fatal("lost protocol evidence reported success")
	}
}

func TestPrivateLabFiles(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "runtime.json")
	for _, test := range []struct {
		name, content string
		mode          os.FileMode
		valid         bool
	}{
		{"private", `{"pid":123}`, 0600, true},
		{"public", `{"pid":123}`, 0644, false},
		{"unknown fields", `{"data":"/data"}`, 0600, false},
		{"trailing", `{"pid":123}{"pid":456}`, 0600, false},
		{"malformed", `{`, 0600, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			os.Chmod(path, test.mode)
			var state State
			if err := privateJSON(path, &state); (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
	os.Remove(path)
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if err := privateJSON(path, new(State)); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestWriteCallSiteRechecksOwnership(t *testing.T) {
	// No client exists: bypassing the ownership call would panic or issue a write.
	c := &Client{state: State{PID: -1}}
	if _, err := c.Do(routeros.Cmd{Path: "/radius/incoming/set", Timeout: time.Second}); err == nil {
		t.Fatal("write without owned VM accepted")
	}
	for _, timeout := range []time.Duration{0, -1, 11 * time.Second} {
		if _, err := c.Do(routeros.Cmd{Path: "/radius/incoming/set", Timeout: timeout}); err == nil {
			t.Fatal("unbounded write accepted")
		}
	}
}
