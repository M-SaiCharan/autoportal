package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// replaceBundle swaps the .app at app for the one inside zipData. The new
// bundle is unpacked next to the old one (same disk, so the final swap is
// two renames) and checked before anything is moved.
func replaceBundle(app string, zipData []byte) error {
	parent := filepath.Dir(app)
	stage, err := os.MkdirTemp(parent, ".autoportal-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", parent, err)
	}
	defer os.RemoveAll(stage)
	if err := unzip(zipData, stage); err != nil {
		return err
	}
	fresh := filepath.Join(stage, "autoportal.app")
	if fi, err := os.Stat(filepath.Join(fresh, "Contents", "MacOS", "autoportal")); err != nil || fi.Mode()&0o111 == 0 {
		return errors.New("the downloaded update does not contain autoportal.app")
	}
	old := filepath.Join(stage, "old.app")
	if err := os.Rename(app, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, app); err != nil {
		_ = os.Rename(old, app)
		return err
	}
	return nil
}

// unzip extracts data into dir, refusing paths that escape it.
func unzip(data []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("the downloaded update is not a valid zip: %w", err)
	}
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("update contains an unsafe path %q", f.Name)
		}
		dst := filepath.Join(dir, name)
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			target, err := readAll(f, 4096)
			if err != nil {
				return err
			}
			t := string(target)
			if filepath.IsAbs(t) || !filepath.IsLocal(filepath.Join(filepath.Dir(name), t)) {
				return fmt.Errorf("update contains an unsafe link %q", f.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(t, dst); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := writeFile(f, dst, mode.Perm()|0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func readAll(f *zip.File, limit int64) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, limit))
}

func writeFile(f *zip.File, dst string, perm os.FileMode) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, io.LimitReader(r, maxDownload)); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
