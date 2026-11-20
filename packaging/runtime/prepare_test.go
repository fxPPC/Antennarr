package main

import (
	"archive/tar"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyLockedCandidateLayerIsSaved(t *testing.T) {
	dir := t.TempDir()
	blob := "verified package installation"
	lock := layer{SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(blob))), Size: int64(len(blob))}
	candidate := filepath.Join(dir, "candidate.tar")
	f, err := os.Create(candidate)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, data := range []string{"unrelated application bytes", blob} {
		h := tar.Header{Name: fmt.Sprintf("blobs/sha256/%x", sha256.Sum256([]byte(data))), Size: int64(len(data)), Mode: 0644}
		if err = tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "runtime.tar.gz")
	if err = fromCandidate(candidate, lock, out); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{blob + "!", blob[1:], strings.ToUpper(blob)} {
		if err = save(strings.NewReader(invalid), lock, out); err == nil {
			t.Fatal("invalid runtime accepted")
		}
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != blob {
		t.Fatal("verified snapshot was replaced", string(got), err)
	}
}
