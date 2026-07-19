package corpus_test

import (
	"os"
	"testing"

	"code.linenisgreat.com/maneater/internal/0/config"
	"code.linenisgreat.com/maneater/internal/alfa/corpus"
)

func collectDocuments(t *testing.T, c corpus.Corpus) ([]corpus.Document, []error) {
	t.Helper()
	return collectWithPrev(t, c, nil)
}

func collectWithPrev(t *testing.T, c corpus.Corpus, prev map[string]string) ([]corpus.Document, []error) {
	t.Helper()
	var docs []corpus.Document
	var errs []error
	for doc, err := range c.Documents(prev) {
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
		Name:       "test",
		Type:       "command",
		ListCmd:    []string{"echo", "key"},
		ReadCmd:    []string{"echo", "text"},
		HashCmd:    []string{"echo", "hash"},
		PrepareCmd: []string{"echo", "prepare"},
		Workers:    8,
	}

	c, err := corpus.FromConfig(cc)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if c.Name() != "test" {
		t.Errorf("Name() = %q, want test", c.Name())
	}

	// Every CorpusConfig knob must propagate — HashCmd, PrepareCmd, and
	// Workers were silently dropped once (maneater#35), which cost the
	// default manpages corpus its hash fast-path and worker pool.
	cmdc, ok := c.(*corpus.CommandCorpus)
	if !ok {
		t.Fatalf("FromConfig returned %T, want *corpus.CommandCorpus", c)
	}
	if len(cmdc.HashCmd) == 0 || cmdc.HashCmd[1] != "hash" {
		t.Errorf("HashCmd = %v, want [echo hash]", cmdc.HashCmd)
	}
	if len(cmdc.PrepareCmd) == 0 || cmdc.PrepareCmd[1] != "prepare" {
		t.Errorf("PrepareCmd = %v, want [echo prepare]", cmdc.PrepareCmd)
	}
	if cmdc.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cmdc.Workers)
	}
}

func TestCommandCorpusHashCmdReuse(t *testing.T) {
	// hash-cmd outputs "fixed-hash" for any key. If prev matches, Texts should be nil.
	c := &corpus.CommandCorpus{
		CorpusName: "hash-reuse",
		ListCmd:    []string{"printf", "k1\nk2\n"},
		ReadCmd:    []string{"sh", "-c", "echo should-not-run"},
		HashCmd:    []string{"sh", "-c", "printf fixed-hash"},
	}
	prev := map[string]string{"k1": "fixed-hash", "k2": "fixed-hash"}
	docs, errs := collectWithPrev(t, c, prev)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2", len(docs))
	}
	for i, doc := range docs {
		if doc.Texts != nil {
			t.Errorf("docs[%d].Texts = %v, want nil (reuse sentinel)", i, doc.Texts)
		}
		if doc.Hash != "fixed-hash" {
			t.Errorf("docs[%d].Hash = %q, want fixed-hash", i, doc.Hash)
		}
	}
}

// TestCommandCorpusHashCmdHashStored is the round-trip the hash
// fast-path depends on: a fresh read must store the HashCmd output as
// the document hash, so that feeding the first pass's hashes back as
// prev yields reuse sentinels on the second pass. Storing the
// text-sha256 instead (the old behavior) put prev in a different hash
// space than the probe, and the fast-path never fired.
func TestCommandCorpusHashCmdHashStored(t *testing.T) {
	c := &corpus.CommandCorpus{
		CorpusName: "hash-stored",
		ListCmd:    []string{"printf", "k1\nk2\n"},
		ReadCmd:    []string{"sh", "-c", "echo text for \"$1\"", "--"},
		HashCmd:    []string{"sh", "-c", "printf 'file-hash-%s' \"$1\"", "--"},
	}

	// Pass 1: no prev — fresh reads must carry the HashCmd hash.
	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("pass 1 errors: %v", errs)
	}
	if len(docs) != 2 {
		t.Fatalf("pass 1: got %d documents, want 2", len(docs))
	}
	prev := make(map[string]string, len(docs))
	for i, doc := range docs {
		want := "file-hash-" + doc.Key
		if doc.Hash != want {
			t.Errorf("pass 1 docs[%d].Hash = %q, want %q (HashCmd output)", i, doc.Hash, want)
		}
		if doc.Texts == nil {
			t.Errorf("pass 1 docs[%d].Texts = nil, want fresh read", i)
		}
		prev[doc.Key] = doc.Hash
	}

	// Pass 2: prev seeded from pass 1 — every doc must be a reuse sentinel.
	docs, errs = collectWithPrev(t, c, prev)
	if len(errs) > 0 {
		t.Fatalf("pass 2 errors: %v", errs)
	}
	for i, doc := range docs {
		if doc.Texts != nil {
			t.Errorf("pass 2 docs[%d].Texts = %v, want nil (reuse sentinel)", i, doc.Texts)
		}
	}
}

