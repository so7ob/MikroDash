package subscriber

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

// WriteHotspotLanding publishes a new complete directory atomically. Identical
// output is a no-op; unknown files, symlinks and changed output are refused.
func WriteHotspotLanding(directory string, b LandingBundle) error {
	if directory == "" || len(b.Files) == 0 {
		return errors.New("nonempty HotSpot output and bundle required")
	}
	manifest, err := b.Manifest()
	if err != nil {
		return err
	}
	files := make(map[string][]byte, len(b.Files)+1)
	for name, body := range b.Files {
		if name == "" || name == "." || name == ".." || name == "manifest.json" || filepath.Base(name) != name || len(body) == 0 || len(body) > 4096 {
			return errors.New("invalid HotSpot asset")
		}
		files[name] = body
	}
	files["manifest.json"] = append(manifest, '\n')
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(directory); err == nil {
		return sameLandingFiles(directory, files)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(directory)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".subscriber-handoff-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, body := range files {
		f, err := os.OpenFile(filepath.Join(stage, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(body)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err = os.Rename(stage, directory); err != nil {
		if sameLandingFiles(directory, files) == nil {
			return nil
		}
		return err
	}
	return nil
}

func sameLandingFiles(directory string, files map[string][]byte) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("HotSpot output must be an owned regular directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != len(files) {
		return errors.New("existing HotSpot output contains different files")
	}
	for _, entry := range entries {
		expected, ok := files[entry.Name()]
		if !ok || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return errors.New("existing HotSpot output is not owned")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("existing HotSpot asset is not a regular file")
		}
		actual, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected) {
			return errors.New("existing HotSpot output differs; use a new review directory")
		}
	}
	return nil
}
