package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/maneater/internal/config"
	"github.com/amarbel-llc/maneater/internal/embedding"
)

func TestConfigHash(t *testing.T) {
	h1 := config.Hash(config.ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, config.CorpusConfig{MaxChars: 500})

	h2 := config.Hash(config.ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, config.CorpusConfig{MaxChars: 500})

	if h1 != h2 {
		t.Errorf("same inputs produced different hashes: %s vs %s", h1, h2)
	}

	if len(h1) != 12 {
		t.Errorf("hash length: got %d, want 12", len(h1))
	}

	h3 := config.Hash(config.ModelConfig{
		Path:           "/nix/store/def-model.gguf",
		DocumentPrefix: "search_document: ",
	}, config.CorpusConfig{MaxChars: 500})

	if h1 == h3 {
		t.Error("different model paths produced same hash")
	}

	h4 := config.Hash(config.ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "query: ",
	}, config.CorpusConfig{MaxChars: 500})

	if h1 == h4 {
		t.Error("different DocumentPrefix produced same hash")
	}

	h5 := config.Hash(config.ModelConfig{
		Path:           "/nix/store/abc-model.gguf",
		DocumentPrefix: "search_document: ",
	}, config.CorpusConfig{MaxChars: 1000})

	if h1 == h5 {
		t.Error("different MaxChars produced same hash")
	}
}

func TestMetaSaveLoad(t *testing.T) {
	dir := t.TempDir()
	meta := embedding.IndexMeta{
		ModelPath:      "/nix/store/abc.gguf",
		DocumentPrefix: "search_document: ",
		ConfigHash:     "abc123def456",
	}

	if err := embedding.SaveMeta(dir, meta); err != nil {
		t.Fatalf("embedding.SaveMeta: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("reading meta.json: %v", err)
	}

	var loaded embedding.IndexMeta
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parsing meta.json: %v", err)
	}

	if loaded.ConfigHash != "abc123def456" {
		t.Errorf("ConfigHash: got %q, want %q", loaded.ConfigHash, "abc123def456")
	}
}
