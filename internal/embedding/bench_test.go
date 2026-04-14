package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func newBenchEmbedder(b *testing.B) *Embedder {
	b.Helper()
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		b.Skip("MANPAGE_MODEL_PATH not set")
	}
	emb, err := NewEmbedder(modelPath)
	if err != nil {
		b.Fatalf("NewEmbedder: %v", err)
	}
	b.Cleanup(emb.Close)
	return emb
}

// BenchmarkEmbedShort measures embedding a short synopsis-length string
// (~50 tokens). This is the typical per-document cost for man page synopses.
func BenchmarkEmbedShort(b *testing.B) {
	emb := newBenchEmbedder(b)
	text := "search_document: ls - list directory contents"

	b.ResetTimer()
	for b.Loop() {
		if _, err := emb.Embed(text); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEmbedMedium measures embedding a medium-length string (~200 tokens).
// This is typical for tldr descriptions or truncated file content.
func BenchmarkEmbedMedium(b *testing.B) {
	emb := newBenchEmbedder(b)
	text := "search_document: sed - Edit text in a scriptable manner. " +
		"Replace all apple occurrences with mango in all input lines and print the result to stdout: " +
		"command | sed 's/apple/mango/g' Replace all apple occurrences with mango in a file and " +
		"save a backup of the original: sed -i bak 's/apple/mango/g' path/to/file " +
		"Execute a specific script file and replace in-place: sed -i -f script.sed path/to/file " +
		"Replace the first occurrence of a regular expression in each line of a file: " +
		"sed -i 's/regex/replace/' path/to/file"

	b.ResetTimer()
	for b.Loop() {
		if _, err := emb.Embed(text); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEmbedLong measures embedding a long string (~500 tokens).
// This is the max-chars=500 default for file corpora.
func BenchmarkEmbedLong(b *testing.B) {
	emb := newBenchEmbedder(b)
	// Build a ~500 char string representative of Go source code.
	text := "search_document: package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
		"func main() {\n\tfmt.Println(\"hello world\")\n\tos.Exit(0)\n}\n\n" +
		"// Document represents a single item to be embedded and indexed.\n" +
		"type Document struct {\n\tKey   string\n\tHash  string\n\tTexts []string\n}\n\n" +
		"// Corpus provides documents for embedding and indexing.\n" +
		"type Corpus interface {\n\tName() string\n\tPrepare() error\n\t" +
		"Documents() iter.Seq2[Document, error]\n}\n\n" +
		"// ConfigHash computes a 12-char hex hash from the model config and corpus\n" +
		"// config fields that affect embedding content.\n" +
		"func ConfigHash(model ModelConfig, corpus CorpusConfig) string {\n\t" +
		"h := sha256.New()\n\tfmt.Fprintf(h, \"model-path:%s\\n\", model.Path)\n\t" +
		"return hex.EncodeToString(h.Sum(nil))[:12]\n}\n"

	b.ResetTimer()
	for b.Loop() {
		if _, err := emb.Embed(text); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCacheSaveLoad measures the JSONL serialization round-trip for
// a realistic index size (1000 entries with 1024-dim embeddings).
func BenchmarkCacheSaveLoad(b *testing.B) {
	entries := make([]CachedEntry, 1000)
	for i := range entries {
		emb := make([]float32, 1024)
		for j := range emb {
			emb[j] = float32(i*1024+j) * 0.001
		}
		entries[i] = CachedEntry{
			Key:       fmt.Sprintf("page-%d(1)", i),
			Hash:      fmt.Sprintf("%064x", i),
			Embedding: emb,
		}
	}

	b.Run("Save", func(b *testing.B) {
		dir := b.TempDir()
		b.ResetTimer()
		for b.Loop() {
			if err := SaveCachedEntries(filepath.Join(dir, "bench"), entries); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load", func(b *testing.B) {
		dir := filepath.Join(b.TempDir(), "bench")
		if err := SaveCachedEntries(dir, entries); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for b.Loop() {
			if _, err := LoadCachedEntries(dir); err != nil {
				b.Fatal(err)
			}
		}
	})
}
