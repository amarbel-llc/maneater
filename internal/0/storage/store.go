// Package storage defines the content-addressed blob-store contract that
// maneater uses to persist and retrieve index blobs, plus a CommandStore
// implementation that shells out to user-configured read/write/exists/init
// commands. The default implementation lives in internal/0/madder and
// satisfies the same Store interface.
//
// Contract (see maneater.toml.5 for the TOML surface):
//
//   - read-cmd <digest> → stdout: raw blob bytes; exit != 0 → not found / error.
//   - write-cmd (data on stdin) → stdout: digest of the written blob. The
//     digest is parsed via parseDigestFromOutput — last "ok N - <digest>"
//     token (TAP-style) or the first whitespace-delimited token on the last
//     non-empty line. Fits madder, aws s3 + a sidecar, and most
//     content-addressable stores.
//   - exists-cmd (optional) → stdout contains a line matching "<store-id>:"
//     or similar; exit != 0 → not exists. If unset, the store is treated
//     as existing (skips fast-fail).
//   - init-cmd (optional) → runs once when the store needs initialization.
//     If unset, init is a no-op.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/madder"
)

// Store is the content-addressed blob storage interface the maneater
// index/search lifecycle calls into.
type Store interface {
	Read(ctx context.Context, digest string) ([]byte, error)
	Write(ctx context.Context, data []byte) (string, error)
	Exists(ctx context.Context) (bool, error)
	Init(ctx context.Context) error
}

// FromConfig returns the Store selected by cfg. When any of cfg.ReadCmd /
// cfg.WriteCmd is populated, returns a generic CommandStore. Otherwise
// returns the madder CLI default (the historical behavior), with StoreID
// defaulting to madder.DefaultStoreID if unset.
func FromConfig(cfg config.StorageConfig) Store {
	if len(cfg.ReadCmd) > 0 || len(cfg.WriteCmd) > 0 {
		return &CommandStore{
			StoreID:   cfg.StoreID,
			ReadCmd:   cfg.ReadCmd,
			WriteCmd:  cfg.WriteCmd,
			ExistsCmd: cfg.ExistsCmd,
			InitCmd:   cfg.InitCmd,
		}
	}
	storeID := cfg.StoreID
	if storeID == "" {
		storeID = madder.DefaultStoreID
	}
	return &madder.Store{StoreID: storeID}
}

// CommandStore is a generic blob store backed by user-configured shell
// commands. StoreID is optional; when set, Exists treats the presence of
// that token (anywhere on a line, before a ":") in exists-cmd stdout as
// "store exists" — same heuristic as madder.
type CommandStore struct {
	StoreID   string
	ReadCmd   []string
	WriteCmd  []string
	ExistsCmd []string
	InitCmd   []string
}

// Read runs ReadCmd with the digest appended as the final positional arg.
// Stdout is the raw blob.
func (s *CommandStore) Read(ctx context.Context, digest string) ([]byte, error) {
	if len(s.ReadCmd) == 0 {
		return nil, fmt.Errorf("storage: read-cmd not configured")
	}
	argv := appendArg(s.ReadCmd, digest)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w\nstderr: %s",
			argv[0], digest, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// Write runs WriteCmd with the blob on stdin. Stdout is parsed for the
// resulting digest via parseDigestFromOutput (shared with madder).
func (s *CommandStore) Write(ctx context.Context, data []byte) (string, error) {
	if len(s.WriteCmd) == 0 {
		return "", fmt.Errorf("storage: write-cmd not configured")
	}
	cmd := exec.CommandContext(ctx, s.WriteCmd[0], s.WriteCmd[1:]...)
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w\nstderr: %s",
			s.WriteCmd[0], err, stderr.String())
	}

	digest, err := madder.ParseDigestFromOutput(stdout.String())
	if err != nil {
		return "", fmt.Errorf("parsing %s output: %w", s.WriteCmd[0], err)
	}
	return digest, nil
}

// Exists runs ExistsCmd if configured. A successful run whose stdout
// contains a line beginning with "<StoreID>:" (or any line at all, if
// StoreID is empty) is treated as "store exists". A non-zero exit means
// "not exists". If ExistsCmd is unset, returns true unconditionally so
// callers skip the fast-fail.
func (s *CommandStore) Exists(ctx context.Context) (bool, error) {
	if len(s.ExistsCmd) == 0 {
		return true, nil
	}
	out, err := exec.CommandContext(ctx, s.ExistsCmd[0], s.ExistsCmd[1:]...).Output()
	if err != nil {
		return false, nil
	}
	if s.StoreID == "" {
		// With no StoreID, success-exit is enough.
		return true, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) == s.StoreID {
			return true, nil
		}
	}
	return false, nil
}

// Init runs InitCmd once if configured, capturing its output so the caller
// can emit structured progress without interleaving. If InitCmd is unset,
// returns nil.
func (s *CommandStore) Init(ctx context.Context) error {
	if len(s.InitCmd) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, s.InitCmd[0], s.InitCmd[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\nstdout: %s\nstderr: %s",
			s.InitCmd[0], err, stdout.String(), stderr.String())
	}
	return nil
}

// appendArg returns base + [arg]. Always allocates a new slice so
// concurrent callers don't alias a shared backing array.
func appendArg(base []string, arg string) []string {
	out := make([]string, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, arg)
	return out
}
