package embedding

import (
	"os"
	"strings"
	"testing"
)

// newEmbedderForTest skips unless MANPAGE_MODEL_PATH is set, builds an
// Embedder with default nCtx/pooling and the given truncate mode, and
// closes it via t.Cleanup.
func newEmbedderForTest(t *testing.T, truncate bool) *Embedder {
	t.Helper()
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		t.Skip("MANPAGE_MODEL_PATH not set")
	}
	emb, err := NewEmbedder(modelPath, 0, "", truncate)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	t.Cleanup(emb.Close)
	return emb
}

// oversizedText returns prose that verifiably tokenizes past emb's
// context window (checked via Tokenize rather than assumed).
func oversizedText(t *testing.T, emb *Embedder) string {
	t.Helper()
	text := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 200)
	nTokens, err := emb.Tokenize(text)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if nTokens <= emb.ContextSize() {
		t.Fatalf("test input only tokenizes to %d tokens (<= context %d); make it longer",
			nTokens, emb.ContextSize())
	}
	return text
}

func TestEmbedProducesNonZeroOutput(t *testing.T) {
	emb := newEmbedderForTest(t, false)

	queryPrefix, _ := testPrefixes()
	vec, err := emb.Embed(queryPrefix + "list files in a directory")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vec) == 0 {
		t.Fatal("embedding has zero length")
	}

	t.Logf("embedding dim: %d", len(vec))
	t.Logf("first 10 values: %v", vec[:10])

	allZero := true
	for _, v := range vec {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("embedding is all zeros")
	}
}

// TestEmbedOversizedInputReturnsError guards against the GGML_ASSERT
// abort: submitting a batch with more tokens than n_ubatch (= nCtx)
// kills the whole process inside llama.cpp ("encoder requires
// n_ubatch >= n_tokens"). Embed must reject oversized inputs with a
// Go error instead of letting them reach llama_decode/llama_encode.
func TestEmbedOversizedInputReturnsError(t *testing.T) {
	emb := newEmbedderForTest(t, false)
	text := oversizedText(t, emb)

	if _, err := emb.Embed(text); err == nil {
		t.Fatalf("Embed accepted input past context size %d; want error", emb.ContextSize())
	}
}

// TestEmbedOversizedInputTruncates is the truncate=true counterpart:
// the same oversized input embeds successfully using only the first
// nCtx tokens.
func TestEmbedOversizedInputTruncates(t *testing.T) {
	emb := newEmbedderForTest(t, true)
	text := oversizedText(t, emb)

	vec, err := emb.Embed(text)
	if err != nil {
		t.Fatalf("Embed with truncate=true: %v", err)
	}
	allZero := true
	for _, v := range vec {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("truncated embedding is all zeros")
	}
}

// TestEmbedLargeContextFitsOversizedDefault encodes the original crash
// scenario's fix: a text that overflows the default 512-token window
// embeds fine when the embedder is configured with a larger n-ctx
// (exercising the n-ctx-sized tokenize buffer rather than the 512
// default).
func TestEmbedLargeContextFitsOversizedDefault(t *testing.T) {
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		t.Skip("MANPAGE_MODEL_PATH not set")
	}

	emb, err := NewEmbedder(modelPath, 2048, "", false)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	t.Cleanup(emb.Close)

	// ~2000 chars of prose ≈ 600-700 tokens: past 512, well inside 2048.
	text := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 44)
	nTokens, err := emb.Tokenize(text)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if nTokens <= 512 || nTokens > emb.ContextSize() {
		t.Fatalf("test input tokenizes to %d tokens, want in (512, %d]", nTokens, emb.ContextSize())
	}

	vec, err := emb.Embed(text)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) == 0 {
		t.Fatal("embedding has zero length")
	}
}

func TestTrainedContextSize(t *testing.T) {
	emb := newEmbedderForTest(t, false)

	tc := emb.TrainedContextSize()
	t.Logf("trained context size: %d", tc)
	if tc <= 0 {
		t.Errorf("TrainedContextSize = %d, want > 0", tc)
	}
}

func TestEmbedSimilarQueriesMoreSimilar(t *testing.T) {
	emb := newEmbedderForTest(t, false)

	queryPrefix, docPrefix := testPrefixes()

	a, err := emb.Embed(queryPrefix + "list files")
	if err != nil {
		t.Fatalf("Embed a: %v", err)
	}

	b, err := emb.Embed(docPrefix + "ls - list directory contents")
	if err != nil {
		t.Fatalf("Embed b: %v", err)
	}

	c, err := emb.Embed(docPrefix + "gcc - GNU C compiler")
	if err != nil {
		t.Fatalf("Embed c: %v", err)
	}

	simAB := CosineSimilarity(a, b)
	simAC := CosineSimilarity(a, c)

	t.Logf("similarity(list files, ls): %.4f", simAB)
	t.Logf("similarity(list files, gcc): %.4f", simAC)

	if simAB <= simAC {
		t.Errorf("expected 'list files' closer to 'ls' than 'gcc', got %.4f <= %.4f", simAB, simAC)
	}
}
