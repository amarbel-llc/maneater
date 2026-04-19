// Package commands implements the three top-level maneater subcommands —
// index, search, and init-store — plus the helpers they share. Wiring
// into command.App stays in cmd/maneater/main.go.
package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/manpath"
	"github.com/amarbel-llc/maneater/internal/alfa/corpus"
	"github.com/amarbel-llc/maneater/internal/bravo/manpages"
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

// corpusConfigForCorpus returns the CorpusConfig matching a corpus by name.
// Returns a zero-value CorpusConfig for the implicit manpages corpus.
func corpusConfigForCorpus(c corpus.Corpus, cfg config.ManeaterConfig) config.CorpusConfig {
	for _, cc := range cfg.Corpora {
		if cc.Name == c.Name() {
			return cc
		}
	}
	return config.CorpusConfig{}
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

func resolveCorpora(cfg config.ManeaterConfig, manPaths []string) ([]corpus.Corpus, error) {
	// If no corpora configured, use implicit manpages corpus.
	if len(cfg.Corpora) == 0 {
		return []corpus.Corpus{manpages.New(manPaths)}, nil
	}

	// When corpora are explicitly configured, only those are used.
	// Add type = "manpages" to include man pages.
	var corpora []corpus.Corpus

	for _, cc := range cfg.Corpora {
		if cc.Type == "manpages" {
			corpora = append(corpora, manpages.New(manPaths))
			continue
		}
		c, err := corpus.FromConfig(cc)
		if err != nil {
			return nil, fmt.Errorf("corpus %q: %w", cc.Name, err)
		}
		corpora = append(corpora, c)
	}

	return corpora, nil
}
