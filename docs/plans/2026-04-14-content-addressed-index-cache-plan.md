# Content-Addressed Index Cache Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Replace the model-name-keyed index cache with a two-layer content-addressed cache that supports incremental indexing.

**Architecture:** Directory layer keyed by `{corpusName}/{configHash}` (config hash = SHA256 of model path + DocumentPrefix + corpus params). Entry layer is a single `entries.jsonl` file with per-document content hashes for incremental reuse. `meta.json` sidecar for debuggability.

**Tech Stack:** Go stdlib (`crypto/sha256`, `encoding/json`, `encoding/hex`). No new dependencies.

**Rollback:** Old and new cache directories are disjoint (`{modelName}/` vs `{configHash}/`). Revert the code and old caches still work.

---

### Task 1: New entry format in `internal/embedding/`

Add `CachedEntry` type and JSONL save/load functions alongside the existing `Entry`/`Index` types. The existing format stays untouched — search will be migrated to read the new format in a later task.

**Files:**
- Create: `internal/embedding/cache.go`
- Test: `internal/embedding/cache_test.go`

**Step 1: Write the failing test**

```go
// internal/embedding/cache_test.go
package embedding

import (
	"path/filepath"
	"testing"
)

func TestCacheSaveLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")

	entries := []CachedEntry{
		{Key: "ls(1)", Hash: "abc123", Embedding: []float32{0.1, 0.2, 0.3}},
		{Key: "sed(1)", Hash: "def456", Embedding: []float32{0.4, 0.5, 0.6}},
	}

	if err := SaveCachedEntries(dir, entries); err != nil {
		t.Fatalf("SaveCachedEntries: %v", err)
	}

	loaded, err := LoadCachedEntries(dir)
	if err != nil {
		t.Fatalf("LoadCachedEntries: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded))
	}
	if loaded[0].Key != "ls(1)" || loaded[0].Hash != "abc123" {
		t.Errorf("entry 0: got %+v", loaded[0])
	}
	if len(loaded[0].Embedding) != 3 {
		t.Errorf("embedding dim: got %d, want 3", len(loaded[0].Embedding))
	}
}

func TestCacheLoadMissing(t *testing.T) {
	_, err := LoadCachedEntries(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for missing cache")
	}
}

func TestCacheSaveEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty-cache")

	if err := SaveCachedEntries(dir, nil); err != nil {
		t.Fatalf("SaveCachedEntries: %v", err)
	}

	loaded, err := LoadCachedEntries(dir)
	if err != nil {
		t.Fatalf("LoadCachedEntries: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("got %d entries, want 0", len(loaded))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test` (or `go test ./internal/embedding/ -run TestCache -v`)
Expected: FAIL — `CachedEntry` type and functions not defined.

**Step 3: Write minimal implementation**

```go
// internal/embedding/cache.go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/embedding/ -run TestCache -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/embedding/cache.go internal/embedding/cache_test.go
git commit -m "Add CachedEntry type with JSONL save/load"
```

---

### Task 2: Config hash computation

Add a function that computes the directory-level config hash from model path, DocumentPrefix, and corpus-specific params. Also add `meta.json` save/load.

**Files:**
- Create: `cmd/maneater/confighash.go`
- Test: `cmd/maneater/confighash_test.go`

**Step 1: Write the failing test**

```go
// cmd/maneater/confighash_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/maneater/ -run 'TestConfigHash|TestMeta' -v`
Expected: FAIL — functions not defined.

**Step 3: Write minimal implementation**

```go
// cmd/maneater/confighash.go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/maneater/ -run 'TestConfigHash|TestMeta' -v`
Expected: PASS

**Step 5: Commit**

```
git add cmd/maneater/confighash.go cmd/maneater/confighash_test.go
git commit -m "Add config hash computation and meta.json sidecar"
```

---

### Task 3: Content hashing in the Corpus interface

Extend the `Corpus` interface so that `Documents()` yields a content hash alongside each document. Each corpus type computes the hash from its source material (raw file for manpages/files, read-cmd output for command).

**Files:**
- Modify: `cmd/maneater/corpus.go` — add `Hash` field to `Document`
- Modify: `cmd/maneater/corpus_files.go` — compute SHA256 of file content
- Modify: `cmd/maneater/corpus_command.go` — compute SHA256 of read-cmd output
- Modify: `cmd/maneater/main.go` — compute SHA256 of roff source in manpagesCorpus

