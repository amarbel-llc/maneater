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
)

// CommandCorpus indexes documents from an external command pair.
// ListCmd outputs one document key per line to stdout.
// ReadCmd receives a key as its final argument and outputs text to stdout.
//
// Callers that want SIGINT cancellation to propagate to the external
// commands should set Ctx before iterating. A zero-value Ctx falls back to
// context.Background inside Documents().
type CommandCorpus struct {
	CorpusName string
	ListCmd    []string
	ReadCmd    []string
	MaxChars   int
	Ctx        context.Context
}

func (c *CommandCorpus) Name() string { return c.CorpusName }

func (c *CommandCorpus) Prepare() error { return nil }

func (c *CommandCorpus) Documents() iter.Seq2[Document, error] {
	return func(yield func(Document, error) bool) {
		maxChars := c.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}

		ctx := c.Ctx
		if ctx == nil {
			ctx = context.Background()
		}

		keys, err := runCmd(ctx, c.ListCmd)
		if err != nil {
			yield(Document{}, fmt.Errorf("list-cmd: %w", err))
			return
		}

		for _, key := range strings.Split(strings.TrimSpace(keys), "\n") {
			if key == "" {
				continue
			}

			args := append(c.ReadCmd[1:], key) //nolint:gocritic
			text, err := runCmd(ctx, append([]string{c.ReadCmd[0]}, args...))
			if err != nil {
				if !yield(Document{}, fmt.Errorf("read-cmd %s: %w", key, err)) {
					return
				}
				continue
			}

			text = strings.TrimSpace(text)
			if len(text) == 0 {
				continue
			}

			h := sha256.Sum256([]byte(text))
			hashHex := hex.EncodeToString(h[:])

			if len(text) > maxChars {
				text = text[:maxChars]
			}

			if !yield(Document{Key: key, Hash: hashHex, Texts: []string{text}}, nil) {
				return
			}
		}
	}
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
