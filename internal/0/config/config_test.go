package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfigModelsOverlayWins(t *testing.T) {
	base := ManeaterConfig{
		Default: "a",
		Models: map[string]ModelConfig{
			"a": {Path: "/old"},
			"b": {Path: "/b"},
		},
	}
	overlay := ManeaterConfig{
		Default: "c",
		Models: map[string]ModelConfig{
			"a": {Path: "/new"},
			"c": {Path: "/c"},
		},
	}

	merged := Merge(base, overlay)
	if merged.Default != "c" {
		t.Errorf("Default = %q, want %q", merged.Default, "c")
	}
	if merged.Models["a"].Path != "/new" {
		t.Errorf("Model a path = %q, want /new", merged.Models["a"].Path)
	}
	if merged.Models["b"].Path != "/b" {
		t.Errorf("Model b should be preserved from base")
	}
	if merged.Models["c"].Path != "/c" {
		t.Errorf("Model c should be added from overlay")
	}
}

func TestLoadHierarchyFallsBackToModelsToml(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "models.toml"), []byte(`
default = "test"

[models.test]
path = "/tmp/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	if cfg.Default != "test" {
		t.Errorf("Default = %q, want %q", cfg.Default, "test")
	}
	if cfg.Models["test"].Path != "/tmp/model.gguf" {
		t.Errorf("Model test path = %q, want /tmp/model.gguf", cfg.Models["test"].Path)
	}
}

func TestLoadHierarchyNoConfigsIsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	if len(cfg.Models) != 0 {
		t.Error("models should be empty when no configs exist")
	}
}

func TestLoadHierarchyExpandsEnvInModelPath(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
default = "test"

[models.test]
path = "$MANEATER_TEST_MODEL_DIR/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_TEST_MODEL_DIR", "/tmp/models")

	cfg, err := LoadHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	want := "/tmp/models/model.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestLoadHierarchyExpandsEnvBraceSyntax(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
default = "test"

[models.test]
path = "${MANEATER_TEST_DATA}/maneater/models/nomic.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_TEST_DATA", "/home/user/.local/share")

	cfg, err := LoadHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	want := "/home/user/.local/share/maneater/models/nomic.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestMergeManpathIncludeAccumulates(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/base/man"},
		},
	}
	overlay := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/overlay/man"},
		},
	}

	merged := Merge(base, overlay)
	if merged.Manpath == nil {
		t.Fatal("merged manpath should not be nil")
	}
	if len(merged.Manpath.Include) != 2 {
		t.Fatalf("expected 2 include paths, got %d", len(merged.Manpath.Include))
	}
	if merged.Manpath.Include[0] != "/base/man" {
		t.Errorf("first include = %q, want /base/man", merged.Manpath.Include[0])
	}
	if merged.Manpath.Include[1] != "/overlay/man" {
		t.Errorf("second include = %q, want /overlay/man", merged.Manpath.Include[1])
	}
}

func TestMergeManpathNoAutoOverlays(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: false},
	}
	overlay := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: true},
	}

	merged := Merge(base, overlay)
	if !merged.Manpath.NoAuto {
		t.Error("overlay no-auto=true should override base no-auto=false")
	}
}

func TestMergeManpathBaseOnlyPreserved(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/base/man"},
			NoAuto:  true,
		},
	}
	overlay := ManeaterConfig{}

	merged := Merge(base, overlay)
	if merged.Manpath == nil || len(merged.Manpath.Include) != 1 {
		t.Error("base manpath should be preserved when overlay has none")
	}
	if !merged.Manpath.NoAuto {
		t.Error("base no-auto should be preserved")
	}
}

func TestParseCorporaConfig(t *testing.T) {
	input := []byte(`
[[corpora]]
name = "docs"
type = "files"
paths = ["docs/*.md", "README.md"]
max-chars = 1000

[[corpora]]
name = "src"
type = "files"
paths = ["*.go"]
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	corpora := DecodeCorpora(doc)
	if len(corpora) != 2 {
		t.Fatalf("expected 2 corpora, got %d", len(corpora))
	}

	if corpora[0].Name != "docs" {
		t.Errorf("first corpus name = %q, want docs", corpora[0].Name)
	}
	if corpora[0].Type != "files" {
		t.Errorf("first corpus type = %q, want files", corpora[0].Type)
	}
	if len(corpora[0].Paths) != 2 {
		t.Fatalf("first corpus paths: got %d, want 2", len(corpora[0].Paths))
	}
	if corpora[0].MaxChars != 1000 {
		t.Errorf("first corpus max-chars = %d, want 1000", corpora[0].MaxChars)
	}

	if corpora[1].Name != "src" {
		t.Errorf("second corpus name = %q, want src", corpora[1].Name)
	}
	if corpora[1].MaxChars != 0 {
		t.Errorf("second corpus max-chars = %d, want 0 (default)", corpora[1].MaxChars)
	}
}

