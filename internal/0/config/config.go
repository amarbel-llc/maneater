// Package config loads and merges maneater.toml hierarchies, defines the
// typed configuration schema consumed by the rest of maneater, and exposes
// helpers for computing config-aware cache hashes.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/maneater/internal/0/config/schema"
)

// The config schema types and the tommy-generated codec live in the leaf
// package internal/0/config/schema (isolated so `tommy generate` can
// regenerate config_tommy.go in place). These aliases re-export them so
// callers keep using config.ManeaterConfig, config.ModelConfig, etc.
// unchanged.
type (
	ManeaterConfig         = schema.ManeaterConfig
	CorpusConfig           = schema.CorpusConfig
	ManpathConfig          = schema.ManpathConfig
	ModelConfig            = schema.ModelConfig
	StorageConfig          = schema.StorageConfig
	ManeaterConfigDocument = schema.ManeaterConfigDocument
)

// DefaultMaxChars is the per-chunk character budget corpora fall back to when
// max-chars is zero/unset. Re-exported from the schema package.
const DefaultMaxChars = schema.DefaultMaxChars

// DecodeManeaterConfig decodes raw maneater.toml bytes into a config document.
// Re-exported from the generated schema codec.
var DecodeManeaterConfig = schema.DecodeManeaterConfig

func loadManeaterFile(path string) (ManeaterConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ManeaterConfig{}, false, nil
		}
		return ManeaterConfig{}, false, fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := DecodeManeaterConfig(data)
	if err != nil {
		return ManeaterConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg := *doc.Data()

	return cfg, true, nil
}

// Merge combines base and overlay configs. Models are merged by name
// (overlay wins per key). Exec rules accumulate (both allow and deny lists
// are appended). Scalar fields (Default) are overwritten by overlay if set.
func Merge(base, overlay ManeaterConfig) ManeaterConfig {
	merged := base

	if overlay.Default != "" {
		merged.Default = overlay.Default
	}

	if len(overlay.Models) > 0 {
		if merged.Models == nil {
			merged.Models = make(map[string]ModelConfig)
		}
		for k, v := range overlay.Models {
			merged.Models[k] = v
		}
	}

	merged.Corpora = append(merged.Corpora, overlay.Corpora...)

	if overlay.Storage != nil {
		cp := *overlay.Storage
		merged.Storage = &cp
	}

	if overlay.Manpath != nil {
		if merged.Manpath == nil {
			cp := *overlay.Manpath
			merged.Manpath = &cp
		} else {
			mergedMP := *merged.Manpath
			mergedMP.Include = append(mergedMP.Include, overlay.Manpath.Include...)
			mergedMP.NoAuto = overlay.Manpath.NoAuto
			merged.Manpath = &mergedMP
		}
	}

	return merged
}

// LoadHierarchy loads and merges maneater.toml files from:
//  1. ~/.config/maneater/maneater.toml (global)
//  2. Each parent directory between home and dir
//  3. ./maneater.toml (project-local)
//
// Falls back to ~/.config/maneater/models.toml at the global level if
// maneater.toml doesn't exist there (backward compatibility).
func LoadHierarchy(home, dir string) (ManeaterConfig, error) {
	merged := ManeaterConfig{}

	loadAndMerge := func(path string) error {
		cfg, found, err := loadManeaterFile(path)
		if err != nil {
			return err
		}
		if found {
			merged = Merge(merged, cfg)
		}
		return nil
	}

	if base := os.Getenv("MANEATER_CONFIG"); base != "" {
		if err := loadAndMerge(base); err != nil {
			return ManeaterConfig{}, err
		}
	}

	globalDir := filepath.Join(home, ".config", "maneater")
	globalPath := filepath.Join(globalDir, "maneater.toml")
	cfg, found, err := loadManeaterFile(globalPath)
	if err != nil {
		return ManeaterConfig{}, err
	}
	if found {
		merged = Merge(merged, cfg)
	} else {
		fallbackPath := filepath.Join(globalDir, "models.toml")
		if err := loadAndMerge(fallbackPath); err != nil {
			return ManeaterConfig{}, err
		}
	}

	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "maneater.toml")
			if err := loadAndMerge(parentPath); err != nil {
				return ManeaterConfig{}, err
			}
		}
	}

	dirPath := filepath.Join(cleanDir, "maneater.toml")
	if err := loadAndMerge(dirPath); err != nil {
		return ManeaterConfig{}, err
	}

	expandEnvInModels(&merged)

	return merged, nil
}

// ResolveStorage returns the effective storage config, defaulting StoreID to
// "maneater" when no [storage] section is configured or store-id is blank.
func ResolveStorage(cfg ManeaterConfig) StorageConfig {
	if cfg.Storage != nil && cfg.Storage.StoreID != "" {
		return *cfg.Storage
	}
	return StorageConfig{StoreID: "maneater"}
}

// expandEnvInModels expands $VAR and ${VAR} references in model path fields.
func expandEnvInModels(cfg *ManeaterConfig) {
	for k, m := range cfg.Models {
		if m.Path != "" {
			m.Path = os.ExpandEnv(m.Path)
			cfg.Models[k] = m
		}
	}
}

// LoadDefault is a convenience wrapper using the real home directory and
// working directory.
func LoadDefault() (ManeaterConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ManeaterConfig{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ManeaterConfig{}, err
	}

	return LoadHierarchy(home, cwd)
}

// ActiveModelForCorpus returns the model entry that should embed
// `corpus`'s documents and queries. When corpus.Model is set, that
// named entry must exist in cfg.Models — an empty fallback is not
// silently substituted. When corpus.Model is empty, behavior matches
// ActiveModel(cfg).
func ActiveModelForCorpus(cfg ManeaterConfig, corpus CorpusConfig) (string, ModelConfig, error) {
	if corpus.Model == "" {
		return ActiveModel(cfg)
	}
	model, ok := cfg.Models[corpus.Model]
	if !ok {
		return "", ModelConfig{}, fmt.Errorf(
			"corpus %q references model %q which is not defined in [models.*]",
			corpus.Name, corpus.Model,
		)
	}
	if model.Path == "" {
		return "", ModelConfig{}, fmt.Errorf(
			"corpus %q references model %q which has no 'path'",
			corpus.Name, corpus.Model,
		)
	}
	return corpus.Model, model, nil
}

// ActiveModel picks the model specified by cfg.Default, or the single model
// if there's only one, returning its name and config.
func ActiveModel(cfg ManeaterConfig) (string, ModelConfig, error) {
	if len(cfg.Models) == 0 {
		return "", ModelConfig{}, fmt.Errorf(
			"no [models.*] entries in config hierarchy\n\nCreate a maneater.toml with at least one [models.<name>] entry",
		)
	}

	name := cfg.Default
	if name == "" {
		if len(cfg.Models) == 1 {
			for k := range cfg.Models {
				name = k
			}
		} else {
			return "", ModelConfig{}, fmt.Errorf(
				"multiple models configured but no 'default' key",
			)
		}
	}

	model, ok := cfg.Models[name]
	if !ok {
		return "", ModelConfig{}, fmt.Errorf(
			"default model %q not found in [models]", name,
		)
	}

	if model.Path == "" {
		return "", ModelConfig{}, fmt.Errorf(
			"model %q has no 'path'", name,
		)
	}

	return name, model, nil
}
