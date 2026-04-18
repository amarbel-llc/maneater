package main

import (
	"testing"

	"github.com/amarbel-llc/maneater/internal/config"
)

func collectDocuments(t *testing.T, c Corpus) ([]Document, []error) {
	t.Helper()
	var docs []Document
	var errs []error
	for doc, err := range c.Documents() {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		docs = append(docs, doc)
	}
	return docs, errs
}

func TestCommandCorpusBasic(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "test",
		ListCmd:    []string{"printf", "alpha\nbeta\ngamma\n"},
		ReadCmd:    []string{"echo", "content for"},
		MaxChars:   1000,
	}

	if c.Name() != "test" {
		t.Errorf("Name() = %q, want test", c.Name())
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 3 {
		t.Fatalf("got %d documents, want 3", len(docs))
	}

	// echo appends the key as an argument, so output is "content for <key>"
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if docs[i].Key != want {
			t.Errorf("docs[%d].Key = %q, want %q", i, docs[i].Key, want)
		}
		wantText := "content for " + want
		if len(docs[i].Texts) != 1 || docs[i].Texts[0] != wantText {
			t.Errorf("docs[%d].Texts = %v, want [%q]", i, docs[i].Texts, wantText)
		}
	}
}

func TestCommandCorpusMaxChars(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "truncate",
		ListCmd:    []string{"echo", "key1"},
		ReadCmd:    []string{"echo", "abcdefghijklmnopqrstuvwxyz"},
		MaxChars:   10,
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if len(docs[0].Texts[0]) > 10 {
		t.Errorf("text length = %d, want <= 10", len(docs[0].Texts[0]))
	}
}

func TestCommandCorpusSkipsEmptyOutput(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "empty",
		ListCmd:    []string{"printf", "key1\nkey2\n"},
		// "true" outputs nothing, so key1 should be skipped; echo produces text for key2
		ReadCmd: []string{"true"},
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 0 {
		t.Errorf("got %d documents, want 0 (all empty)", len(docs))
	}
}

func TestCommandCorpusSkipsBlankKeys(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "blanks",
		ListCmd:    []string{"printf", "alpha\n\n\nbeta\n"},
		ReadCmd:    []string{"echo", "text for"},
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 2 {
		t.Errorf("got %d documents, want 2", len(docs))
	}
}

func TestCommandCorpusListCmdFailure(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "bad-list",
		ListCmd:    []string{"false"},
		ReadCmd:    []string{"echo"},
	}

	docs, errs := collectDocuments(t, c)
	if len(docs) != 0 {
		t.Errorf("got %d documents, want 0", len(docs))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
}

func TestCommandCorpusReadCmdFailure(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "bad-read",
		ListCmd:    []string{"printf", "key1\nkey2\n"},
		ReadCmd:    []string{"false"},
	}

	docs, errs := collectDocuments(t, c)
	if len(docs) != 0 {
		t.Errorf("got %d documents, want 0", len(docs))
	}
	if len(errs) != 2 {
		t.Errorf("got %d errors, want 2", len(errs))
	}
}

func TestCommandCorpusDefaultMaxChars(t *testing.T) {
	c := &CommandCorpus{
		CorpusName: "defaults",
		ListCmd:    []string{"echo", "k"},
		ReadCmd:    []string{"echo", "short"},
		// MaxChars left at 0 — should use defaultMaxChars (500)
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	// "short k" is well under 500, should pass through unchanged
	if docs[0].Texts[0] != "short k" {
		t.Errorf("text = %q, want %q", docs[0].Texts[0], "short k")
	}
}

func TestParseCommandCorpusConfig(t *testing.T) {
	input := []byte(`
[[corpora]]
name = "stories"
type = "command"
list-cmd = ["nebulous", "corpus-list"]
read-cmd = ["nebulous", "corpus-read"]
max-chars = 2000
`)
	doc, err := config.DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	corpora := config.DecodeCorpora(doc)
	if len(corpora) != 1 {
		t.Fatalf("expected 1 corpus, got %d", len(corpora))
	}

	cc := corpora[0]
	if cc.Name != "stories" {
		t.Errorf("name = %q, want stories", cc.Name)
	}
	if cc.Type != "command" {
		t.Errorf("type = %q, want command", cc.Type)
	}
	if len(cc.ListCmd) != 2 || cc.ListCmd[0] != "nebulous" || cc.ListCmd[1] != "corpus-list" {
		t.Errorf("list-cmd = %v, want [nebulous corpus-list]", cc.ListCmd)
	}
	if len(cc.ReadCmd) != 2 || cc.ReadCmd[0] != "nebulous" || cc.ReadCmd[1] != "corpus-read" {
		t.Errorf("read-cmd = %v, want [nebulous corpus-read]", cc.ReadCmd)
	}
	if cc.MaxChars != 2000 {
		t.Errorf("max-chars = %d, want 2000", cc.MaxChars)
	}
}

func TestCorpusFromConfigCommand(t *testing.T) {
	cc := config.CorpusConfig{
		Name:    "test",
		Type:    "command",
		ListCmd: []string{"echo", "key"},
		ReadCmd: []string{"echo", "text"},
	}

	corpus, err := corpusFromConfig(cc)
	if err != nil {
		t.Fatalf("corpusFromConfig: %v", err)
	}
	if corpus.Name() != "test" {
		t.Errorf("Name() = %q, want test", corpus.Name())
	}
}

func TestCorpusFromConfigCommandValidation(t *testing.T) {
	tests := []struct {
		name string
		cc   config.CorpusConfig
	}{
		{"no name", config.CorpusConfig{Type: "command", ListCmd: []string{"echo"}, ReadCmd: []string{"echo"}}},
		{"no list-cmd", config.CorpusConfig{Name: "x", Type: "command", ReadCmd: []string{"echo"}}},
		{"no read-cmd", config.CorpusConfig{Name: "x", Type: "command", ListCmd: []string{"echo"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := corpusFromConfig(tt.cc)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
