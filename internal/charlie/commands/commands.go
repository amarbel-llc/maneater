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

// defaultManpagesCorpusConfig synthesizes the CorpusConfig that maneater
// uses when the user's TOML has no [[corpora]] entries. It is a plain
// type = "command" corpus that shells out to this binary's hidden
// man-{list,hash,read,prepare} subcommands. Manpath is passed through
// MANEATER_MANPATH (set by resolveCorpora before this function returns).
func defaultManpagesCorpusConfig() config.CorpusConfig {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "maneater"
	}
	return config.CorpusConfig{
		Name:       "manpages",
		Type:       "command",
		ListCmd:    []string{exe, "man-list"},
		ReadCmd:    []string{exe, "man-read"},
		HashCmd:    []string{exe, "man-hash"},
		PrepareCmd: []string{exe, "man-prepare"},
		Workers:    8,
	}
}

func resolveCorpora(cfg config.ManeaterConfig, manPaths []string) ([]resolvedCorpus, error) {
	// Hidden man-* subcommands read MANEATER_MANPATH. Set it here so both
	// the synthesized default corpus and any user-written corpus referencing
	// `maneater man-*` commands see the same manpath.
	os.Setenv("MANEATER_MANPATH", strings.Join(manPaths, ":"))

	ccs := cfg.Corpora
	if len(ccs) == 0 {
		ccs = []config.CorpusConfig{defaultManpagesCorpusConfig()}
	}

	out := make([]resolvedCorpus, 0, len(ccs))
	for _, cc := range ccs {
		c, err := corpus.FromConfig(cc)
		if err != nil {
			return nil, fmt.Errorf("corpus %q: %w", cc.Name, err)
		}
		out = append(out, resolvedCorpus{Corpus: c, Config: cc})
	}
	return out, nil
}
