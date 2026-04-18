// Package corpus defines the Corpus abstraction — a named source of
// Documents to be embedded and indexed — plus the Files and Command
// implementations. Manpages live in their own package because they carry
// heavier system-interaction helpers.
package corpus

import "iter"

// Document represents a single item to be embedded and indexed.
type Document struct {
	Key   string   // unique identifier shown in search results
	Hash  string   // hex SHA256 of source content for incremental caching
	Texts []string // text chunks to embed (each becomes a separate index entry)
}

// Corpus provides documents for embedding and indexing.
type Corpus interface {
	Name() string
	Prepare() error
	Documents() iter.Seq2[Document, error]
}

const defaultMaxChars = 500
