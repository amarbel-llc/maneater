package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/madder"
	"github.com/amarbel-llc/maneater/internal/0/storage"
)

// TestFromConfigDefaultsToMadder verifies that an empty StorageConfig
// (the pre-#8 baseline) still resolves to the madder CLI backend.
func TestFromConfigDefaultsToMadder(t *testing.T) {
	s := storage.FromConfig(config.StorageConfig{})
	if _, ok := s.(*madder.Store); !ok {
		t.Fatalf("empty config should yield *madder.Store, got %T", s)
	}
}

// TestFromConfigDefaultsToMadderWithStoreID verifies a StoreID alone (no
// commands) still selects madder and propagates the ID.
func TestFromConfigDefaultsToMadderWithStoreID(t *testing.T) {
	s := storage.FromConfig(config.StorageConfig{StoreID: "my-store"})
	ms, ok := s.(*madder.Store)
	if !ok {
		t.Fatalf("expected *madder.Store, got %T", s)
	}
	if ms.StoreID != "my-store" {
		t.Errorf("StoreID = %q, want my-store", ms.StoreID)
	}
}

// TestFromConfigCommandStoreWhenReadCmdSet confirms that configuring even
// one of the command fields swaps in CommandStore.
func TestFromConfigCommandStoreWhenReadCmdSet(t *testing.T) {
	s := storage.FromConfig(config.StorageConfig{
		ReadCmd: []string{"cat"},
	})
	if _, ok := s.(*storage.CommandStore); !ok {
		t.Fatalf("expected *storage.CommandStore, got %T", s)
	}
}

// TestFromConfigCommandStoreWhenWriteCmdSet is the symmetric test: only
// WriteCmd configured (no ReadCmd) still flips the dispatch to
// CommandStore. Covers the OR branch in FromConfig.
func TestFromConfigCommandStoreWhenWriteCmdSet(t *testing.T) {
	s := storage.FromConfig(config.StorageConfig{
		WriteCmd: []string{"cat"},
	})
	if _, ok := s.(*storage.CommandStore); !ok {
		t.Fatalf("expected *storage.CommandStore, got %T", s)
	}
}

// TestCommandStoreRoundTrip wires up a filesystem-backed storage backend
// using only sh/cat/sha256sum, then round-trips a blob through Write +
// Read. This is the smoke test for the end-to-end contract.
func TestCommandStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	blobsDir := filepath.Join(dir, "blobs")
	if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// write-cmd: hash stdin into $BLOBS/<digest>, print the digest.
	// Uses process substitution / temp files so both stdin and its hash
	// are available. Simplest reliable shape: tee → sha256sum → rename.
	writeScript := `
set -eu
tmp="$(mktemp -p "$BLOBS" .inflight.XXXXXX)"
cat >"$tmp"
digest="$(sha256sum "$tmp" | awk '{print $1}')"
mv "$tmp" "$BLOBS/$digest"
echo "$digest"
`
	readScript := `
set -eu
cat "$BLOBS/$1"
`

	s := &storage.CommandStore{
		StoreID:  "test",
		WriteCmd: []string{"sh", "-c", writeScript, "--"},
		ReadCmd:  []string{"sh", "-c", readScript, "--"},
	}
	t.Setenv("BLOBS", blobsDir)

	ctx := context.Background()
	payload := []byte("hello, storage world\n")
	digest, err := s.Write(ctx, payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if digest == "" {
		t.Fatal("Write returned empty digest")
	}

	got, err := s.Read(ctx, digest)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, payload)
	}
}

// TestCommandStoreExistsUnsetIsTrue: without ExistsCmd, Exists returns
// true so callers skip the fast-fail (useful for stores that don't model
// existence, like S3).
func TestCommandStoreExistsUnsetIsTrue(t *testing.T) {
	s := &storage.CommandStore{}
	ok, err := s.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("Exists should return true when ExistsCmd is unset")
	}
}

// TestCommandStoreExistsMatchesStoreID confirms the madder-style "<name>:"
// parsing works for generic command output.
func TestCommandStoreExistsMatchesStoreID(t *testing.T) {
	s := &storage.CommandStore{
		StoreID:   "my-store",
		ExistsCmd: []string{"printf", "foo: bar\nmy-store: local hash bucketed\nother: remote\n"},
	}
	ok, err := s.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("Exists should match 'my-store:' line")
	}
}

// TestCommandStoreExistsMissesStoreID confirms no match → false.
func TestCommandStoreExistsMissesStoreID(t *testing.T) {
	s := &storage.CommandStore{
		StoreID:   "missing-store",
		ExistsCmd: []string{"printf", "other: remote\n"},
	}
	ok, err := s.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Error("Exists should not match when StoreID isn't in output")
	}
}

