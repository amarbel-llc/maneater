package corpus_test

import (
	"testing"

	"github.com/amarbel-llc/maneater/internal/config"
	"github.com/amarbel-llc/maneater/internal/corpus"
)

func collectDocuments(t *testing.T, c corpus.Corpus) ([]corpus.Document, []error) {
	t.Helper()
	var docs []corpus.Document
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
	c := &corpus.CommandCorpus{
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
	c := &corpus.CommandCorpus{
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
	c := &corpus.CommandCorpus{
		CorpusName: "empty",
		ListCmd:    []string{"printf", "key1\nkey2\n"},
		ReadCmd:    []string{"true"},
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
	c := &corpus.CommandCorpus{
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
	c := &corpus.CommandCorpus{
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
	c := &corpus.CommandCorpus{
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
	c := &corpus.CommandCorpus{
		CorpusName: "defaults",
		ListCmd:    []string{"echo", "k"},
		ReadCmd:    []string{"echo", "short"},
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if docs[0].Texts[0] != "short k" {
		t.Errorf("text = %q, want %q", docs[0].Texts[0], "short k")
	}
}

func TestFromConfigCommand(t *testing.T) {
	cc := config.CorpusConfig{
		Name:    "test",
		Type:    "command",
		ListCmd: []string{"echo", "key"},
		ReadCmd: []string{"echo", "text"},
	}

	c, err := corpus.FromConfig(cc)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if c.Name() != "test" {
		t.Errorf("Name() = %q, want test", c.Name())
	}
}

func TestFromConfigCommandValidation(t *testing.T) {
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
			_, err := corpus.FromConfig(tt.cc)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
