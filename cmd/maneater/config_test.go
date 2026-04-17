package main

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

	merged := MergeConfig(base, overlay)
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

func TestLoadManeaterHierarchyFallsBackToModelsToml(t *testing.T) {
	tmpHome := t.TempDir()

	// Only models.toml exists (no maneater.toml).
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

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	if cfg.Default != "test" {
		t.Errorf("Default = %q, want %q", cfg.Default, "test")
	}
	if cfg.Models["test"].Path != "/tmp/model.gguf" {
		t.Errorf("Model test path = %q, want /tmp/model.gguf", cfg.Models["test"].Path)
	}
}

func TestLoadManeaterHierarchyNoConfigsIsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	if len(cfg.Models) != 0 {
		t.Error("models should be empty when no configs exist")
	}
}

func TestLoadManeaterHierarchyExpandsEnvInModelPath(t *testing.T) {
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

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	want := "/tmp/models/model.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestLoadManeaterHierarchyExpandsEnvBraceSyntax(t *testing.T) {
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

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	want := "/home/user/.local/share/maneater/models/nomic.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestMergeConfigManpathIncludeAccumulates(t *testing.T) {
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

	merged := MergeConfig(base, overlay)
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

func TestMergeConfigManpathNoAutoOverlays(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: false},
	}
	overlay := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: true},
	}

	merged := MergeConfig(base, overlay)
	if !merged.Manpath.NoAuto {
		t.Error("overlay no-auto=true should override base no-auto=false")
	}
}

func TestMergeConfigManpathBaseOnlyPreserved(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/base/man"},
			NoAuto:  true,
		},
	}
	overlay := ManeaterConfig{}

	merged := MergeConfig(base, overlay)
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
	corpora := decodeCorporaFromCST(doc)
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

	corpora := decodeCorporaFromCST(doc)
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

	merged := MergeConfig(base, overlay)
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

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
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
read-cmd = ["aws", "s3", "cp"]
write-cmd = ["aws", "s3", "cp", "-"]
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
	if len(cfg.Storage.ReadCmd) != 3 {
		t.Fatalf("expected 3 read-cmd args, got %d", len(cfg.Storage.ReadCmd))
	}
	if cfg.Storage.ReadCmd[0] != "aws" {
		t.Errorf("read-cmd[0] = %q, want aws", cfg.Storage.ReadCmd[0])
	}
	if cfg.Storage.StoreID != "my-bucket" {
		t.Errorf("store-id = %q, want my-bucket", cfg.Storage.StoreID)
	}
}

func TestMergeConfigStorageOverlayReplaces(t *testing.T) {
	base := ManeaterConfig{
		Storage: &StorageConfig{
			ReadCmd:  []string{"old", "read"},
			WriteCmd: []string{"old", "write"},
			StoreID:  "old-store",
		},
	}
	overlay := ManeaterConfig{
		Storage: &StorageConfig{
			ReadCmd:  []string{"new", "read"},
			WriteCmd: []string{"new", "write"},
			StoreID:  "new-store",
		},
	}

	merged := MergeConfig(base, overlay)
	if merged.Storage == nil {
		t.Fatal("merged storage should not be nil")
	}
	if merged.Storage.StoreID != "new-store" {
		t.Errorf("store-id = %q, want new-store", merged.Storage.StoreID)
	}
	if merged.Storage.ReadCmd[0] != "new" {
		t.Errorf("read-cmd[0] = %q, want new", merged.Storage.ReadCmd[0])
	}
}

func TestMergeConfigStorageBasePreserved(t *testing.T) {
	base := ManeaterConfig{
		Storage: &StorageConfig{
			ReadCmd: []string{"base", "read"},
			StoreID: "base-store",
		},
	}
	overlay := ManeaterConfig{}

	merged := MergeConfig(base, overlay)
	if merged.Storage == nil {
		t.Fatal("base storage should be preserved when overlay has none")
	}
	if merged.Storage.StoreID != "base-store" {
		t.Errorf("store-id = %q, want base-store", merged.Storage.StoreID)
	}
}

func TestResolveStorageDefaults(t *testing.T) {
	cfg := ManeaterConfig{}
	sc := resolveStorage(cfg)
	if sc.StoreID != "maneater" {
		t.Errorf("default store-id = %q, want maneater", sc.StoreID)
	}
	if len(sc.ReadCmd) == 0 || sc.ReadCmd[0] != "madder" {
		t.Errorf("default read-cmd should start with madder")
	}
	if len(sc.WriteCmd) < 3 || sc.WriteCmd[2] != "maneater" {
		t.Errorf("default write-cmd should include store-id 'maneater'")
	}
}

func TestResolveStorageExplicit(t *testing.T) {
	cfg := ManeaterConfig{
		Storage: &StorageConfig{
			ReadCmd:  []string{"custom", "read"},
			WriteCmd: []string{"custom", "write"},
			StoreID:  "custom",
		},
	}
	sc := resolveStorage(cfg)
	if sc.StoreID != "custom" {
		t.Errorf("store-id = %q, want custom", sc.StoreID)
	}
	if sc.ReadCmd[0] != "custom" {
		t.Errorf("read-cmd[0] = %q, want custom", sc.ReadCmd[0])
	}
}

func TestLoadManeaterHierarchyBaseFromEnv(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Base config provides a model.
	baseDir := t.TempDir()
	basePath := filepath.Join(baseDir, "maneater.toml")
	if err := os.WriteFile(basePath, []byte(`
default = "base-model"
[models.base-model]
path = "/base/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Project config adds corpora (no models).
	if err := os.WriteFile(filepath.Join(dir, "maneater.toml"), []byte(`
[[corpora]]
name = "docs"
type = "files"
paths = ["*.md"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_CONFIG", basePath)

	cfg, err := LoadManeaterHierarchy(home, dir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	// Model from base layer should be present.
	if cfg.Models["base-model"].Path != "/base/model.gguf" {
		t.Errorf("expected base model, got models = %v", cfg.Models)
	}

	// Corpora from project layer should accumulate on top.
	if len(cfg.Corpora) != 1 || cfg.Corpora[0].Name != "docs" {
		t.Errorf("expected project corpora, got %v", cfg.Corpora)
	}
}

func TestActiveModelFromConfigSingleModel(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"only": {Path: "/model.gguf"},
		},
	}
	name, model, err := activeModelFromConfig(cfg)
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

func TestActiveModelFromConfigNoModels(t *testing.T) {
	_, _, err := activeModelFromConfig(ManeaterConfig{})
	if err == nil {
		t.Error("expected error for empty models")
	}
}

func TestActiveModelFromConfigDefaultKey(t *testing.T) {
	cfg := ManeaterConfig{
		Default: "b",
		Models: map[string]ModelConfig{
			"a": {Path: "/a.gguf"},
			"b": {Path: "/b.gguf"},
		},
	}
	name, model, err := activeModelFromConfig(cfg)
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

func TestActiveModelFromConfigMultipleNoDefault(t *testing.T) {
	cfg := ManeaterConfig{
		Models: map[string]ModelConfig{
			"a": {Path: "/a.gguf"},
			"b": {Path: "/b.gguf"},
		},
	}
	_, _, err := activeModelFromConfig(cfg)
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
