// Package recovery creates portable application-state backups, never live media.
package recovery

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"antennarr/internal/catalog"
	"antennarr/internal/config"
)

const MaxArchive = 1 << 30
const MaxExpanded = 2 << 30

var dirs = []string{"config", "database", "secrets", "sources", "logs"}

type Manifest struct {
	Format   int               `json:"format"`
	Created  int64             `json:"created"`
	TunerURL string            `json:"tuner_url"`
	Files    map[string]string `json:"files"`
}

func allowed(name string) bool {
	if strings.Contains(name, "\\") || strings.Contains(name, "\x00") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name {
		return false
	}
	for _, d := range dirs {
		if strings.HasPrefix(name, d+"/") {
			return name != "database/antennarr.db-wal" && name != "database/antennarr.db-shm"
		}
	}
	return false
}

// Create stages an immutable copy before compression. An optional callback lets
// the caller release its administration lock once all input files are captured.
func Create(root, base string, store *catalog.Store, captured ...func()) (string, error) {
	// VACUUM INTO produces a consistent SQLite snapshot while WAL writers continue.
	snapshot, e := store.Backup()
	if e != nil {
		return "", e
	}
	defer os.Remove(filepath.Join(root, "backups", snapshot))
	stage, e := os.MkdirTemp(filepath.Join(root, "backups"), ".backup-stage-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	var stagedBytes int64
	for _, d := range dirs {
		e = filepath.WalkDir(filepath.Join(root, d), func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("backup cannot include symlinks")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return errors.New("unsupported backup file")
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if !allowed(filepath.ToSlash(rel)) {
				return nil
			}
			if filepath.ToSlash(rel) == "database/antennarr.db" {
				path = filepath.Join(root, "backups", snapshot)
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			dest := filepath.Join(stage, rel)
			if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, io.LimitReader(in, MaxExpanded-stagedBytes+1))
			closeErr := out.Close()
			stagedBytes += n
			if stagedBytes > MaxExpanded {
				return errors.New("application data exceeds backup limit")
			}
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		})
		if e != nil {
			return "", e
		}
	}
	for _, ready := range captured {
		if ready != nil {
			ready()
		}
	}
	name := fmt.Sprintf("antennarr-%d.zip", time.Now().UnixNano())
	out, e := os.OpenFile(filepath.Join(root, "backups", name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(out.Name())
		}
	}()
	z := zip.NewWriter(out)
	m := Manifest{Format: 1, Created: time.Now().Unix(), TunerURL: base, Files: map[string]string{}}
	var total int64
	add := func(name, path string) error {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return e
		}
		total += st.Size()
		if total > MaxExpanded {
			return errors.New("application data exceeds backup limit")
		}
		w, e := z.Create(name)
		if e != nil {
			return e
		}
		h := sha256.New()
		if _, e = io.Copy(io.MultiWriter(w, h), f); e != nil {
			return e
		}
		m.Files[name] = hex.EncodeToString(h.Sum(nil))
		return nil
	}
	for _, d := range dirs {
		e = filepath.WalkDir(filepath.Join(stage, d), func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("backup cannot include symlinks")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return errors.New("unsupported backup file")
			}
			rel, _ := filepath.Rel(stage, path)
			rel = filepath.ToSlash(rel)
			if !allowed(rel) {
				return nil
			}
			return add(rel, path)
		})
		if e != nil {
			return "", e
		}
	}
	b, _ := json.Marshal(m)
	w, e := z.Create("manifest.json")
	if e != nil {
		return "", e
	}
	if _, e = w.Write(b); e != nil {
		return "", e
	}
	if e = z.Close(); e != nil {
		return "", e
	}
	if e = out.Sync(); e != nil {
		return "", e
	}
	if e = out.Close(); e != nil {
		return "", e
	}
	st, e := os.Stat(out.Name())
	if e != nil || st.Size() > MaxArchive {
		return "", errors.New("compressed backup exceeds 1 GiB")
	}
	ok = true
	return name, nil
}

