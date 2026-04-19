// Package main is maneater-man: the lean companion binary the default
// manpages corpus (internal/charlie/commands.defaultManpagesCorpusConfig)
// invokes via type="command" list/hash/read/prepare commands.
//
// It exists as a separate binary so the per-page subprocess spawn cost
// doesn't drag along the CGO + llama-cpp init that every spawn of the
// main `maneater` binary pays. See maneater#12 for the motivation and
// maneater#17 for broader derivation-granularity notes.
//
// Subcommands (all read MANEATER_MANPATH, colon-separated, as their manpath):
//
//	maneater-man list           — one page key per line to stdout.
//	maneater-man hash <page>    — hex sha256 of the source roff file (or blank if not found).
//	maneater-man read <page>    — synopsis + NUL + tldr to stdout (either may be empty).
//	maneater-man prepare        — one-shot setup (currently: warm tldr cache).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/amarbel-llc/maneater/internal/0/manpath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "maneater-man: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(sub string, args []string) error {
	switch sub {
	case "list":
		return runList()
	case "hash":
		return runHash(args)
	case "read":
		return runRead(args)
	case "prepare":
		return runPrepare()
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: maneater-man <subcommand> [args]

Subcommands:
  list              List man pages on MANEATER_MANPATH
  hash  <page>      Print hex SHA-256 of a page's source file
  read  <page>      Print synopsis + \0 + tldr
  prepare           Warm the tldr cache (one-shot)
`)
}

func runList() error {
	paths, err := resolveManpath()
	if err != nil {
		return err
	}
	pages, err := manpath.ListManPages(paths)
	if err != nil {
		return err
	}
	for _, p := range pages {
		fmt.Println(p)
	}
	return nil
}

func runHash(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("hash: missing page argument")
	}
	key := args[len(args)-1]
	paths, err := resolveManpath()
	if err != nil {
		return err
	}
	name, section := manpath.ParsePageKey(key)
	source, err := manpath.LocateSource(paths, section, name)
	if err != nil || source == "" {
		fmt.Println("")
		return nil
	}
	fmt.Println(manpath.HashFile(source))
	return nil
}

func runRead(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("read: missing page argument")
	}
	key := args[len(args)-1]
	paths, err := resolveManpath()
	if err != nil {
		return err
	}
	synopsis := manpath.ExtractSynopsis(paths, key)
	tldr := manpath.ExtractTldr(key)
	fmt.Printf("%s\x00%s", synopsis, tldr)
	return nil
}

func runPrepare() error {
	manpath.EnsureTldrCache()
	return nil
}

// resolveManpath reads MANEATER_MANPATH (colon-separated); falls back to the
// same resolution the main binary's index command uses when the env var
// is unset (rare — resolveCorpora in internal/charlie/commands sets it).
func resolveManpath() ([]string, error) {
	if raw := os.Getenv("MANEATER_MANPATH"); raw != "" {
		return strings.Split(raw, ":"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return manpath.Resolve(nil, false, cwd)
}
