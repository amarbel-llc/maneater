package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/maneater/internal/config"
	"github.com/amarbel-llc/maneater/internal/corpus"
	"github.com/amarbel-llc/maneater/internal/embedding"
	"github.com/amarbel-llc/maneater/internal/madder"
	"github.com/amarbel-llc/maneater/internal/manifest"
)

// RunIndex embeds every configured corpus and writes the resulting blobs to
// the configured madder store. Reads os.Args for the --force flag. Honors
// ctx cancellation between documents, between corpora, and in every
// subprocess shell-out (madder + command corpora).
func RunIndex(ctx context.Context) error {
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" {
			force = true
		}
	}

	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	modelName, modelCfg, err := config.ActiveModel(cfg)
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

	for _, c := range corpora {
		if cmdc, ok := c.(*corpus.CommandCorpus); ok {
			cmdc.Ctx = ctx
		}
	}

	sc := config.ResolveStorage(cfg)
	store := &madder.Store{StoreID: sc.StoreID}

	exists, err := store.Exists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("madder store %q is not initialized — run 'maneater init-store' first", store.StoreID)
	}

	fmt.Printf("Using model %q from %s\n", modelName, modelCfg.Path)

	emb, err := embedding.NewEmbedder(modelCfg.Path)
	if err != nil {
		return fmt.Errorf("loading model: %w", err)
	}
	defer emb.Close()

	for _, c := range corpora {
		if err := ctx.Err(); err != nil {
			return err
		}

		cc := corpusConfigForCorpus(c, cfg)
		cfgHash := config.Hash(modelCfg, cc)
		dataDir := indexDataDir(c.Name(), cfgHash)

		// Load existing entries from blob store for incremental reuse.
		existing := make(map[string]embedding.CachedEntry)
		if !force {
			if man, err := manifest.Load(dataDir); err == nil && man.ConfigHash == cfgHash {
				if blob, err := store.Read(ctx, man.BlobDigest); err == nil {
					if _, cached, err := embedding.UnmarshalIndexBlob(blob); err == nil {
						for _, e := range cached {
							existing[e.Key] = e
						}
						fmt.Fprintf(os.Stderr, "maneater: loaded %d entries from blob store for %s\n",
							len(existing), c.Name())
					}
				}
			}
		}

		if err := c.Prepare(); err != nil {
			return fmt.Errorf("preparing corpus %s: %w", c.Name(), err)
		}

		var entries []embedding.CachedEntry
		var reusedCount, embeddedCount int

		for doc, docErr := range c.Documents() {
			if err := ctx.Err(); err != nil {
				return err
			}
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
				if err := ctx.Err(); err != nil {
					return err
				}
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
					c.Name(), total, reusedCount, embeddedCount)
			}
		}

		meta := embedding.IndexMeta{
			ModelPath:      modelCfg.Path,
			DocumentPrefix: modelCfg.DocumentPrefix,
			ConfigHash:     cfgHash,
		}

		blob, err := embedding.MarshalIndexBlob(meta, entries)
		if err != nil {
			return fmt.Errorf("serializing index blob for %s: %w", c.Name(), err)
		}
		digest, err := store.Write(ctx, blob)
		if err != nil {
			return fmt.Errorf("writing blob for %s: %w", c.Name(), err)
		}
		if err := manifest.Save(dataDir, manifest.IndexManifest{
			BlobDigest: digest,
			ConfigHash: cfgHash,
		}); err != nil {
			return fmt.Errorf("saving manifest for %s: %w", c.Name(), err)
		}
		if err := embedding.SaveMeta(dataDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "maneater: warning: could not save meta.json: %v\n", err)
		}

		fmt.Printf("Done: %s — %d entries (%d reused, %d embedded) blob %s\n",
			c.Name(), len(entries), reusedCount, embeddedCount, digest)
	}

	return nil
}
