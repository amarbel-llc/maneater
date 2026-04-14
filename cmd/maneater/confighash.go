package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigHash computes a 12-char hex hash from the model config and corpus
// config fields that affect embedding content. Changing any of these values
// produces a different hash, which maps to a different cache directory.
func ConfigHash(model ModelConfig, corpus CorpusConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "model-path:%s\n", model.Path)
	fmt.Fprintf(h, "document-prefix:%s\n", model.DocumentPrefix)
	fmt.Fprintf(h, "max-chars:%d\n", corpus.MaxChars)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// IndexMeta is the metadata sidecar written alongside entries.jsonl for
// debuggability. It records the full config snapshot so a human can
// understand what produced a given cache directory.
type IndexMeta struct {
	ModelPath      string `json:"modelPath"`
	DocumentPrefix string `json:"documentPrefix"`
	ConfigHash     string `json:"configHash"`
}

// SaveMeta writes meta.json to dir.
func SaveMeta(dir string, meta IndexMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling meta: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644)
}
