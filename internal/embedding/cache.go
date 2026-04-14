package embedding

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CachedEntry is a single index entry with a content hash for incremental
// updates. The hash tracks the source document content so unchanged documents
// can reuse their embeddings across index rebuilds.
type CachedEntry struct {
	Key       string    `json:"key"`
	Hash      string    `json:"hash"`
	Embedding []float32 `json:"embedding"`
}

const entriesFile = "entries.jsonl"

// SaveCachedEntries writes entries to entries.jsonl in dir.
func SaveCachedEntries(dir string, entries []CachedEntry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	f, err := os.Create(filepath.Join(dir, entriesFile))
	if err != nil {
		return fmt.Errorf("creating %s: %w", entriesFile, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encoding entry %s: %w", e.Key, err)
		}
	}

	return nil
}

// LoadCachedEntries reads entries.jsonl from dir into a slice.
func LoadCachedEntries(dir string) ([]CachedEntry, error) {
	f, err := os.Open(filepath.Join(dir, entriesFile))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", entriesFile, err)
	}
	defer f.Close()

	var entries []CachedEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var e CachedEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("parsing entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", entriesFile, err)
	}

	return entries, nil
}
