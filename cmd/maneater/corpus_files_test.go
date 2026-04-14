package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesCorpusDocumentsHaveHash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	corpus := &FilesCorpus{
		CorpusName: "test",
		Patterns:   []string{filepath.Join(dir, "*.txt")},
	}

	for doc, err := range corpus.Documents() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.Hash == "" {
			t.Errorf("document %q has empty hash", doc.Key)
		}
		if len(doc.Hash) != 64 { // SHA256 hex
			t.Errorf("document %q hash length: got %d, want 64", doc.Key, len(doc.Hash))
		}
	}
}

func TestFilesCorpusHashDeterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}

	corpus := &FilesCorpus{
		CorpusName: "test",
		Patterns:   []string{filepath.Join(dir, "*.txt")},
	}

	var hashes []string
	for i := 0; i < 2; i++ {
		for doc, err := range corpus.Documents() {
			if err != nil {
				t.Fatal(err)
			}
			hashes = append(hashes, doc.Hash)
		}
	}

	if hashes[0] != hashes[1] {
		t.Errorf("same content produced different hashes: %s vs %s", hashes[0], hashes[1])
	}
}
