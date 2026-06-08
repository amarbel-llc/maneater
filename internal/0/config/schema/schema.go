// Package schema defines maneater's typed configuration structs and the
// tommy-generated TOML codec for them. It is deliberately a leaf package with
// no consumers so that `tommy generate` can regenerate config_tommy.go in
// place: the generator requires the codegen-target package to type-check, and
// co-locating the struct with its loader/merge logic (package config) would
// reintroduce the tommy #93 bootstrap catch-22. Package config re-exports
// these types via aliases, so callers continue to use config.ManeaterConfig
// et al.
package schema

import "fmt"

//go:generate tommy generate
type ManeaterConfig struct {
	Default string                 `toml:"default"`
	Models  map[string]ModelConfig `toml:"models"`
	Manpath *ManpathConfig         `toml:"manpath"`
	Storage *StorageConfig         `toml:"storage"`
	Corpora []CorpusConfig         `toml:"corpora"`
}

// CorpusConfig is one [[corpora]] entry. Type, Name, and Paths /
// ListCmd / ReadCmd are the structural shape; MaxChars, Workers, and
// the optional hooks are knobs.
//
// Model (toml: model) names the [models.<name>] entry to embed this
// corpus's documents and queries with. Empty falls back to
// cfg.Default. See FDR-0001 (smart-retrieval corpus profile).
type CorpusConfig struct {
	Name       string   `toml:"name"`
	Type       string   `toml:"type"` // "files", "command", or "manpages" (expanded to "command" at resolve time)
	Paths      []string `toml:"paths"`
	MaxChars   int      `toml:"max-chars"`
	ListCmd    []string `toml:"list-cmd"`
	ReadCmd    []string `toml:"read-cmd"`
	HashCmd    []string `toml:"hash-cmd"`    // optional; per-key probe
	PrepareCmd []string `toml:"prepare-cmd"` // optional; once during Prepare
	Workers    int      `toml:"workers"`     // 0 or 1 = serial; >1 = worker pool
	Model      string   `toml:"model"`       // optional; overrides cfg.Default
}

// ManpathConfig controls how maneater discovers man pages beyond the system
// manpath. Include paths are prepended to the system manpath. When NoAuto is
// false (the default), maneater also probes common in-repo locations (man/,
// doc/man/, share/man/) in the current working directory.
type ManpathConfig struct {
	Include []string `toml:"include"`
	NoAuto  bool     `toml:"no-auto"`
}

// ModelConfig is one [models.<name>] entry. Path is required; the
// remaining fields tune embedding behavior.
//
// NCtx (toml: n-ctx) sets llama_context_default_params.n_ctx for this
// model. Defaults to 512 when zero/unset, preserving the historical
// behavior. Larger values raise quality on long documents at memory +
// latency cost; see FDR-0001 (smart-retrieval corpus profile).
//
// Pooling (toml: pooling) selects llama_pooling_type: "" (model
// default), "mean", "cls", or "last". Decoder-LLM-as-encoder models
// (Qwen3-Embedding, Mistral-derived) typically need "last".
//
// Truncate (toml: truncate) controls what happens when a text
// tokenizes to more than NCtx tokens: false (the default) rejects the
// text with a per-document error; true silently embeds only the first
// NCtx tokens. Truncation changes embedding content, so it
// participates in config.Hash.
type ModelConfig struct {
	Path           string `toml:"path"`
	QueryPrefix    string `toml:"query-prefix"`
	DocumentPrefix string `toml:"document-prefix"`
	NCtx           int    `toml:"n-ctx"`
	Pooling        string `toml:"pooling"`
	Truncate       bool   `toml:"truncate"`
}

// ResolvedNCtx returns the effective context size: m.NCtx when
// positive, otherwise 512 (the maneater historical default).
func (m ModelConfig) ResolvedNCtx() int {
	if m.NCtx > 0 {
		return m.NCtx
	}
	return 512
}

// DefaultMaxChars is the per-chunk character budget corpora fall back
// to when max-chars is zero/unset. The corpus implementations apply
// it; it lives here so config-level validation (chunk budget vs model
// context window) agrees with them on the effective value.
const DefaultMaxChars = 500

// ResolvedMaxChars returns the effective per-chunk character budget:
// c.MaxChars when positive, otherwise DefaultMaxChars.
func (c CorpusConfig) ResolvedMaxChars() int {
	if c.MaxChars > 0 {
		return c.MaxChars
	}
	return DefaultMaxChars
}

// ValidatePooling returns nil if m.Pooling is one of the accepted
// values, otherwise an error naming the offender. Empty string ("")
// is accepted and means "use the model's GGUF-declared default".
func (m ModelConfig) ValidatePooling() error {
	switch m.Pooling {
	case "", "mean", "cls", "last":
		return nil
	default:
		return fmt.Errorf("unknown pooling type %q (want one of \"\", \"mean\", \"cls\", \"last\")", m.Pooling)
	}
}

// StorageConfig controls how maneater persists and retrieves index blobs.
// The default (empty) config selects the builtin madder CLI backend with
// StoreID "maneater". Populating the *Cmd fields swaps the backend for a
// generic "shell out to configured commands" implementation — see
// internal/0/storage for the contract.
type StorageConfig struct {
	StoreID   string   `toml:"store-id"`
	ReadCmd   []string `toml:"read-cmd"`
	WriteCmd  []string `toml:"write-cmd"`
	ExistsCmd []string `toml:"exists-cmd"`
	InitCmd   []string `toml:"init-cmd"`
}
