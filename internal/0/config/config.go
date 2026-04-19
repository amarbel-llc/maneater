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

	"github.com/amarbel-llc/tommy/pkg/cst"
)

//go:generate tommy generate
type ManeaterConfig struct {
	Default string                 `toml:"default"`
	Models  map[string]ModelConfig `toml:"models"`
	Manpath *ManpathConfig         `toml:"manpath"`
	Storage *StorageConfig         `toml:"storage"`
	Corpora []CorpusConfig         `toml:"-"` // decoded manually from [[corpora]]
}

type CorpusConfig struct {
	Name     string   `toml:"name"`
	Type     string   `toml:"type"` // "manpages", "files", or "command"
	Paths    []string `toml:"paths"`
	MaxChars int      `toml:"max-chars"`
	ListCmd  []string `toml:"list-cmd"`
	ReadCmd  []string `toml:"read-cmd"`
}

// ManpathConfig controls how maneater discovers man pages beyond the system
// manpath. Include paths are prepended to the system manpath. When NoAuto is
// false (the default), maneater also probes common in-repo locations (man/,
// doc/man/, share/man/) in the current working directory.
type ManpathConfig struct {
	Include []string `toml:"include"`
	NoAuto  bool     `toml:"no-auto"`
}

type ModelConfig struct {
	Path           string `toml:"path"`
	QueryPrefix    string `toml:"query-prefix"`
	DocumentPrefix string `toml:"document-prefix"`
}

type StorageConfig struct {
	StoreID string `toml:"store-id"`
}

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
	cfg.Corpora = DecodeCorpora(doc)

	return cfg, true, nil
}

// DecodeCorpora extracts [[corpora]] array-of-tables entries from the tommy
// CST. Tommy doesn't generate code for array-of-tables, so we walk the CST
// manually using the same primitives the generated code uses.
func DecodeCorpora(doc *ManeaterConfigDocument) []CorpusConfig {
	var corpora []CorpusConfig

	for _, ch := range doc.cstDoc.Root().Children {
		if ch.Kind != cst.NodeArrayTable {
			continue
		}
		if cst.TableHeaderKey(ch) != "corpora" {
			continue
		}

		var cc CorpusConfig
		for _, kv := range ch.Children {
			if kv.Kind != cst.NodeKeyValue {
				continue
			}
			switch cst.KeyValueName(kv) {
			case "name":
				if v, ok := cst.ExtractString(kv); ok {
					cc.Name = v
				}
			case "type":
				if v, ok := cst.ExtractString(kv); ok {
					cc.Type = v
				}
			case "paths":
				if v, ok := cst.ExtractStringSlice(kv); ok {
					cc.Paths = v
				}
			case "max-chars":
				if v, ok := cst.ExtractInt(kv); ok {
					cc.MaxChars = v
				}
			case "list-cmd":
				if v, ok := cst.ExtractStringSlice(kv); ok {
					cc.ListCmd = v
				}
			case "read-cmd":
				if v, ok := cst.ExtractStringSlice(kv); ok {
					cc.ReadCmd = v
				}
			}
		}
		corpora = append(corpora, cc)
	}

	return corpora
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
