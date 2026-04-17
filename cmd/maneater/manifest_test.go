package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "manifest")
	m := IndexManifest{
		BlobDigest: "blake2b256-abc123def456",
		ConfigHash: "def456",
	}

	if err := SaveManifest(dir, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.BlobDigest != m.BlobDigest {
		t.Errorf("BlobDigest = %q, want %q", loaded.BlobDigest, m.BlobDigest)
	}
	if loaded.ConfigHash != m.ConfigHash {
		t.Errorf("ConfigHash = %q, want %q", loaded.ConfigHash, m.ConfigHash)
	}
}

func TestManifestLoadMissing(t *testing.T) {
	_, err := LoadManifest(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for missing manifest")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}
