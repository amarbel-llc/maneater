// Package corpus defines the Corpus abstraction — a named source of
// Documents to be embedded and indexed — plus the Files and Command
// implementations. Manpages live in their own package because they carry
// heavier system-interaction helpers.
package corpus

import "iter"

// Document represents a single item to be embedded and indexed. If Texts is
// nil, the caller should reuse a previously cached entry for Key (the
// corpus has already determined the hash is unchanged via HashCmd and
// skipped the expensive read).
type Document struct {
	Key   string   // unique identifier shown in search results
	Hash  string   // hex SHA256 of source content for incremental caching
	Texts []string // text chunks to embed; nil = "reuse cached entry"
}

// Corpus provides documents for embedding and indexing.
//
// Documents receives the previous run's hashes keyed by Document.Key,
// enabling implementations with a HashCmd to skip ReadCmd when the hash is
// unchanged. In that case the yielded Document has Texts == nil and the
// caller reuses its cached entry. Corpora without a cheap hash path
// ignore prev — the caller still gets incremental behavior via post-hoc
// hash comparison in the index loop.
type Corpus interface {
	Name() string
	Prepare() error
	Documents(prev map[string]string) iter.Seq2[Document, error]
}

const defaultMaxChars = 500
