package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/amarbel-llc/maneater/internal/blobstore"
	"github.com/amarbel-llc/maneater/internal/embedding"
	"github.com/amarbel-llc/maneater/internal/manifest"
	"github.com/amarbel-llc/maneater/internal/manpath"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
)

// searcher holds state for the embedding-based search pipeline.
type searcher struct {
	mu        sync.Mutex
	embedder  *embedding.Embedder
	index     *embedding.Index
	cfg       ManeaterConfig
	modelName string
	modelCfg  ModelConfig
	manpath   []string
	corpora   []Corpus
}

//go:embed maneater.1 maneater.toml.5
var manpages embed.FS

func newApp() *command.App {
	app := command.NewApp("maneater", "Man page search index and semantic search CLI")
	app.Version = "0.6.0"
	app.Description.Long = "Maneater builds and queries a semantic search index over Unix man pages " +
		"using vector embeddings. It extracts synopses and tldr descriptions, embeds " +
		"them with nomic-embed-text-v1.5, and supports ranked search by natural language query."

	app.ExtraManpages = []command.ManpageFile{
		{Source: manpages, Path: "maneater.1", Section: 1, Name: "maneater.1"},
		{Source: manpages, Path: "maneater.toml.5", Section: 5, Name: "maneater.toml.5"},
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
			return runIndex()
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
		RunCLI: func(_ context.Context, args json.RawMessage) error {
			return runSearch(os.Args[2:])
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
			return runInitStore()
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

func runSearch(args []string) error {
	topK := 10
	var queryParts []string

	for i := 0; i < len(args); i++ {
		if args[i] == "--top-k" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid --top-k value: %s", args[i+1])
			}
			topK = n
			i++
		} else {
			queryParts = append(queryParts, args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		return fmt.Errorf("usage: maneater search <query> [--top-k N]")
	}

	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	manPaths, err := resolveManpathFromConfig(cfg.Manpath, cwd)
	if err != nil {
		return err
	}

	corpora, err := resolveCorpora(cfg, manPaths)
	if err != nil {
		return err
	}

	s := &searcher{cfg: cfg, manpath: manPaths, corpora: corpora}
	result, err := s.handleSearch(query, topK)
	if err != nil {
		return err
	}

	fmt.Print(result)
	return nil
}

func (s *searcher) handleSearch(query string, topK int) (string, error) {
	if err := s.ensureSearchReady(); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queryText := s.modelCfg.QueryPrefix + query
	queryEmb, err := s.embedder.Embed(queryText)
	if err != nil {
		return "", fmt.Errorf("embedding query: %w", err)
	}

	results := s.index.Search(queryEmb, topK)

	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q (%d matches):\n\n", query, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (score: %.4f)\n", i+1, r.Page, r.Score)
	}

	return b.String(), nil
}

func (s *searcher) ensureSearchReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.embedder != nil && s.index != nil {
		return nil
	}

	if s.modelCfg.Path == "" {
		name, model, err := activeModelFromConfig(s.cfg)
		if err != nil {
			return err
		}
		s.modelName = name
		s.modelCfg = model
	}

	if s.embedder == nil {
		emb, err := embedding.NewEmbedder(s.modelCfg.Path)
		if err != nil {
			return fmt.Errorf("loading embedding model: %w", err)
		}
		s.embedder = emb
	}

	if s.index == nil {
		idx, err := s.loadOrBuildIndex()
		if err != nil {
			return fmt.Errorf("building search index: %w", err)
		}
		s.index = idx
	}

	return nil
}

func (s *searcher) loadOrBuildIndex() (*embedding.Index, error) {
	sc := resolveStorage(s.cfg)
	store := &blobstore.CommandBlobStore{ReadCmd: sc.ReadCmd, WriteCmd: sc.WriteCmd}

	var combined *embedding.Index

	for _, corpus := range s.corpora {
		cc := corpusConfigForCorpus(corpus, s.cfg)
		cfgHash := ConfigHash(s.modelCfg, cc)
		dataDir := indexDataDir(corpus.Name(), cfgHash)

		man, err := manifest.Load(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: no index for %s (run 'maneater index' to build)\n",
				corpus.Name())
			continue
		}
		if man.ConfigHash != cfgHash {
			fmt.Fprintf(os.Stderr, "maneater: stale index for %s (run 'maneater index' to rebuild)\n",
				corpus.Name())
			continue
		}

		blob, err := store.Read(man.BlobDigest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: could not fetch %s from blob store: %v\n",
				corpus.Name(), err)
			continue
		}

		_, entries, err := embedding.UnmarshalIndexBlob(blob)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: could not deserialize %s index: %v\n",
				corpus.Name(), err)
			continue
		}

		fmt.Fprintf(os.Stderr, "maneater: loaded %s index (%d entries) from blob store\n",
			corpus.Name(), len(entries))

		idx := embedding.NewIndex(0)
		for _, e := range entries {
			idx.Add(e.Key, e.Embedding)
		}
		if len(entries) > 0 {
			idx.Dim = len(entries[0].Embedding)
		}

		if combined == nil {
			combined = idx
		} else {
			combined.Entries = append(combined.Entries, idx.Entries...)
		}
	}

	if combined == nil {
		combined = embedding.NewIndex(0)
	}

	return combined, nil
}