// TestCommandCorpusHashCmdBlankFallsBack: a HashCmd that outputs
// nothing must not break hashing — the document falls back to the
// text-sha256, same as a corpus without HashCmd.
func TestCommandCorpusHashCmdBlankFallsBack(t *testing.T) {
	c := &corpus.CommandCorpus{
		CorpusName: "hash-blank",
		ListCmd:    []string{"printf", "k1\n"},
		ReadCmd:    []string{"sh", "-c", "echo stable text", "--"},
		HashCmd:    []string{"sh", "-c", "printf ''", "--"},
	}

	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if docs[0].Hash == "" {
		t.Error("Hash is empty, want text-sha256 fallback")
	}
	if len(docs[0].Hash) != 64 {
		t.Errorf("Hash = %q, want 64-char hex sha256 of the text", docs[0].Hash)
	}
}

func TestCommandCorpusHashCmdMismatchRunsReadCmd(t *testing.T) {
	// hash-cmd outputs a fresh hash per key; prev has a stale one. ReadCmd must run.
	c := &corpus.CommandCorpus{
		CorpusName: "hash-miss",
		ListCmd:    []string{"printf", "k1\n"},
		ReadCmd:    []string{"echo", "fresh text for"},
		HashCmd:    []string{"sh", "-c", "printf new-hash"},
	}
	prev := map[string]string{"k1": "old-hash"}
	docs, errs := collectWithPrev(t, c, prev)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if docs[0].Texts == nil {
		t.Errorf("expected Texts to be populated (read-cmd ran), got nil")
	}
	if len(docs[0].Texts) == 0 || docs[0].Texts[0] != "fresh text for k1" {
		t.Errorf("docs[0].Texts = %v, want [\"fresh text for k1\"]", docs[0].Texts)
	}
}

func TestCommandCorpusWorkersPreservesOrder(t *testing.T) {
	// With 4 workers and 8 keys whose read-cmd sleeps for a key-dependent time,
	// results must still arrive in list order.
	c := &corpus.CommandCorpus{
		CorpusName: "parallel",
		ListCmd:    []string{"printf", "k1\nk2\nk3\nk4\nk5\nk6\nk7\nk8\n"},
		// Key encodes its sleep: k1 sleeps longer than k8, so parallel
		// completion order differs from input order, forcing the reorder buffer.
		ReadCmd: []string{"sh", "-c", `
key=$1
case $key in
  k1) sleep 0.05 ;;
  k2) sleep 0.04 ;;
  k3) sleep 0.03 ;;
  k4) sleep 0.02 ;;
  k5) sleep 0.01 ;;
esac
echo "text-for-$key"
`, "--"},
		Workers: 4,
	}
	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 8 {
		t.Fatalf("got %d documents, want 8", len(docs))
	}
	for i, want := range []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8"} {
		if docs[i].Key != want {
			t.Errorf("docs[%d].Key = %q, want %q (order not preserved)", i, docs[i].Key, want)
		}
	}
}

func TestCommandCorpusNULSplitTexts(t *testing.T) {
	// read-cmd outputs NUL-separated chunks; the corpus should split them into
	// distinct Texts entries.
	c := &corpus.CommandCorpus{
		CorpusName: "chunks",
		ListCmd:    []string{"echo", "k"},
		ReadCmd:    []string{"sh", "-c", `printf 'first\0second\0'`, "--"},
		MaxChars:   1000,
	}
	docs, errs := collectDocuments(t, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if len(docs[0].Texts) != 2 {
		t.Fatalf("got %d chunks, want 2: %v", len(docs[0].Texts), docs[0].Texts)
	}
	if docs[0].Texts[0] != "first" || docs[0].Texts[1] != "second" {
		t.Errorf("chunks = %v, want [first second]", docs[0].Texts)
	}
}

func TestCommandCorpusPrepareCmdRuns(t *testing.T) {
	marker := t.TempDir() + "/prepared"
	c := &corpus.CommandCorpus{
		CorpusName: "prep",
		PrepareCmd: []string{"touch", marker},
		ListCmd:    []string{"echo", "k"},
		ReadCmd:    []string{"echo", "t"},
	}
	if err := c.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("prepare-cmd did not create marker: %v", err)
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
