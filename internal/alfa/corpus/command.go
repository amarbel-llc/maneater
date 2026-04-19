package corpus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"iter"
	"os/exec"
	"strings"
	"sync"

	"github.com/amarbel-llc/maneater/internal/0/execx"
)

// CommandCorpus indexes documents via a pair-or-quartet of external commands.
//
//   - ListCmd outputs one document key per line to stdout (called once).
//   - ReadCmd receives a key as its final argument and outputs the document
//     text to stdout. Multiple text chunks may be NUL-separated.
//   - HashCmd (optional) receives a key and outputs a hex hash. When set, the
//     corpus probes this first and — if the hash matches prev[key] — yields a
//     Document with Texts == nil to signal "reuse cached entry" without
//     running the (expensive) ReadCmd.
//   - PrepareCmd (optional) runs once during Prepare().
//
// Workers > 1 enables a worker pool for ReadCmd / HashCmd dispatch, with an
// internal reorder buffer so Documents() still yields in ListCmd's output
// order (keeps blob digests stable for content-addressable dedup).
//
// Callers that want SIGINT cancellation to propagate to the external
// commands should set Ctx before iterating. A zero-value Ctx falls back to
// context.Background.
type CommandCorpus struct {
	CorpusName string
	ListCmd    []string
	ReadCmd    []string
	HashCmd    []string
	PrepareCmd []string
	MaxChars   int
	Workers    int
	Ctx        context.Context
}

func (c *CommandCorpus) Name() string { return c.CorpusName }

func (c *CommandCorpus) Prepare() error {
	if len(c.PrepareCmd) == 0 {
		return nil
	}
	if _, err := runCmd(c.contextOrBackground(), c.PrepareCmd); err != nil {
		return fmt.Errorf("prepare-cmd: %w", err)
	}
	return nil
}

func (c *CommandCorpus) Documents(prev map[string]string) iter.Seq2[Document, error] {
	return func(yield func(Document, error) bool) {
		maxChars := c.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}

		ctx := c.contextOrBackground()

		listOut, err := runCmd(ctx, c.ListCmd)
		if err != nil {
			yield(Document{}, fmt.Errorf("list-cmd: %w", err))
			return
		}

		keys := splitKeys(listOut)
		if len(keys) == 0 {
			return
		}

		workers := c.Workers
		if workers < 1 {
			workers = 1
		}

		if workers == 1 {
			c.runSerial(ctx, keys, prev, maxChars, yield)
			return
		}

		c.runParallel(ctx, keys, prev, maxChars, workers, yield)
	}
}

func (c *CommandCorpus) contextOrBackground() context.Context {
	if c.Ctx != nil {
		return c.Ctx
	}
	return context.Background()
}

// runSerial processes keys in list order in the caller's goroutine.
func (c *CommandCorpus) runSerial(
	ctx context.Context,
	keys []string,
	prev map[string]string,
	maxChars int,
	yield func(Document, error) bool,
) {
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			yield(Document{}, err)
			return
		}
		doc, err := c.processKey(ctx, key, prev, maxChars)
		if err != nil {
			if !yield(Document{}, err) {
				return
			}
			continue
		}
		if doc.Key == "" {
			continue
		}
		if !yield(doc, nil) {
			return
		}
	}
}

// runParallel dispatches keys across a worker pool, using a reorder buffer so
// the outer yield fires in ListCmd's output order. Workers error-return goes
// through yield as well, keeping per-key behavior identical to runSerial.
func (c *CommandCorpus) runParallel(
	ctx context.Context,
	keys []string,
	prev map[string]string,
	maxChars, workers int,
	yield func(Document, error) bool,
) {
	type result struct {
		seq int
		doc Document
		err error
	}

	jobs := make(chan int, workers*2)
	results := make(chan result, workers*2)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for seq := range jobs {
				if err := ctx.Err(); err != nil {
					results <- result{seq: seq, err: err}
					continue
				}
				doc, err := c.processKey(ctx, keys[seq], prev, maxChars)
				results <- result{seq: seq, doc: doc, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i := range keys {
			if ctx.Err() != nil {
				return
			}
			jobs <- i
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	pending := make(map[int]result)
	next := 0
	for r := range results {
		pending[r.seq] = r
		for {
			cur, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			next++

			if cur.err != nil {
				if !yield(Document{}, cur.err) {
					// Drain remaining results so workers exit cleanly.
					for range results {
					}
					return
				}
				continue
			}
			if cur.doc.Key == "" {
				continue
			}
			if !yield(cur.doc, nil) {
				for range results {
				}
				return
			}
		}
	}
}

// processKey runs HashCmd (if configured) then ReadCmd for a single key,
// returning either a reuse sentinel (Texts == nil) or a fresh Document.
func (c *CommandCorpus) processKey(
	ctx context.Context,
	key string,
	prev map[string]string,
	maxChars int,
) (Document, error) {
	// HashCmd fast-path: if the hash matches prev, signal reuse and skip ReadCmd.
	if len(c.HashCmd) > 0 && prev != nil {
		hashOut, err := runCmd(ctx, execx.AppendArg(c.HashCmd, key))
		if err != nil {
			return Document{}, fmt.Errorf("hash-cmd %s: %w", key, err)
		}
		hash := strings.TrimSpace(hashOut)
		if hash != "" {
			if prevHash, ok := prev[key]; ok && prevHash == hash {
				return Document{Key: key, Hash: hash, Texts: nil}, nil
			}
		}
	}

	text, err := runCmd(ctx, execx.AppendArg(c.ReadCmd, key))
	if err != nil {
		return Document{}, fmt.Errorf("read-cmd %s: %w", key, err)
	}

	trimmed := strings.TrimSpace(text)
	if len(trimmed) == 0 {
		return Document{}, nil // skip empties
	}

	h := sha256.Sum256([]byte(trimmed))
	hashHex := hex.EncodeToString(h[:])

	chunks := splitChunks(trimmed, maxChars)
	if len(chunks) == 0 {
		return Document{}, nil
	}

	return Document{Key: key, Hash: hashHex, Texts: chunks}, nil
}

// splitKeys parses newline-separated stdout into a list of non-empty keys.
func splitKeys(out string) []string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "\n")
	keys := raw[:0]
	for _, k := range raw {
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// splitChunks splits NUL-delimited text into chunks, truncating each to
// maxChars. Non-NUL input becomes a single chunk.
func splitChunks(text string, maxChars int) []string {
	raw := strings.Split(text, "\x00")
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if len(c) > maxChars {
			c = c[:maxChars]
		}
		out = append(out, c)
	}
	return out
}

func runCmd(ctx context.Context, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