func TestParseCorporaWithModels(t *testing.T) {
	input := []byte(`
default = "nomic"

[models.nomic]
path = "/tmp/model.gguf"

[[corpora]]
name = "notes"
type = "files"
paths = ["*.md"]
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
	if cfg.Default != "nomic" {
		t.Errorf("Default = %q, want nomic", cfg.Default)
	}
	if cfg.Models["nomic"].Path != "/tmp/model.gguf" {
		t.Errorf("model path wrong")
	}

	corpora := DecodeCorpora(doc)
	if len(corpora) != 1 {
		t.Fatalf("expected 1 corpus, got %d", len(corpora))
	}
	if corpora[0].Name != "notes" {
		t.Errorf("corpus name = %q, want notes", corpora[0].Name)
	}
}

func TestMergeCorporaAccumulates(t *testing.T) {
	base := ManeaterConfig{
		Corpora: []CorpusConfig{
			{Name: "docs", Type: "files", Paths: []string{"docs/*.md"}},
		},
	}
	overlay := ManeaterConfig{
		Corpora: []CorpusConfig{
			{Name: "src", Type: "files", Paths: []string{"*.go"}},
		},
	}

	merged := Merge(base, overlay)
	if len(merged.Corpora) != 2 {
		t.Fatalf("expected 2 corpora, got %d", len(merged.Corpora))
	}
	if merged.Corpora[0].Name != "docs" {
		t.Errorf("first corpus = %q, want docs", merged.Corpora[0].Name)
	}
	if merged.Corpora[1].Name != "src" {
		t.Errorf("second corpus = %q, want src", merged.Corpora[1].Name)
	}
}

func TestLoadHierarchyWithCorpora(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
[models.test]
path = "/tmp/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "maneater.toml"), []byte(`
[[corpora]]
name = "project-docs"
type = "files"
paths = ["*.md"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	if len(cfg.Models) != 1 {
		t.Errorf("expected 1 model from global config")
	}
	if len(cfg.Corpora) != 1 {
		t.Fatalf("expected 1 corpus from project config, got %d", len(cfg.Corpora))
	}
	if cfg.Corpora[0].Name != "project-docs" {
		t.Errorf("corpus name = %q, want project-docs", cfg.Corpora[0].Name)
	}
}

func TestParseStorageConfig(t *testing.T) {
	input := []byte(`
[storage]
store-id = "my-bucket"
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
	if cfg.Storage == nil {
		t.Fatal("storage config should not be nil")
	}
	if cfg.Storage.StoreID != "my-bucket" {
		t.Errorf("store-id = %q, want my-bucket", cfg.Storage.StoreID)
	}
}

func TestMergeStorageOverlayReplaces(t *testing.T) {
	base := ManeaterConfig{
		Storage: &StorageConfig{StoreID: "old-store"},
	}
	overlay := ManeaterConfig{
		Storage: &StorageConfig{StoreID: "new-store"},
	}

	merged := Merge(base, overlay)
	if merged.Storage == nil {
		t.Fatal("merged storage should not be nil")
	}
	if merged.Storage.StoreID != "new-store" {
		t.Errorf("store-id = %q, want new-store", merged.Storage.StoreID)
	}
}

func TestMergeStorageBasePreserved(t *testing.T) {
	base := ManeaterConfig{
		Storage: &StorageConfig{StoreID: "base-store"},
	}
	overlay := ManeaterConfig{}

	merged := Merge(base, overlay)
	if merged.Storage == nil {
		t.Fatal("base storage should be preserved when overlay has none")
	}
	if merged.Storage.StoreID != "base-store" {
		t.Errorf("store-id = %q, want base-store", merged.Storage.StoreID)
	}
}

func TestResolveStorageDefaults(t *testing.T) {
	cfg := ManeaterConfig{}
	sc := ResolveStorage(cfg)
	if sc.StoreID != "maneater" {
		t.Errorf("default store-id = %q, want maneater", sc.StoreID)
	}
}

func TestResolveStorageExplicit(t *testing.T) {
	cfg := ManeaterConfig{
		Storage: &StorageConfig{StoreID: "custom"},
	}
	sc := ResolveStorage(cfg)
	if sc.StoreID != "custom" {
		t.Errorf("store-id = %q, want custom", sc.StoreID)
	}
}

func TestLoadHierarchyBaseFromEnv(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	baseDir := t.TempDir()
	basePath := filepath.Join(baseDir, "maneater.toml")
	if err := os.WriteFile(basePath, []byte(`
default = "base-model"
[models.base-model]
path = "/base/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "maneater.toml"), []byte(`
[[corpora]]
name = "docs"
type = "files"
paths = ["*.md"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_CONFIG", basePath)

	cfg, err := LoadHierarchy(home, dir)
	if err != nil {
		t.Fatalf("LoadHierarchy: %v", err)
	}

	if cfg.Models["base-model"].Path != "/base/model.gguf" {
		t.Errorf("expected base model, got models = %v", cfg.Models)
	}

	if len(cfg.Corpora) != 1 || cfg.Corpora[0].Name != "docs" {
		t.Errorf("expected project corpora, got %v", cfg.Corpora)
	}
}

func TestActiveModelSingleModel(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"only": {Path: "/model.gguf"},
		},
	}
	name, model, err := ActiveModel(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "only" {
		t.Errorf("name = %q, want only", name)
	}
	if model.Path != "/model.gguf" {
		t.Errorf("path = %q, want /model.gguf", model.Path)
	}
}

func TestActiveModelNoModels(t *testing.T) {
	_, _, err := ActiveModel(ManeaterConfig{})
	if err == nil {
		t.Error("expected error for empty models")
	}
}

func TestActiveModelDefaultKey(t *testing.T) {
	cfg := ManeaterConfig{
		Default: "b",
		Models: map[string]ModelConfig{
			"a": {Path: "/a.gguf"},
			"b": {Path: "/b.gguf"},
		},
	}
	name, model, err := ActiveModel(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "b" {
		t.Errorf("name = %q, want b", name)
	}
	if model.Path != "/b.gguf" {
		t.Errorf("path = %q, want /b.gguf", model.Path)
	}
}

func TestActiveModelMultipleNoDefault(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"a": {Path: "/a.gguf"},
			"b": {Path: "/b.gguf"},
		},
	}
	_, _, err := ActiveModel(cfg)
	if err == nil {
		t.Error("expected error for multiple models without default")
	}
}

func TestParseManpathConfig(t *testing.T) {
	input := []byte(`
[manpath]
include = ["/extra/man", "vendor/man"]
no-auto = true
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
	if cfg.Manpath == nil {
		t.Fatal("manpath config should not be nil")
	}
	if len(cfg.Manpath.Include) != 2 {
		t.Fatalf("expected 2 include paths, got %d", len(cfg.Manpath.Include))
	}
	if !cfg.Manpath.NoAuto {
		t.Error("no-auto should be true")
	}
}

func TestParseModelNCtxAndPooling(t *testing.T) {
	input := []byte(`
[models.qwen3-4b]
path = "/tmp/qwen3.gguf"
n-ctx = 4096
pooling = "last"
query-prefix = "Instruct: ...\nQuery: "
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
	m := cfg.Models["qwen3-4b"]
	if m.NCtx != 4096 {
		t.Errorf("NCtx = %d, want 4096", m.NCtx)
	}
	if m.Pooling != "last" {
		t.Errorf("Pooling = %q, want last", m.Pooling)
	}
}

func TestModelResolvedNCtxDefaults(t *testing.T) {
	cases := []struct {
		got  int
		want int
	}{
		{0, 512},     // unset preserves the historical hardcoded value
		{1, 1},       // any positive value passes through
		{4096, 4096}, // realistic large value
	}
	for _, c := range cases {
		m := ModelConfig{NCtx: c.got}
		if got := m.ResolvedNCtx(); got != c.want {
			t.Errorf("ResolvedNCtx() with NCtx=%d = %d, want %d", c.got, got, c.want)
		}
	}
}

func TestModelResolvedNCtxNegativeFallsBackToDefault(t *testing.T) {
	m := ModelConfig{NCtx: -1}
	if got := m.ResolvedNCtx(); got != 512 {
		t.Errorf("ResolvedNCtx() with NCtx=-1 = %d, want 512 (default)", got)
	}
}

func TestModelValidatePoolingAccepts(t *testing.T) {
	for _, p := range []string{"", "mean", "cls", "last"} {
		m := ModelConfig{Pooling: p}
		if err := m.ValidatePooling(); err != nil {
			t.Errorf("ValidatePooling(%q): unexpected error %v", p, err)
		}
	}
}

func TestParseCorpusModelField(t *testing.T) {
	input := []byte(`
[[corpora]]
name = "project-docs"
type = "files"
paths = ["docs/*.md"]
model = "qwen3-4b"
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	corpora := DecodeCorpora(doc)
	if len(corpora) != 1 {
		t.Fatalf("got %d corpora, want 1", len(corpora))
	}
	if corpora[0].Model != "qwen3-4b" {
		t.Errorf("Model = %q, want qwen3-4b", corpora[0].Model)
	}
}

func TestActiveModelForCorpusOverridesDefault(t *testing.T) {
	cfg := ManeaterConfig{
		Default: "small",
		Models: map[string]ModelConfig{
			"small": {Path: "/small.gguf"},
			"big":   {Path: "/big.gguf", NCtx: 4096, Pooling: "last"},
		},
	}
	corpus := CorpusConfig{Name: "smart", Model: "big"}

	name, model, err := ActiveModelForCorpus(cfg, corpus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "big" {
		t.Errorf("name = %q, want big", name)
	}
	if model.NCtx != 4096 {
		t.Errorf("NCtx = %d, want 4096", model.NCtx)
	}
}

func TestActiveModelForCorpusFallsBackToDefault(t *testing.T) {
	cfg := ManeaterConfig{
		Default: "small",
		Models: map[string]ModelConfig{
			"small": {Path: "/small.gguf"},
			"big":   {Path: "/big.gguf"},
		},
	}
	corpus := CorpusConfig{Name: "manpages"} // Model empty

	name, _, err := ActiveModelForCorpus(cfg, corpus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "small" {
		t.Errorf("name = %q, want small (default)", name)
	}
}

func TestActiveModelForCorpusUndefinedModelErrors(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"only": {Path: "/only.gguf"},
		},
	}
	corpus := CorpusConfig{Name: "broken", Model: "missing"}

	_, _, err := ActiveModelForCorpus(cfg, corpus)
	if err == nil {
		t.Fatal("expected error for undefined model reference")
	}
}

func TestActiveModelForCorpusPathlessModelErrors(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"empty": {Path: ""}, // intentionally pathless
		},
	}
	corpus := CorpusConfig{Name: "broken", Model: "empty"}

	_, _, err := ActiveModelForCorpus(cfg, corpus)
	if err == nil {
		t.Fatal("expected error for pathless model")
	}
}

func TestModelValidatePoolingRejects(t *testing.T) {
	for _, p := range []string{"max", "first", "MEAN", " last", "last "} {
		m := ModelConfig{Pooling: p}
		if err := m.ValidatePooling(); err == nil {
			t.Errorf("ValidatePooling(%q): expected error, got nil", p)
		}
	}
}