type pageText struct {
	index    int
	page     string
	hash     string
	synopsis string
	tldr     string
}

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
func corpusConfigForCorpus(c Corpus, cfg ManeaterConfig) CorpusConfig {
	for _, cc := range cfg.Corpora {
		if cc.Name == c.Name() {
			return cc
		}
	}
	return CorpusConfig{}
}

// resolveManpathFromConfig unwraps the optional ManpathConfig into the flat
// arguments manpath.Resolve expects.
func resolveManpathFromConfig(cfg *ManpathConfig, cwd string) ([]string, error) {
	var include []string
	var noAuto bool
	if cfg != nil {
		include = cfg.Include
		noAuto = cfg.NoAuto
	}
	return manpath.Resolve(include, noAuto, cwd)
}

// manpagesCorpus wraps the existing man-page discovery and extraction
// functions as a Corpus. This is an adapter — the extraction logic stays
// in main.go for now and will move to corpus_man.go later.
type manpagesCorpus struct {
	manpath []string
}

func (c *manpagesCorpus) Name() string { return "manpages" }

func (c *manpagesCorpus) Prepare() error {
	manpath.EnsureTldrCache()
	return nil
}

func (c *manpagesCorpus) Documents() iter.Seq2[Document, error] {
	return func(yield func(Document, error) bool) {
		pages, err := manpath.ListManPages(c.manpath)
		if err != nil {
			yield(Document{}, err)
			return
		}

		// Extract text concurrently using the existing worker pipeline.
		texts := make(chan pageText, 32)

		go func() {
			defer close(texts)

			type indexed struct {
				pt  pageText
				seq int
			}

			workers := 8
			sem := make(chan struct{}, workers)
			results := make(chan indexed, 32)

			go func() {
				defer close(results)
				var wg sync.WaitGroup
				for i, page := range pages {
					wg.Add(1)
					sem <- struct{}{}
					go func(seq int, page string) {
						defer wg.Done()
						defer func() { <-sem }()
						name, section := manpath.ParsePageKey(page)
						sourcePath, _ := manpath.LocateSource(c.manpath, section, name)
						fileHash := ""
						if sourcePath != "" {
							fileHash = manpath.HashFile(sourcePath)
						}
						results <- indexed{
							pt: pageText{
								index:    seq,
								page:     page,
								hash:     fileHash,
								synopsis: manpath.ExtractSynopsis(c.manpath, page),
								tldr:     manpath.ExtractTldr(page),
							},
							seq: seq,
						}
					}(i, page)
				}
				wg.Wait()
			}()

			pending := make(map[int]pageText)
			next := 0
			for r := range results {
				pending[r.seq] = r.pt
				for {
					pt, ok := pending[next]
					if !ok {
						break
					}
					delete(pending, next)
					texts <- pt
					next++
				}
			}
		}()

		for pt := range texts {
			var chunks []string
			if pt.synopsis != "" {
				chunks = append(chunks, pt.synopsis)
			}
			if pt.tldr != "" {
				chunks = append(chunks, pt.tldr)
			}
			if len(chunks) == 0 {
				continue
			}
			if !yield(Document{Key: pt.page, Hash: pt.hash, Texts: chunks}, nil) {
				return
			}
		}
	}
}

func resolveCorpora(cfg ManeaterConfig, manpath []string) ([]Corpus, error) {
	// If no corpora configured, use implicit manpages corpus.
	if len(cfg.Corpora) == 0 {
		return []Corpus{&manpagesCorpus{manpath: manpath}}, nil
	}

	// When corpora are explicitly configured, only those are used.
	// Add type = "manpages" to include man pages.
	var corpora []Corpus

	for _, cc := range cfg.Corpora {
		if cc.Type == "manpages" {
			corpora = append(corpora, &manpagesCorpus{manpath: manpath})
			continue
		}
		c, err := corpusFromConfig(cc)
		if err != nil {
			return nil, fmt.Errorf("corpus %q: %w", cc.Name, err)
		}
		corpora = append(corpora, c)
	}

	return corpora, nil
}

