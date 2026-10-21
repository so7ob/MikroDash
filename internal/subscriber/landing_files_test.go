package subscriber

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHotspotLandingOutputIsIdempotentAndNeverOverwritesReview(t *testing.T) {
	bundle, err := RenderHotspotLanding(landingFixture(), "C", []byte("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "review")
	if err = WriteHotspotLanding(out, bundle); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(out, "login.html")
	fixed := time.Unix(1000000, 0)
	if err = os.Chtimes(page, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	if err = WriteHotspotLanding(out, bundle); err != nil {
		t.Fatal("identical review did not repeat", err)
	}
	info, err := os.Stat(page)
	if err != nil || !info.ModTime().Equal(fixed) {
		t.Fatal("idempotent repeat rewrote a reviewed file", err)
	}
	if err = os.WriteFile(filepath.Join(out, "operator-note.txt"), []byte("synthetic operator note"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = WriteHotspotLanding(out, bundle); err == nil {
		t.Fatal("unknown existing file accepted")
	}
	if err = os.Remove(filepath.Join(out, "operator-note.txt")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(page, []byte("synthetic changed page"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = WriteHotspotLanding(out, bundle); err == nil {
		t.Fatal("changed reviewed file overwritten")
	}
	actual, err := os.ReadFile(page)
	if err != nil || string(actual) != "synthetic changed page" {
		t.Fatal("operator change was lost", err)
	}
}

func TestHotspotLandingOutputRejectsLinkedAndUnsafeAssets(t *testing.T) {
	bundle, err := RenderHotspotLanding(landingFixture(), "C", []byte("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	out := filepath.Join(root, "review")
	if err = WriteHotspotLanding(out, bundle); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err = os.Symlink(out, linked); err != nil {
		t.Fatal(err)
	}
	if err = WriteHotspotLanding(linked, bundle); err == nil {
		t.Fatal("linked output directory accepted")
	}
	copy := filepath.Join(root, "copy.html")
	if err = os.WriteFile(copy, bundle.Files["login.html"], 0600); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(out, "login.html")
	if err = os.Remove(page); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(copy, page); err != nil {
		t.Fatal(err)
	}
	if err = WriteHotspotLanding(out, bundle); err == nil {
		t.Fatal("identical linked asset accepted")
	}
	bundle.Files["../outside"] = []byte("synthetic")
	absent := filepath.Join(root, "never-published")
	if err = WriteHotspotLanding(absent, bundle); err == nil {
		t.Fatal("unsafe filename accepted")
	}
	if _, err = os.Stat(absent); !os.IsNotExist(err) {
		t.Fatal("invalid bundle was partially published", err)
	}
}
