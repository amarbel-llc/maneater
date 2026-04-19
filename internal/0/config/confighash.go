package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Hash computes a 12-char hex hash from the model config and corpus config
// fields that affect embedding content. Changing any of these values
// produces a different hash, which maps to a different cache directory.
func Hash(model ModelConfig, corpus CorpusConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "model-path:%s\n", model.Path)
	fmt.Fprintf(h, "document-prefix:%s\n", model.DocumentPrefix)
	fmt.Fprintf(h, "max-chars:%d\n", corpus.MaxChars)
	return hex.EncodeToString(h.Sum(nil))[:12]
}
