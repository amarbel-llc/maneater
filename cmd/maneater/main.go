package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/amarbel-llc/maneater/internal/0/manpath"
	"github.com/amarbel-llc/maneater/internal/charlie/commands"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
)

// manpathFromEnv returns the manpath the `man-*` hidden subcommands should
// scan. MANEATER_MANPATH (colon-separated) takes precedence; otherwise fall
// back to the same resolution the index command uses.
func manpathFromEnv() ([]string, error) {
	if raw := os.Getenv("MANEATER_MANPATH"); raw != "" {
		return strings.Split(raw, ":"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return manpath.Resolve(nil, false, cwd)
}

//go:embed maneater.1 maneater.toml.5
var embeddedManpages embed.FS

func newApp() *command.App {
	app := command.NewApp("maneater", "Man page search index and semantic search CLI")
	app.Version = "0.6.0"
	app.Description.Long = "Maneater builds and queries a semantic search index over Unix man pages " +
		"using vector embeddings. It extracts synopses and tldr descriptions, embeds " +
		"them with nomic-embed-text-v1.5, and supports ranked search by natural language query."

	app.ExtraManpages = []command.ManpageFile{
		{Source: embeddedManpages, Path: "maneater.1", Section: 1, Name: "maneater.1"},
		{Source: embeddedManpages, Path: "maneater.toml.5", Section: 5, Name: "maneater.toml.5"},
	}

	app.Examples = []command.Example{
		{
			Description: "Build or rebuild the search index",
			Command:     "maneater index",
		},
		{
			Description: "Search for man pages about listing files",
			Command:     "maneater search list files",
		},
		{
			Description: "Search with custom result count",
			Command:     "maneater search --top-k 20 configure network",
		},
	}

	app.AddCommand(&command.Command{
		Name: "index",
		Description: command.Description{
			Short: "Build or rebuild the search index",
			Long: "Loads the embedding model, scans all configured corpora, " +
				"embeds their documents, and saves the index to the XDG cache directory. " +
				"Unchanged documents are skipped (incremental). Use --force for a full rebuild.",
		},
		Params: []command.Param{
			{
				Name:        "force",
				Description: "Force full rebuild, ignoring cached entries",
				Type:        command.Bool,
			},
		},
		RunCLI: func(ctx context.Context, _ json.RawMessage) error {
			return commands.RunIndex(ctx)
		},
	})

	app.AddCommand(&command.Command{
		Name: "search",
		Description: command.Description{
			Short: "Semantic man page search",
			Long: "Search for man pages by natural language query. Returns ranked " +
				"results with page names and cosine similarity scores. Requires a " +
				"pre-built index (maneater index).",
		},
		Params: []command.Param{
			{
				Name:        "top-k",
				Description: "Number of results to return",
				Type:        command.Int,
			},
			{
				Name:        "query",
				Description: "Natural language search query",
				Type:        command.String,
				Required:    true,
			},
		},
		RunCLI: func(ctx context.Context, _ json.RawMessage) error {
			return commands.RunSearch(ctx, os.Args[2:])
		},
	})

	app.AddCommand(&command.Command{
		Name: "init-store",
		Description: command.Description{
			Short: "Initialize the blob storage backend",
			Long: "Sets up the default madder content-addressed blob store for sharing " +
				"indexes across machines. Creates an XDG user store with the configured " +
				"store ID (default: 'maneater'). Safe to run multiple times.",
		},
		RunCLI: func(ctx context.Context, _ json.RawMessage) error {
			return commands.RunInitStore(ctx)
		},
	})

	// Hidden man-* subcommands are the contract the synthesized default
	// manpages corpus invokes via type = "command". They wrap
	// internal/0/manpath helpers with CLI-shaped I/O. MANEATER_MANPATH
	// (colon-separated) configures the scan root; when unset, the same
	// resolution as `maneater index` is used.

	app.AddCommand(&command.Command{
		Name:   "man-list",
		Hidden: true,
		Description: command.Description{
			Short: "List man pages on MANEATER_MANPATH (for default corpus)",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			paths, err := manpathFromEnv()
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
		},
	})

	app.AddCommand(&command.Command{
		Name:   "man-hash",
		Hidden: true,
		Description: command.Description{
			Short: "Print hex SHA-256 of a man page's source file (for default corpus hash-cmd)",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			if len(os.Args) < 3 {
				return fmt.Errorf("usage: maneater man-hash <page>")
			}
			key := os.Args[len(os.Args)-1]
			paths, err := manpathFromEnv()
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
		},
	})

	app.AddCommand(&command.Command{
		Name:   "man-read",
		Hidden: true,
		Description: command.Description{
			Short: "Extract synopsis + tldr for a man page (NUL-delimited)",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			if len(os.Args) < 3 {
				return fmt.Errorf("usage: maneater man-read <page>")
			}
			key := os.Args[len(os.Args)-1]
			paths, err := manpathFromEnv()
			if err != nil {
				return err
			}
			synopsis := manpath.ExtractSynopsis(paths, key)
			tldr := manpath.ExtractTldr(key)
			// CommandCorpus splits stdout on \0 into chunks. Emit both
			// regardless of emptiness; splitChunks drops empty segments.
			fmt.Printf("%s\x00%s", synopsis, tldr)
			return nil
		},
	})

	app.AddCommand(&command.Command{
		Name:   "man-prepare",
		Hidden: true,
		Description: command.Description{
			Short: "One-time setup for the default manpages corpus (warms tldr cache)",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			manpath.EnsureTldrCache()
			return nil
		},
	})

	return app
}

func main() {
	app := newApp()

	if len(os.Args) >= 2 && os.Args[1] == "generate-plugin" {
		if err := app.HandleGeneratePlugin(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := app.RunCLI(ctx, os.Args[1:], command.StubPrompter{}); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}
}
