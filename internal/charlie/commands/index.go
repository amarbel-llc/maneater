package commands

import (
	"context"
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/embedding"
	"github.com/amarbel-llc/maneater/internal/0/madder"
	"github.com/amarbel-llc/maneater/internal/0/manifest"
	"github.com/amarbel-llc/maneater/internal/alfa/corpus"
)

// RunIndex embeds every configured corpus and writes the resulting blobs to
// the configured madder store. Reads os.Args for the --force flag. Honors
// ctx cancellation between documents, between corpora, and in every
// subprocess shell-out (madder + command corpora). Emits TAP-14 progress to
// stdout: `ok N - <key>` per embedded document, `ok N - <key> # SKIP reused
// from blob store` per incremental reuse, `not ok N - <key>` per per-doc
// error, `bail out!` on fast-fails.
func RunIndex(ctx context.Context) error {
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" {
			force = true
		}
	}

	tw := tap.NewWriter(os.Stdout)

	cfg, err := config.LoadDefault()
	if err != nil {
		tw.BailOut(fmt.Sprintf("loading config: %v", err))
		return fmt.Errorf("loading config: %w", err)
	}

	modelName, modelCfg, err := config.ActiveModel(cfg)
	if err != nil {
		tw.BailOut(err.Error())
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		tw.BailOut(err.Error())
		return err
	}

	manPaths, err := resolveManpathFromConfig(cfg.Manpath, cwd)
	if err != nil {
		tw.BailOut(err.Error())
		return err
	}

	corpora, err := resolveCorpora(cfg, manPaths)
	if err != nil {
		tw.BailOut(err.Error())
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
		tw.BailOut(fmt.Sprintf("madder list: %v", err))
		return err
	}
	if !exists {
		msg := fmt.Sprintf("madder store %q is not initialized — run 'maneater init-store' first", store.StoreID)
		tw.BailOut(msg)
		return fmt.Errorf("%s", msg)
	}

	tw.Comment(fmt.Sprintf("using model %q from %s", modelName, modelCfg.Path))

	emb, err := embedding.NewEmbedder(modelCfg.Path)
	if err != nil {
		tw.BailOut(fmt.Sprintf("loading model: %v", err))
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
						tw.Comment(fmt.Sprintf("loaded %d entries from blob store for %s",
							len(existing), c.Name()))
					}
				}
			}
		}

		if err := c.Prepare(); err != nil {
			tw.BailOut(fmt.Sprintf("preparing corpus %s: %v", c.Name(), err))
			return fmt.Errorf("preparing corpus %s: %w", c.Name(), err)
		}

		var entries []embedding.CachedEntry
		var reusedCount, embeddedCount int

		for doc, docErr := range c.Documents() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if docErr != nil {
				tw.NotOk(fmt.Sprintf("%s/<unknown>", c.Name()),
					map[string]string{"message": docErr.Error()})
				continue
			}

			desc := fmt.Sprintf("%s/%s", c.Name(), doc.Key)

			// Check if we can reuse the existing entry.
			if cached, ok := existing[doc.Key]; ok && cached.Hash == doc.Hash {
				entries = append(entries, cached)
				reusedCount++
				tw.Skip(desc, "reused from blob store")
				continue
			}

			// Embed all text chunks for this document.
			docOK := true
			for _, text := range doc.Texts {
				if err := ctx.Err(); err != nil {
					return err
				}
				docText := modelCfg.DocumentPrefix + text
				vec, embErr := emb.Embed(docText)
				if embErr != nil {
					tw.NotOk(desc, map[string]string{
						"message": embErr.Error(),
						"stage":   "embed",
					})
					docOK = false
					continue
				}
				entries = append(entries, embedding.CachedEntry{
					Key:       doc.Key,
					Hash:      doc.Hash,
					Embedding: vec,
				})
			}
			if docOK {
				tw.Ok(desc)
			}
			embeddedCount++
		}

		meta := embedding.IndexMeta{
			ModelPath:      modelCfg.Path,
			DocumentPrefix: modelCfg.DocumentPrefix,
			ConfigHash:     cfgHash,
		}

		blob, err := embedding.MarshalIndexBlob(meta, entries)
		if err != nil {
			tw.BailOut(fmt.Sprintf("serializing index blob for %s: %v", c.Name(), err))
			return fmt.Errorf("serializing index blob for %s: %w", c.Name(), err)
		}
		digest, err := store.Write(ctx, blob)
		if err != nil {
			tw.BailOut(fmt.Sprintf("writing blob for %s: %v", c.Name(), err))
			return fmt.Errorf("writing blob for %s: %w", c.Name(), err)
		}
		if err := manifest.Save(dataDir, manifest.IndexManifest{
			BlobDigest: digest,
			ConfigHash: cfgHash,
		}); err != nil {
			tw.BailOut(fmt.Sprintf("saving manifest for %s: %v", c.Name(), err))
			return fmt.Errorf("saving manifest for %s: %w", c.Name(), err)
		}
		if err := embedding.SaveMeta(dataDir, meta); err != nil {
			tw.Comment(fmt.Sprintf("warning: could not save meta.json: %v", err))
		}

		tw.Comment(fmt.Sprintf("Done: %s — %d entries (%d reused, %d embedded) blob %s",
			c.Name(), len(entries), reusedCount, embeddedCount, digest))
	}

	tw.Plan()
	return nil
}
