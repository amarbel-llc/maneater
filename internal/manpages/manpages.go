// Package manpages provides a Corpus implementation that indexes Unix man
// pages. It is a thin adapter: the heavy lifting (manpath walking, synopsis
// and tldr extraction, source location) lives in internal/manpath.
package manpages

import (
	"iter"
	"sync"

	"github.com/amarbel-llc/maneater/internal/corpus"
	"github.com/amarbel-llc/maneater/internal/manpath"
)

// Corpus is the manpages Corpus implementation. Construct with New.
type Corpus struct {
	manpath []string
}

// New returns a Corpus that scans the given manpath directories.
func New(manpath []string) *Corpus {
	return &Corpus{manpath: manpath}
}

func (c *Corpus) Name() string { return "manpages" }

func (c *Corpus) Prepare() error {
	manpath.EnsureTldrCache()
	return nil
}

type pageText struct {
	index    int
	page     string
	hash     string
	synopsis string
	tldr     string
}

func (c *Corpus) Documents() iter.Seq2[corpus.Document, error] {
	return func(yield func(corpus.Document, error) bool) {
		pages, err := manpath.ListManPages(c.manpath)
		if err != nil {
			yield(corpus.Document{}, err)
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
			if !yield(corpus.Document{Key: pt.page, Hash: pt.hash, Texts: chunks}, nil) {
				return
			}
		}
	}
}
