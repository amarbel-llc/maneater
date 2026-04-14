package embedding

import (
	"path/filepath"
	"testing"
)

func TestCacheSaveLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")

	entries := []CachedEntry{
		{Key: "ls(1)", Hash: "abc123", Embedding: []float32{0.1, 0.2, 0.3}},
		{Key: "sed(1)", Hash: "def456", Embedding: []float32{0.4, 0.5, 0.6}},
	}

	if err := SaveCachedEntries(dir, entries); err != nil {
		t.Fatalf("SaveCachedEntries: %v", err)
	}

	loaded, err := LoadCachedEntries(dir)
	if err != nil {
		t.Fatalf("LoadCachedEntries: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded))
	}
	if loaded[0].Key != "ls(1)" || loaded[0].Hash != "abc123" {
		t.Errorf("entry 0: got %+v", loaded[0])
	}
	if len(loaded[0].Embedding) != 3 {
		t.Errorf("embedding dim: got %d, want 3", len(loaded[0].Embedding))
	}
}

func TestCacheLoadMissing(t *testing.T) {
	_, err := LoadCachedEntries(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for missing cache")
	}
}

func TestCacheSaveEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty-cache")

	if err := SaveCachedEntries(dir, nil); err != nil {
		t.Fatalf("SaveCachedEntries: %v", err)
	}

	loaded, err := LoadCachedEntries(dir)
	if err != nil {
		t.Fatalf("LoadCachedEntries: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("got %d entries, want 0", len(loaded))
	}
}
