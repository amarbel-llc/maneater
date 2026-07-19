package commands

import (
	"strings"
	"testing"

	"code.linenisgreat.com/maneater/internal/0/config"
	"code.linenisgreat.com/maneater/internal/alfa/corpus"
)

func TestExpandManpagesCorpusDefaults(t *testing.T) {
	got, err := expandManpagesCorpus(config.CorpusConfig{Type: "manpages"})
	if err != nil {
		t.Fatalf("expandManpagesCorpus: %v", err)
	}

	if got.Name != "manpages" {
		t.Errorf("Name = %q, want manpages", got.Name)
	}
	if got.Type != "command" {
		t.Errorf("Type = %q, want command", got.Type)
	}
	if len(got.ListCmd) != 2 || got.ListCmd[0] != "maneater-man" || got.ListCmd[1] != "list" {
		t.Errorf("ListCmd = %v, want [maneater-man list]", got.ListCmd)
	}
	if len(got.ReadCmd) != 2 || got.ReadCmd[1] != "read" {
		t.Errorf("ReadCmd = %v, want [maneater-man read]", got.ReadCmd)
	}
	if len(got.HashCmd) != 2 || got.HashCmd[1] != "hash" {
		t.Errorf("HashCmd = %v, want [maneater-man hash]", got.HashCmd)
	}
	if len(got.PrepareCmd) != 2 || got.PrepareCmd[1] != "prepare" {
		t.Errorf("PrepareCmd = %v, want [maneater-man prepare]", got.PrepareCmd)
	}
	if got.Workers != 8 {
		t.Errorf("Workers = %d, want 8", got.Workers)
	}

	// MaxChars and Model must pass through UNRESOLVED: config.Hash folds
	// their raw values, and the synthesized default has always been
	// {MaxChars: 0, Model: ""} — normalizing here would invalidate every
	// existing default-corpus cache digest.
	if got.MaxChars != 0 {
		t.Errorf("MaxChars = %d, want 0 (raw, for cache-hash stability)", got.MaxChars)
	}
	if got.Model != "" {
		t.Errorf("Model = %q, want \"\" (raw, for cache-hash stability)", got.Model)
	}
}

func TestExpandManpagesCorpusOverrides(t *testing.T) {
	got, err := expandManpagesCorpus(config.CorpusConfig{
		Type:     "manpages",
		Name:     "system-man",
		MaxChars: 800,
		Model:    "qwen3-embedding-4b",
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("expandManpagesCorpus: %v", err)
	}

	if got.Name != "system-man" {
		t.Errorf("Name = %q, want system-man", got.Name)
	}
	if got.MaxChars != 800 {
		t.Errorf("MaxChars = %d, want 800", got.MaxChars)
	}
	if got.Model != "qwen3-embedding-4b" {
		t.Errorf("Model = %q, want qwen3-embedding-4b", got.Model)
	}
	if got.Workers != 2 {
		t.Errorf("Workers = %d, want 2", got.Workers)
	}
}

func TestExpandManpagesCorpusRejectsCommandFields(t *testing.T) {
	cases := []struct {
		key string
		cc  config.CorpusConfig
	}{
		{"paths", config.CorpusConfig{Type: "manpages", Paths: []string{"docs/*"}}},
		{"list-cmd", config.CorpusConfig{Type: "manpages", ListCmd: []string{"ls"}}},
		{"read-cmd", config.CorpusConfig{Type: "manpages", ReadCmd: []string{"cat"}}},
		{"hash-cmd", config.CorpusConfig{Type: "manpages", HashCmd: []string{"sha256sum"}}},
		{"prepare-cmd", config.CorpusConfig{Type: "manpages", PrepareCmd: []string{"true"}}},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			_, err := expandManpagesCorpus(tc.cc)
			if err == nil {
				t.Fatalf("expandManpagesCorpus accepted %s on a manpages corpus; want error", tc.key)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error %q does not name the offending key %q", err, tc.key)
			}
		})
	}
}

func TestResolveCorporaDefaultIsManpages(t *testing.T) {
	resolved, err := resolveCorpora(config.ManeaterConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveCorpora: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d corpora, want 1 (synthesized manpages default)", len(resolved))
	}

	rc := resolved[0]
	if rc.Corpus.Name() != "manpages" {
		t.Errorf("Corpus.Name() = %q, want manpages", rc.Corpus.Name())
	}
	if rc.Config.Type != "command" {
		t.Errorf("Config.Type = %q, want command (expanded)", rc.Config.Type)
	}
	cmdc, ok := rc.Corpus.(*corpus.CommandCorpus)
	if !ok {
		t.Fatalf("Corpus is %T, want *corpus.CommandCorpus", rc.Corpus)
	}
	if cmdc.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cmdc.Workers)
	}
	if len(cmdc.HashCmd) == 0 {
		t.Error("HashCmd empty; default manpages corpus must keep its hash fast-path")
	}
}

func TestResolveCorporaManpagesAlongsideFiles(t *testing.T) {
	cfg := config.ManeaterConfig{
		Corpora: []config.CorpusConfig{
			{Type: "manpages"},
			{Name: "docs", Type: "files", Paths: []string{"docs/*.md"}},
		},
	}

	resolved, err := resolveCorpora(cfg, nil)
	if err != nil {
		t.Fatalf("resolveCorpora: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("got %d corpora, want 2", len(resolved))
	}
	if resolved[0].Corpus.Name() != "manpages" {
		t.Errorf("first corpus = %q, want manpages", resolved[0].Corpus.Name())
	}
	if resolved[1].Corpus.Name() != "docs" {
		t.Errorf("second corpus = %q, want docs", resolved[1].Corpus.Name())
	}
}
