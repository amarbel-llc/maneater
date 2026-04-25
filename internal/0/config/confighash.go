package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Hash computes a 12-char hex hash from the model config and corpus config
// fields that affect embedding content. Changing any of these values
// produces a different hash, which maps to a different cache directory.
//
// Inputs folded into the digest:
//   - model.Path
//   - model.DocumentPrefix
//   - model.NCtx (resolved via ResolvedNCtx so 0 collapses to 512 — the
//     historical default — and old caches round-trip)
//   - model.Pooling
//   - corpus.MaxChars
//   - corpus.Model (the [models.<name>] selector; cache invalidates if
//     a corpus is repointed at a different model entry without changing
//     the model's underlying path)
func Hash(model ModelConfig, corpus CorpusConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "model-path:%s\n", model.Path)
	fmt.Fprintf(h, "document-prefix:%s\n", model.DocumentPrefix)
	fmt.Fprintf(h, "n-ctx:%d\n", model.ResolvedNCtx())
	fmt.Fprintf(h, "pooling:%s\n", model.Pooling)
	fmt.Fprintf(h, "max-chars:%d\n", corpus.MaxChars)
	fmt.Fprintf(h, "corpus-model:%s\n", corpus.Model)
	return hex.EncodeToString(h.Sum(nil))[:12]
}
