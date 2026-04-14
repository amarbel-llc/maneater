package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigHash(t *testing.T) {
	h1 := ConfigHash(ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, CorpusConfig{MaxChars: 500})

	// Same inputs produce same hash.
	h2 := ConfigHash(ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, CorpusConfig{MaxChars: 500})

	if h1 != h2 {
		t.Errorf("same inputs produced different hashes: %s vs %s", h1, h2)
	}

	// 12 hex chars.
	if len(h1) != 12 {
		t.Errorf("hash length: got %d, want 12", len(h1))
	}

	// Different model path produces different hash.
	h3 := ConfigHash(ModelConfig{
		Path:           "/nix/store/def-model.gguf",
		DocumentPrefix: "search_document: ",
	}, CorpusConfig{MaxChars: 500})

	if h1 == h3 {
		t.Error("different model paths produced same hash")
	}

	// Different DocumentPrefix produces different hash.
	h4 := ConfigHash(ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "query: ",
	}, CorpusConfig{MaxChars: 500})

	if h1 == h4 {
		t.Error("different DocumentPrefix produced same hash")
	}

	// Different max-chars produces different hash.
	h5 := ConfigHash(ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, CorpusConfig{MaxChars: 1000})

	if h1 == h5 {
		t.Error("different MaxChars produced same hash")
	}
}

func TestMetaSaveLoad(t *testing.T) {
	dir := t.TempDir()
	meta := IndexMeta{
		ModelPath:      "/nix/store/abc.gguf",
		DocumentPrefix: "search_document: ",
		ConfigHash:     "abc123def456",
	}

	if err := SaveMeta(dir, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("reading meta.json: %v", err)
	}

	var loaded IndexMeta
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parsing meta.json: %v", err)
	}

	if loaded.ConfigHash != "abc123def456" {
		t.Errorf("ConfigHash: got %q, want %q", loaded.ConfigHash, "abc123def456")
	}
}
