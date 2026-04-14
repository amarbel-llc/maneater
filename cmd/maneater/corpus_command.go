package main

import (
	"bytes"
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
type CommandCorpus struct {
	CorpusName string
	ListCmd    []string
	ReadCmd    []string
	MaxChars   int
}

func (c *CommandCorpus) Name() string { return c.CorpusName }

func (c *CommandCorpus) Prepare() error { return nil }

func (c *CommandCorpus) Documents() iter.Seq2[Document, error] {
	return func(yield func(Document, error) bool) {
		maxChars := c.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}

		keys, err := runCmd(c.ListCmd)
		if err != nil {
			yield(Document{}, fmt.Errorf("list-cmd: %w", err))
			return
		}

		for _, key := range strings.Split(strings.TrimSpace(keys), "\n") {
			if key == "" {
				continue
			}

			args := append(c.ReadCmd[1:], key) //nolint:gocritic
			text, err := runCmd(append([]string{c.ReadCmd[0]}, args...))
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

func runCmd(argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