func runIndex() error {
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" {
			force = true
		}
	}

	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	modelName, modelCfg, err := activeModelFromConfig(cfg)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	manPaths, err := resolveManpathFromConfig(cfg.Manpath, cwd)
	if err != nil {
		return err
	}

	corpora, err := resolveCorpora(cfg, manPaths)
	if err != nil {
		return err
	}

	fmt.Printf("Using model %q from %s\n", modelName, modelCfg.Path)

	emb, err := embedding.NewEmbedder(modelCfg.Path)
	if err != nil {
		return fmt.Errorf("loading model: %w", err)
	}
	defer emb.Close()

	sc := resolveStorage(cfg)
	store := &blobstore.CommandBlobStore{ReadCmd: sc.ReadCmd, WriteCmd: sc.WriteCmd}

	for _, corpus := range corpora {
		cc := corpusConfigForCorpus(corpus, cfg)
		cfgHash := ConfigHash(modelCfg, cc)
		dataDir := indexDataDir(corpus.Name(), cfgHash)

		// Load existing entries from blob store for incremental reuse.
		existing := make(map[string]embedding.CachedEntry)
		if !force {
			if man, err := manifest.Load(dataDir); err == nil && man.ConfigHash == cfgHash {
				if blob, err := store.Read(man.BlobDigest); err == nil {
					if _, cached, err := embedding.UnmarshalIndexBlob(blob); err == nil {
						for _, e := range cached {
							existing[e.Key] = e
						}
						fmt.Fprintf(os.Stderr, "maneater: loaded %d entries from blob store for %s\n",
							len(existing), corpus.Name())
					}
				}
			}
		}

		if err := corpus.Prepare(); err != nil {
			return fmt.Errorf("preparing corpus %s: %w", corpus.Name(), err)
		}

		var entries []embedding.CachedEntry
		var reusedCount, embeddedCount int

		for doc, docErr := range corpus.Documents() {
			if docErr != nil {
				fmt.Fprintf(os.Stderr, "maneater: skipping document: %v\n", docErr)
				continue
			}

			// Check if we can reuse the existing entry.
			if cached, ok := existing[doc.Key]; ok && cached.Hash == doc.Hash {
				entries = append(entries, cached)
				reusedCount++
				continue
			}

			// Embed all text chunks for this document.
			for _, text := range doc.Texts {
				docText := modelCfg.DocumentPrefix + text
				vec, embErr := emb.Embed(docText)
				if embErr != nil {
					fmt.Fprintf(os.Stderr, "maneater: skipping %s: %v\n", doc.Key, embErr)
					continue
				}
				entries = append(entries, embedding.CachedEntry{
					Key:       doc.Key,
					Hash:      doc.Hash,
					Embedding: vec,
				})
			}
			embeddedCount++

			total := reusedCount + embeddedCount
			if total%100 == 0 {
				fmt.Fprintf(os.Stderr, "maneater: [%s] processed %d documents (%d reused, %d embedded)\n",
					corpus.Name(), total, reusedCount, embeddedCount)
			}
		}

		meta := embedding.IndexMeta{
			ModelPath:      modelCfg.Path,
			DocumentPrefix: modelCfg.DocumentPrefix,
			ConfigHash:     cfgHash,
		}

		blob, err := embedding.MarshalIndexBlob(meta, entries)
		if err != nil {
			return fmt.Errorf("serializing index blob for %s: %w", corpus.Name(), err)
		}
		digest, err := store.Write(blob)
		if err != nil {
			return fmt.Errorf("writing blob for %s: %w\nRun 'maneater init-store' to initialize the madder store.", corpus.Name(), err)
		}
		if err := manifest.Save(dataDir, manifest.IndexManifest{
			BlobDigest: digest,
			ConfigHash: cfgHash,
		}); err != nil {
			return fmt.Errorf("saving manifest for %s: %w", corpus.Name(), err)
		}
		if err := embedding.SaveMeta(dataDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "maneater: warning: could not save meta.json: %v\n", err)
		}

		fmt.Printf("Done: %s — %d entries (%d reused, %d embedded) blob %s\n",
			corpus.Name(), len(entries), reusedCount, embeddedCount, digest)
	}

	return nil
}

func runInitStore() error {
	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	sc := resolveStorage(cfg)

	listed, err := exec.Command("madder", "list").Output()
	if err != nil {
		return fmt.Errorf("could not list madder stores: %w\nIs madder installed and on PATH?", err)
	}

	for _, line := range strings.Split(string(listed), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == sc.StoreID {
			fmt.Printf("Madder store %q already exists.\n", sc.StoreID)
			return nil
		}
	}

	cmd := exec.Command("madder", "init", sc.StoreID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("madder init %s: %w", sc.StoreID, err)
	}

	fmt.Printf("Initialized madder store %q.\n", sc.StoreID)
	return nil
}
