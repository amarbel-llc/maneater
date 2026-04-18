package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/amarbel-llc/maneater/internal/config"
	"github.com/amarbel-llc/maneater/internal/corpus"
	"github.com/amarbel-llc/maneater/internal/embedding"
	"github.com/amarbel-llc/maneater/internal/madder"
	"github.com/amarbel-llc/maneater/internal/manifest"
)

// searcher holds state for the embedding-based search pipeline.
type searcher struct {
	mu        sync.Mutex
	embedder  *embedding.Embedder
	index     *embedding.Index
	cfg       config.ManeaterConfig
	modelName string
	modelCfg  config.ModelConfig
	manpath   []string
	corpora   []corpus.Corpus
}

// RunSearch parses args ("<query words...> [--top-k N]"), loads the index,
// embeds the query, and prints ranked results to stdout.
func RunSearch(args []string) error {
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

	cfg, err := config.LoadDefault()
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
		name, model, err := config.ActiveModel(s.cfg)
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
	sc := config.ResolveStorage(s.cfg)
	store := &madder.Store{StoreID: sc.StoreID}

	var combined *embedding.Index

	for _, c := range s.corpora {
		cc := corpusConfigForCorpus(c, s.cfg)
		cfgHash := config.Hash(s.modelCfg, cc)
		dataDir := indexDataDir(c.Name(), cfgHash)

		man, err := manifest.Load(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: no index for %s (run 'maneater index' to build)\n",
				c.Name())
			continue
		}
		if man.ConfigHash != cfgHash {
			fmt.Fprintf(os.Stderr, "maneater: stale index for %s (run 'maneater index' to rebuild)\n",
				c.Name())
			continue
		}

		blob, err := store.Read(man.BlobDigest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: could not fetch %s from blob store: %v\n",
				c.Name(), err)
			continue
		}

		_, entries, err := embedding.UnmarshalIndexBlob(blob)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: could not deserialize %s index: %v\n",
				c.Name(), err)
			continue
		}

		fmt.Fprintf(os.Stderr, "maneater: loaded %s index (%d entries) from blob store\n",
			c.Name(), len(entries))

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
