package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/embedding"
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

func TestConfigHashNCtxInvalidates(t *testing.T) {
	base := config.ModelConfig{Path: "/m.gguf"}
	cc := config.CorpusConfig{MaxChars: 500}

	// 0 and 512 must hash equal because ResolvedNCtx collapses 0 -> 512;
	// otherwise old caches built before the field existed would all
	// invalidate on first run after upgrade.
	hZero := config.Hash(base, cc)
	hExplicit512 := config.Hash(config.ModelConfig{Path: "/m.gguf", NCtx: 512}, cc)
	if hZero != hExplicit512 {
		t.Errorf("NCtx=0 and NCtx=512 should hash equal, got %q vs %q", hZero, hExplicit512)
	}

	hLarge := config.Hash(config.ModelConfig{Path: "/m.gguf", NCtx: 4096}, cc)
	if hZero == hLarge {
		t.Error("different NCtx produced same hash")
	}
}

func TestConfigHashPoolingInvalidates(t *testing.T) {
	base := config.ModelConfig{Path: "/m.gguf"}
	cc := config.CorpusConfig{MaxChars: 500}

	hDefault := config.Hash(base, cc) // pooling = ""
	hLast := config.Hash(config.ModelConfig{Path: "/m.gguf", Pooling: "last"}, cc)
	hMean := config.Hash(config.ModelConfig{Path: "/m.gguf", Pooling: "mean"}, cc)

	if hDefault == hLast {
		t.Error("default pooling and last pooling hashed equal")
	}
	if hLast == hMean {
		t.Error("different pooling values hashed equal")
	}
}

func TestConfigHashCorpusModelInvalidates(t *testing.T) {
	mc := config.ModelConfig{Path: "/m.gguf"}

	hUnset := config.Hash(mc, config.CorpusConfig{MaxChars: 500})
	hQwen := config.Hash(mc, config.CorpusConfig{MaxChars: 500, Model: "qwen3-4b"})

	if hUnset == hQwen {
		t.Error("corpus.Model unset vs set should hash differently")
	}

	hSnowflake := config.Hash(mc, config.CorpusConfig{MaxChars: 500, Model: "snowflake"})
	if hQwen == hSnowflake {
		t.Error("different corpus.Model values hashed equal")
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