// TestCommandStoreExistsCmdFailure: non-zero exit → false, nil error
// (parity with madder.Store.Exists, which swallows the error to signal
// "not exists").
func TestCommandStoreExistsCmdFailure(t *testing.T) {
	s := &storage.CommandStore{
		StoreID:   "x",
		ExistsCmd: []string{"false"},
	}
	ok, err := s.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Error("Exists should be false when ExistsCmd exits non-zero")
	}
}

// TestCommandStoreInitRuns exercises the optional InitCmd path.
func TestCommandStoreInitRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "init-ran")
	s := &storage.CommandStore{
		InitCmd: []string{"touch", marker},
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("InitCmd did not create marker: %v", err)
	}
}

// TestCommandStoreInitUnsetIsNoOp: no InitCmd configured → nil error.
func TestCommandStoreInitUnsetIsNoOp(t *testing.T) {
	s := &storage.CommandStore{}
	if err := s.Init(context.Background()); err != nil {
		t.Errorf("Init with no cmd should be no-op, got %v", err)
	}
}

// TestCommandStoreReadCmdFailure: non-zero exit from read-cmd surfaces
// as an error whose message includes the digest arg and the stderr
// from the child process.
func TestCommandStoreReadCmdFailure(t *testing.T) {
	s := &storage.CommandStore{
		ReadCmd: []string{"sh", "-c", "echo >&2 no-such-blob; exit 2", "--"},
	}
	_, err := s.Read(context.Background(), "some-digest")
	if err == nil {
		t.Fatal("expected error from failing read-cmd")
	}
	msg := err.Error()
	if !strings.Contains(msg, "some-digest") {
		t.Errorf("error should mention the digest; got %q", msg)
	}
	if !strings.Contains(msg, "no-such-blob") {
		t.Errorf("error should surface stderr; got %q", msg)
	}
}

// TestCommandStoreWriteCmdFailure: non-zero exit from write-cmd surfaces
// as an error including the write-cmd binary name and stderr.
func TestCommandStoreWriteCmdFailure(t *testing.T) {
	s := &storage.CommandStore{
		WriteCmd: []string{"sh", "-c", "echo >&2 disk-full; exit 28", "--"},
	}
	_, err := s.Write(context.Background(), []byte("payload"))
	if err == nil {
		t.Fatal("expected error from failing write-cmd")
	}
	if !strings.Contains(err.Error(), "disk-full") {
		t.Errorf("error should surface stderr; got %q", err)
	}
}

// TestCommandStoreWriteBadOutput: write-cmd exits 0 but emits nothing,
// so ParseDigestFromOutput returns an error. CommandStore must wrap
// that rather than hand back the empty string.
func TestCommandStoreWriteBadOutput(t *testing.T) {
	s := &storage.CommandStore{
		WriteCmd: []string{"sh", "-c", "true", "--"}, // exit 0, no stdout
	}
	_, err := s.Write(context.Background(), []byte("payload"))
	if err == nil {
		t.Fatal("expected error from empty write-cmd output")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error should mention parsing; got %q", err)
	}
}

// TestCommandStoreInitCmdFailure: non-zero exit from init-cmd surfaces
// as an error carrying both stdout and stderr, so a failed init
// doesn't silently lose the child process's diagnostics.
func TestCommandStoreInitCmdFailure(t *testing.T) {
	s := &storage.CommandStore{
		InitCmd: []string{"sh", "-c", "echo init-stdout; echo >&2 init-stderr; exit 7", "--"},
	}
	err := s.Init(context.Background())
	if err == nil {
		t.Fatal("expected error from failing init-cmd")
	}
	msg := err.Error()
	if !strings.Contains(msg, "init-stdout") {
		t.Errorf("error should carry stdout; got %q", msg)
	}
	if !strings.Contains(msg, "init-stderr") {
		t.Errorf("error should carry stderr; got %q", msg)
	}
}

// TestCommandStoreExistsEmptyStoreIDSucceeds: when StoreID is unset, any
// zero-exit exists-cmd (regardless of stdout shape) is treated as
// "exists". Covers the fallback branch right for stores that don't
// expose a canonical name token (e.g. S3 buckets).
func TestCommandStoreExistsEmptyStoreIDSucceeds(t *testing.T) {
	s := &storage.CommandStore{
		ExistsCmd: []string{"true"}, // zero exit, empty stdout
	}
	ok, err := s.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("Exists with empty StoreID + success should return true")
	}
}
