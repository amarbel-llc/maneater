package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"iter"
	"os"
	"path/filepath"
	"sort"
)

// FilesCorpus indexes text files matching glob patterns.
type FilesCorpus struct {
	CorpusName string
	Patterns   []string
	MaxChars   int
}

func (c *FilesCorpus) Name() string { return c.CorpusName }

func (c *FilesCorpus) Prepare() error { return nil }

func (c *FilesCorpus) Documents() iter.Seq2[Document, error] {
	return func(yield func(Document, error) bool) {
		maxChars := c.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}

		paths, err := c.resolvePatterns()
		if err != nil {
			yield(Document{}, err)
			return
		}

		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				if !yield(Document{}, err) {
					return
				}
				continue
			}

			if isBinary(content) {
				continue
			}

			h := sha256.Sum256(content)
			hashHex := hex.EncodeToString(h[:])

			text := string(content)
			if len(text) > maxChars {
				text = text[:maxChars]
			}

			if !yield(Document{Key: path, Hash: hashHex, Texts: []string{text}}, nil) {
				return
			}
		}
	}
}

func (c *FilesCorpus) resolvePatterns() ([]string, error) {
	seen := make(map[string]bool)
	var result []string

	for _, pattern := range c.Patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}

		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}

			if !seen[m] {
				seen[m] = true
				result = append(result, m)
			}
		}
	}

	sort.Strings(result)
	return result, nil
}

func isBinary(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	return bytes.ContainsRune(check, 0)
}
