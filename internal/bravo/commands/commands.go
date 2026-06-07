// Package commands implements the three top-level maneater subcommands —
// index, search, and init-store — plus the helpers they share. Wiring
// into command.App stays in cmd/maneater/main.go.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/manpath"
	"github.com/amarbel-llc/maneater/internal/alfa/corpus"
)

// indexDataDir returns the per-corpus on-disk cache path that holds the
// manifest + meta for a given config hash. Blob content lives in the
// content-addressed store; this directory only tracks "which digest
// corresponds to which corpus + config".
func indexDataDir(corpusName, configHash string) string {
	var base string
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		base = filepath.Join(xdg, "maneater", "index")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share", "maneater", "index")
	}
	return filepath.Join(base, corpusName, configHash)
}

// resolvedCorpus pairs a Corpus with the CorpusConfig it was built from. The
// config is needed for cache-hash computation (config.Hash) and for wiring
// the synthesized default manpages corpus, whose config doesn't appear in
// the user's TOML.
type resolvedCorpus struct {
	Corpus corpus.Corpus
	Config config.CorpusConfig
}

// resolveManpathFromConfig unwraps the optional ManpathConfig into the flat
// arguments manpath.Resolve expects.
func resolveManpathFromConfig(cfg *config.ManpathConfig, cwd string) ([]string, error) {
	var include []string
	var noAuto bool
	if cfg != nil {
		include = cfg.Include
		noAuto = cfg.NoAuto
	}
	return manpath.Resolve(include, noAuto, cwd)
}

// expandManpagesCorpus translates a `type = "manpages"` CorpusConfig into
// the equivalent type = "command" corpus shelling out to the lean
// `maneater-man` companion binary (no CGO, no llama-cpp init cost per
// subprocess spawn — see maneater#12). The same expansion (from a zero
// CorpusConfig) synthesizes the default corpus when the user's TOML has
// no [[corpora]] entries.
//
// User-set Name, Workers, MaxChars, and Model are honored; the command
// fields are rejected because they would silently fight the synthesized
// ones. MaxChars and Model pass through UNRESOLVED: config.Hash folds
// their raw values, and normalizing here would invalidate existing
// default-corpus cache digests. Manpath is passed through
// MANEATER_MANPATH (set by resolveCorpora before expansion).
func expandManpagesCorpus(cc config.CorpusConfig) (config.CorpusConfig, error) {
	name := cc.Name
	if name == "" {
		name = "manpages"
	}

	conflicts := []struct {
		set bool
		key string
	}{
		{len(cc.Paths) > 0, "paths"},
		{len(cc.ListCmd) > 0, "list-cmd"},
		{len(cc.ReadCmd) > 0, "read-cmd"},
		{len(cc.HashCmd) > 0, "hash-cmd"},
		{len(cc.PrepareCmd) > 0, "prepare-cmd"},
	}
	for _, c := range conflicts {
		if c.set {
			return config.CorpusConfig{}, fmt.Errorf(
				"corpus %q: type \"manpages\" does not accept %s; use type \"command\" for custom commands",
				name, c.key,
			)
		}
	}

	out := cc
	out.Name = name
	out.Type = "command"
	out.ListCmd = []string{"maneater-man", "list"}
	out.ReadCmd = []string{"maneater-man", "read"}
	out.HashCmd = []string{"maneater-man", "hash"}
	out.PrepareCmd = []string{"maneater-man", "prepare"}
	if out.Workers == 0 {
		out.Workers = 8
	}
	return out, nil
}

func resolveCorpora(cfg config.ManeaterConfig, manPaths []string) ([]resolvedCorpus, error) {
	// Hidden man-* subcommands read MANEATER_MANPATH. Set it here so both
	// the synthesized default corpus and any user-written corpus referencing
	// `maneater man-*` commands see the same manpath.
	os.Setenv("MANEATER_MANPATH", strings.Join(manPaths, ":"))

	ccs := cfg.Corpora
	if len(ccs) == 0 {
		ccs = []config.CorpusConfig{{Type: "manpages"}}
	}

	out := make([]resolvedCorpus, 0, len(ccs))
	for _, cc := range ccs {
		if cc.Type == "manpages" {
			expanded, err := expandManpagesCorpus(cc)
			if err != nil {
				return nil, err
			}
			cc = expanded
		}
		c, err := corpus.FromConfig(cc)
		if err != nil {
			return nil, fmt.Errorf("corpus %q: %w", cc.Name, err)
		}
		out = append(out, resolvedCorpus{Corpus: c, Config: cc})
	}
	return out, nil
}