**Step 1: Write the failing test**

Add a test that creates a files corpus and verifies documents have non-empty hashes:

```go
// cmd/maneater/corpus_files_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesCorpusDocumentsHaveHash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	corpus := &FilesCorpus{
		CorpusName: "test",
		Patterns:   []string{filepath.Join(dir, "*.txt")},
	}

	for doc, err := range corpus.Documents() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.Hash == "" {
			t.Errorf("document %q has empty hash", doc.Key)
		}
		if len(doc.Hash) != 64 { // SHA256 hex
			t.Errorf("document %q hash length: got %d, want 64", doc.Key, len(doc.Hash))
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/maneater/ -run TestFilesCorpusDocumentsHaveHash -v`
Expected: FAIL — `Document` has no `Hash` field.

**Step 3: Add Hash field to Document and implement in each corpus**

In `cmd/maneater/corpus.go`, add the field:

```go
type Document struct {
	Key   string   // unique identifier shown in search results
	Hash  string   // hex SHA256 of source content for incremental caching
	Texts []string // text chunks to embed (each becomes a separate index entry)
}
```

In `cmd/maneater/corpus_files.go`, hash the raw file content before truncation:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	// ... existing imports
)

// In Documents(), after reading the file and checking isBinary:
	hash := sha256.Sum256(content)
	hashHex := hex.EncodeToString(hash[:])
	// ... truncate text ...
	if !yield(Document{Key: path, Hash: hashHex, Texts: []string{text}}, nil) {
```

In `cmd/maneater/corpus_command.go`, hash the read-cmd output:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	// ... existing imports
)

// In Documents(), after getting text from read-cmd:
	hash := sha256.Sum256([]byte(text))
	hashHex := hex.EncodeToString(hash[:])
	// ... truncate text ...
	if !yield(Document{Key: key, Hash: hashHex, Texts: []string{text}}, nil) {
```

In `cmd/maneater/main.go`, in `manpagesCorpus.Documents()`, hash the raw roff source file. Add a helper:

```go
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
```

Then in the worker goroutine, locate the source file and hash it:

```go
// In the worker goroutine, alongside extractSynopsis/extractTldr:
name, section := parsePageKey(page)
sourcePath, _ := locateSource(c.manpath, section, name)
fileHash := ""
if sourcePath != "" {
	fileHash = hashFile(sourcePath)
}
```

Pass the hash through `pageText` (add a `hash` field) and set it on the `Document`:

```go
if !yield(Document{Key: pt.page, Hash: pt.hash, Texts: chunks}, nil) {
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/maneater/ -run TestFilesCorpusDocumentsHaveHash -v`
Expected: PASS

Run: `go test ./... -v` (full suite to catch regressions)
Expected: PASS

**Step 5: Commit**

```
git add cmd/maneater/corpus.go cmd/maneater/corpus_files.go cmd/maneater/corpus_command.go cmd/maneater/main.go
git commit -m "Add content hashing to Document and all corpus types"
```

---

### Task 4: Wire incremental indexing into `runIndex`

Replace the current `runIndex` flow with the incremental cache logic:
1. Compute config hash per corpus
2. Load existing cached entries
3. Skip documents with matching hashes
4. Embed only new/changed documents
5. Write entries.jsonl + meta.json

Also add `--force` flag to skip cache loading.

**Files:**
- Modify: `cmd/maneater/main.go:831-883` — rewrite `runIndex()`
- Modify: `cmd/maneater/main.go:73-83` — add `--force` flag to index command
- Modify: `cmd/maneater/main.go:292-301` — update `indexCacheDir` signature
- Modify: `cmd/maneater/main.go:304-338` — update `buildCorpusIndex` to support incremental

**Step 1: Write the failing test**

```go
// cmd/maneater/incremental_test.go
package main

import (
	"testing"

	"github.com/amarbel-llc/maneater/internal/embedding"
)

func TestIncrementalBuildSkipsUnchanged(t *testing.T) {
	// Simulate existing cached entries.
	existing := map[string]embedding.CachedEntry{
		"a.txt": {Key: "a.txt", Hash: "aaa", Embedding: []float32{1, 2, 3}},
		"b.txt": {Key: "b.txt", Hash: "bbb", Embedding: []float32{4, 5, 6}},
	}

	// Current documents: a.txt unchanged, b.txt changed hash, c.txt new.
	type doc struct {
		key  string
		hash string
	}
	current := []doc{
		{key: "a.txt", hash: "aaa"}, // unchanged
		{key: "b.txt", hash: "ccc"}, // changed
		{key: "c.txt", hash: "ddd"}, // new
	}

	var reused, needsEmbed []string
	for _, d := range current {
		if e, ok := existing[d.key]; ok && e.Hash == d.hash {
			reused = append(reused, d.key)
		} else {
			needsEmbed = append(needsEmbed, d.key)
		}
	}

	if len(reused) != 1 || reused[0] != "a.txt" {
		t.Errorf("reused: got %v, want [a.txt]", reused)
	}
	if len(needsEmbed) != 2 {
		t.Errorf("needsEmbed: got %v, want [b.txt c.txt]", needsEmbed)
	}
}
```

**Step 2: Run test to verify it passes** (this test validates the logic pattern, not the wiring)

Run: `go test ./cmd/maneater/ -run TestIncrementalBuild -v`
Expected: PASS (it's a pure logic test)

**Step 3: Rewrite `runIndex` with incremental logic**

Update `indexCacheDir` to accept a config hash instead of model name:

```go
func indexCacheDir(corpusName, configHash string) string {
	var base string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, "maneater", "index")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache", "maneater", "index")
	}
	return filepath.Join(base, corpusName, configHash)
}
```

Add `--force` flag parsing in `runIndex`:

```go
func runIndex() error {
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" {
			force = true
		}
	}
	// ... rest of function
```

Rewrite the per-corpus loop in `runIndex`:

```go
for _, corpus := range corpora {
	cc := corpusConfigForCorpus(corpus, cfg)
	hash := ConfigHash(modelCfg, cc)
	cacheDir := indexCacheDir(corpus.Name(), hash)

	// Load existing entries for incremental reuse.
	existing := make(map[string]embedding.CachedEntry)
	if !force {
		if cached, err := embedding.LoadCachedEntries(cacheDir); err == nil {
			for _, e := range cached {
				existing[e.Key] = e
			}
			fmt.Fprintf(os.Stderr, "maneater: loaded %d cached entries for %s\n",
				len(existing), corpus.Name())
		}
	}

	if err := corpus.Prepare(); err != nil {
		return fmt.Errorf("preparing corpus %s: %w", corpus.Name(), err)
	}

	var entries []embedding.CachedEntry
	var reusedCount, embeddedCount int

	for doc, err := range corpus.Documents() {
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: skipping document: %v\n", err)
			continue
		}

		// Check if we can reuse the existing entry.
		if cached, ok := existing[doc.Key]; ok && cached.Hash == doc.Hash {
			entries = append(entries, cached)
			reusedCount++
			continue
		}

		// Embed all text chunks for this document.
		for _, text := range doc.Texts {
			docText := modelCfg.DocumentPrefix + text
			vec, err := emb.Embed(docText)
			if err != nil {
				fmt.Fprintf(os.Stderr, "maneater: skipping %s: %v\n", doc.Key, err)
				continue
			}
			entries = append(entries, embedding.CachedEntry{
				Key:       doc.Key,
				Hash:      doc.Hash,
				Embedding: vec,
			})
		}
		embeddedCount++

		total := reusedCount + embeddedCount
		if total%100 == 0 {
			fmt.Fprintf(os.Stderr, "maneater: [%s] processed %d documents (%d reused, %d embedded)\n",
				corpus.Name(), total, reusedCount, embeddedCount)
		}
	}

	if err := embedding.SaveCachedEntries(cacheDir, entries); err != nil {
		return fmt.Errorf("saving index for %s: %w", corpus.Name(), err)
	}

	meta := IndexMeta{
		ModelPath:      modelCfg.Path,
		DocumentPrefix: modelCfg.DocumentPrefix,
		ConfigHash:     hash,
	}
	if err := SaveMeta(cacheDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: warning: could not save meta.json: %v\n", err)
	}

	fmt.Printf("Done: %s — %d entries (%d reused, %d embedded) saved to %s\n",
		corpus.Name(), len(entries), reusedCount, embeddedCount, cacheDir)
}
```

Add a helper to extract the `CorpusConfig` for a given corpus (needed for config hash):

```go
func corpusConfigForCorpus(c Corpus, cfg ManeaterConfig) CorpusConfig {
	for _, cc := range cfg.Corpora {
		if cc.Name == c.Name() {
			return cc
		}
	}
	// Implicit manpages corpus — return zero-value CorpusConfig.
	return CorpusConfig{}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS

**Step 5: Commit**

```
git add cmd/maneater/main.go
git commit -m "Wire incremental indexing with config hash and --force flag"
```

---

### Task 5: Update search to load new cache format

Update `loadOrBuildIndex` in the searcher to read from `entries.jsonl` (new format). Fall back to building if no cache exists (but do NOT write — indexing is explicit only).

**Files:**
- Modify: `cmd/maneater/main.go:247-283` — rewrite `loadOrBuildIndex`

**Step 1: Rewrite `loadOrBuildIndex`**

```go
func (s *searcher) loadOrBuildIndex() (*embedding.Index, error) {
	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	var combined *embedding.Index

	for _, corpus := range s.corpora {
		cc := corpusConfigForCorpus(corpus, cfg)
		hash := ConfigHash(s.modelCfg, cc)
		cacheDir := indexCacheDir(corpus.Name(), hash)

		cached, err := embedding.LoadCachedEntries(cacheDir)
		if err == nil {
			fmt.Fprintf(os.Stderr, "maneater: loaded %s index (%d entries) from %s\n",
				corpus.Name(), len(cached), cacheDir)

			idx := embedding.NewIndex(0)
			for _, e := range cached {
				idx.Add(e.Key, e.Embedding)
			}
			if len(cached) > 0 {
				idx.Dim = len(cached[0].Embedding)
			}

			if combined == nil {
				combined = idx
			} else {
				combined.Entries = append(combined.Entries, idx.Entries...)
			}
		} else {
			fmt.Fprintf(os.Stderr, "maneater: no index for %s (run 'maneater index' to build)\n",
				corpus.Name())
		}
	}

	if combined == nil {
		combined = embedding.NewIndex(0)
	}

	return combined, nil
}
```

Key change: search no longer builds on cache miss. It warns and returns an empty index for that corpus.

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: PASS

**Step 3: Commit**

```
git add cmd/maneater/main.go
git commit -m "Update search to load new entries.jsonl cache format"
```

---

### Task 6: Remove old save/load format

Delete `Save` and `LoadIndex` from `internal/embedding/index.go` and remove old format references. Update tests.

**Files:**
- Modify: `internal/embedding/index.go:65-158` — remove `Save` and `LoadIndex`
- Modify: `internal/embedding/index_test.go` — remove `TestIndexSaveLoad`, `TestLoadIndexMissing`, `TestIndexSaveCreatesDir`

**Step 1: Delete the old functions**

Remove `Save()` and `LoadIndex()` from `index.go`. Remove `TestIndexSaveLoad`, `TestLoadIndexMissing`, and `TestIndexSaveCreatesDir` from `index_test.go`.

Also remove the old `buildCorpusIndex` function from `main.go` if it is no longer called.

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: PASS — no remaining references to old format.

If compilation fails, grep for remaining callers and update them.

**Step 3: Commit**

```
git add internal/embedding/index.go internal/embedding/index_test.go cmd/maneater/main.go
git commit -m "Remove old pages.txt + embeddings.jsonl format"
```

---

### Task 7: Add `--force` flag to index command definition

Wire `--force` into the command.Param definition so it appears in help text and man page.

**Files:**
- Modify: `cmd/maneater/main.go:73-83` — add force param

**Step 1: Add the param**

In the index command definition:

```go
Params: []command.Param{
	{
		Name:        "force",
		Description: "Force full rebuild, ignoring cached entries",
		Type:        command.Bool,
	},
},
```

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: PASS

**Step 3: Commit**

```
git add cmd/maneater/main.go
git commit -m "Add --force flag to index command definition"
```

---

### Task 8: Nix build verification

Verify the nix build still works with the new code.

**Step 1: Build**

Run: `just build-nix`
Expected: Build succeeds.

**Step 2: Commit** (if any nix changes needed, otherwise skip)