// Stage validates the entire upload without touching the running catalogue.
func Stage(root string, input io.ReaderAt, size int64) (Manifest, error) {
	var m Manifest
	if size <= 0 || size > MaxArchive {
		return m, errors.New("backup exceeds 1 GiB")
	}
	z, e := zip.NewReader(input, size)
	if e != nil {
		return m, errors.New("not an Antennarr ZIP backup")
	}
	temp, e := os.MkdirTemp(root, "restore-upload-")
	if e != nil {
		return m, e
	}
	defer os.RemoveAll(temp)
	// Rooted filesystem operations enforce the staging boundary independently
	// of ZIP-name validation, including symlink/path-resolution escape attempts.
	staging, e := os.OpenRoot(temp)
	if e != nil {
		return m, e
	}
	defer staging.Close()
	hashes := map[string]string{}
	var total int64
	for _, f := range z.File {
		if len(hashes) > 10000 || f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() || (f.Name != "manifest.json" && !allowed(f.Name)) {
			return m, errors.New("unsafe backup entry")
		}
		if _, ok := hashes[f.Name]; ok {
			return m, errors.New("duplicate backup entry")
		}
		if f.UncompressedSize64 > MaxExpanded {
			return m, errors.New("expanded backup too large")
		}
		total += int64(f.UncompressedSize64)
		if total > MaxExpanded {
			return m, errors.New("expanded backup too large")
		}
		source, e := f.Open()
		if e != nil {
			return m, e
		}
		target := filepath.FromSlash(f.Name)
		if e = staging.MkdirAll(filepath.Dir(target), 0700); e != nil {
			source.Close()
			return m, e
		}
		dest, e := staging.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			source.Close()
			return m, e
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(dest, h), io.LimitReader(source, MaxExpanded+1))
		source.Close()
		closeErr := dest.Close()
		if copyErr != nil || closeErr != nil || n != int64(f.UncompressedSize64) {
			return m, errors.New("damaged backup entry")
		}
		hashes[f.Name] = hex.EncodeToString(h.Sum(nil))
	}
	raw, e := os.ReadFile(filepath.Join(temp, "manifest.json"))
	if e != nil || json.Unmarshal(raw, &m) != nil || m.Format != 1 {
		return m, errors.New("unsupported backup manifest")
	}
	delete(hashes, "manifest.json")
	if len(hashes) != len(m.Files) {
		return m, errors.New("backup file list mismatch")
	}
	for name, hash := range hashes {
		if m.Files[name] != hash {
			return m, errors.New("backup checksum mismatch")
		}
	}
	for _, name := range []string{"config/config.json", "database/antennarr.db", "secrets/admin-password"} {
		if _, ok := hashes[name]; !ok {
			return m, errors.New("backup missing required state")
		}
	}
	password, err := os.ReadFile(filepath.Join(temp, "secrets/admin-password"))
	if err != nil || len(password) == 0 || len(password) > 4096 {
		return m, errors.New("backup admin credential is invalid")
	}
	if _, e = config.ReadAdminCredentials(temp); e != nil {
		return m, errors.New("backup admin login is invalid")
	}
	c, e := config.Load(temp)
	if e != nil || config.Validate(c) != nil {
		return m, errors.New("backup configuration is invalid")
	}
	db, e := catalog.Open(temp)
	if e != nil {
		return m, errors.New("backup catalogue is incompatible")
	}
	var integrity string
	e = db.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	var broken int
	rows, foreignErr := db.DB.Query("PRAGMA foreign_key_check")
	if foreignErr == nil {
		for rows.Next() {
			broken++
		}
		foreignErr = rows.Err()
		rows.Close()
	}
	db.Close()
	if e != nil || integrity != "ok" || foreignErr != nil || broken > 0 {
		return m, errors.New("backup catalogue is damaged")
	}
	if _, e = os.Stat(filepath.Join(root, "restore-pending")); e == nil {
		return m, errors.New("a restore is already pending")
	}
	if e = os.Rename(temp, filepath.Join(root, "restore-pending")); e != nil {
		return m, e
	}
	return m, nil
}

// ApplyPending runs before opening SQLite or accepting HTTP. The staged original
// stays intact until all directories are installed, so interrupted restores retry.
func ApplyPending(root string) error {
	pending := filepath.Join(root, "restore-pending")
	if _, e := os.Stat(pending); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	raw, e := os.ReadFile(filepath.Join(pending, "manifest.json"))
	var m Manifest
	if e != nil || json.Unmarshal(raw, &m) != nil || m.Format != 1 {
		return errors.New("invalid pending restore")
	}
	for _, d := range dirs {
		if e = os.RemoveAll(filepath.Join(root, d)); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Join(root, d), 0700); e != nil {
			return e
		}
		e = filepath.WalkDir(filepath.Join(pending, d), func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(pending, path)
			target := filepath.Join(root, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			if !entry.Type().IsRegular() {
				return errors.New("unsafe staged file")
			}
			// SQLite sidecars were checkpointed when validation closed the staged DB.
			if strings.HasSuffix(rel, "antennarr.db-wal") || strings.HasSuffix(rel, "antennarr.db-shm") {
				return nil
			}
			in, e := os.Open(path)
			if e != nil {
				return e
			}
			defer in.Close()
			out, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = io.Copy(out, in)
			if e == nil {
				e = out.Sync()
			}
			ce := out.Close()
			if e != nil {
				return e
			}
			return ce
		})
		if e != nil {
			return e
		}
	}
	completed := filepath.Join(root, fmt.Sprintf("restore-completed-%d", time.Now().UnixNano()))
	if e = os.Rename(pending, completed); e != nil {
		return e
	}
	return os.RemoveAll(completed)
}
