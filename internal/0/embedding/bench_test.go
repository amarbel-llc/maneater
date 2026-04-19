package embedding

import (
	"fmt"
	"os"
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

// BenchmarkBatchEmbed measures embedding N short texts in a single
// llama_encode call vs N separate calls, to quantify batching gains.
func BenchmarkBatchEmbed(b *testing.B) {
	emb := newBenchEmbedder(b)

	texts := []string{
		"search_document: ls - list directory contents",
		"search_document: cat - concatenate and print files",
		"search_document: grep - file pattern searcher",
		"search_document: sed - stream editor for filtering and transforming text",
		"search_document: awk - pattern-directed scanning and processing language",
	}

	b.Run("Sequential_5x", func(b *testing.B) {
		b.ResetTimer()
		for b.Loop() {
			for _, text := range texts {
				if _, err := emb.Embed(text); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("Batched_5x", func(b *testing.B) {
		b.ResetTimer()
		for b.Loop() {
			if _, err := emb.BatchEmbed(texts); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkChunkedEmbed compares embedding a long document as one call
// vs. splitting it into overlapping chunks and embedding each sequentially.
// This tests whether N small embeddings are faster than one large one,
// given the quadratic attention cost in BERT-style models.
//
// Results (snowflake-arctic-embed-l-v2.0, i7-1165G7, 2026-04-14):
//
//	Document: 226 tokens, chunked into 4 chunks of ~128 tokens (overlap 25)
//	Monolithic-8     3,877,489,846 ns/op  (3.88s)
//	Chunked_4x-8     6,695,235,119 ns/op  (6.70s)
//
// Chunking was ~1.7x SLOWER. At 226 tokens the per-call fixed overhead
// (CGo crossing, batch alloc, tokenization) dominates the quadratic
// attention savings. Chunking may only help for documents much closer
// to the 512-token context limit, if at all.
func BenchmarkChunkedEmbed(b *testing.B) {
	emb := newBenchEmbedder(b)

	// ~500 char document (same as BenchmarkEmbedLong).
	fullText := "search_document: package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
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

	// Chunk the text into overlapping windows by token count.
	// Target ~128 tokens per chunk with ~25 token overlap.
	chunkSize := 128
	overlap := 25
	chunks := chunkByTokens(b, emb, fullText, chunkSize, overlap)

	fullTokens, err := emb.Tokenize(fullText)
	if err != nil {
		b.Fatalf("Tokenize: %v", err)
	}
	b.Logf("full text: %d tokens, chunked into %d chunks of ~%d tokens (overlap %d)",
		fullTokens, len(chunks), chunkSize, overlap)
	for i, c := range chunks {
		n, _ := emb.Tokenize(c)
		b.Logf("  chunk %d: %d tokens, %d chars", i, n, len(c))
	}

	b.Run("Monolithic", func(b *testing.B) {
		b.ResetTimer()
		for b.Loop() {
			if _, err := emb.Embed(fullText); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run(fmt.Sprintf("Chunked_%dx", len(chunks)), func(b *testing.B) {
		b.ResetTimer()
		for b.Loop() {
			for _, chunk := range chunks {
				if _, err := emb.Embed(chunk); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// chunkByTokens splits text into overlapping chunks of approximately
// chunkSize tokens with overlap tokens shared between consecutive chunks.
// It uses the embedder's tokenizer to measure boundaries accurately,
// falling back to character-based splitting for the actual substring
// extraction (since we don't have token-to-char offset mapping).
func chunkByTokens(tb testing.TB, emb *Embedder, text string, chunkSize, overlap int) []string {
	tb.Helper()

	// Use a simple word-boundary approach: split into words, greedily
	// accumulate until we exceed chunkSize tokens, then step back by
	// overlap tokens worth of words.
	words := splitWords(text)
	if len(words) == 0 {
		return nil
	}

	var chunks []string
	start := 0
	for start < len(words) {
		// Grow window until we exceed chunkSize tokens or run out of words.
		end := start + 1
		for end <= len(words) {
			candidate := joinWords(words[start:end])
			n, err := emb.Tokenize(candidate)
			if err != nil {
				tb.Fatalf("Tokenize: %v", err)
			}
			if n > chunkSize && end > start+1 {
				end-- // back off one word
				break
			}
			end++
		}
		if end > len(words) {
			end = len(words)
		}

		chunks = append(chunks, joinWords(words[start:end]))

		// Advance by (chunk words - overlap words). Estimate overlap
		// words as a fraction of the chunk words.
		chunkWords := end - start
		overlapWords := chunkWords * overlap / chunkSize
		if overlapWords < 1 {
			overlapWords = 1
		}
		advance := chunkWords - overlapWords
		if advance < 1 {
			advance = 1
		}
		start += advance
	}

	return chunks
}

func splitWords(s string) []string {
	var words []string
	word := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(r)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

func joinWords(words []string) string {
	result := ""
	for i, w := range words {
		if i > 0 {
			result += " "
		}
		result += w
	}
	return result
}
