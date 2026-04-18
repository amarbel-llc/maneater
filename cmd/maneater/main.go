package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/amarbel-llc/maneater/internal/commands"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
)

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
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return commands.RunIndex()
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
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return commands.RunSearch(os.Args[2:])
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
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return commands.RunInitStore()
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
