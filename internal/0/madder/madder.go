// Package madder is maneater's thin wrapper around the madder CLI for blob
// storage. All shell-outs to madder live here.
package madder

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultStoreID is the madder store ID used when none is configured.
const DefaultStoreID = "maneater"

// Store is a handle on a single madder blob store, identified by StoreID.
type Store struct {
	StoreID string
}

// Read fetches a blob by digest via `madder cat <digest>`.
func (s *Store) Read(ctx context.Context, digest string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "madder", "cat", digest)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("madder cat %s: %w\nstderr: %s", digest, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// Write streams data to `madder write <store-id> -` and returns the digest
// reported by madder (parsed from TAP-formatted stdout).
func (s *Store) Write(ctx context.Context, data []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "madder", "write", s.StoreID, "-")
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("madder write %s: %w\nstderr: %s", s.StoreID, err, stderr.String())
	}

	digest, err := ParseDigestFromOutput(stdout.String())
	if err != nil {
		return "", fmt.Errorf("parsing madder write output: %w", err)
	}
	return digest, nil
}

// Exists reports whether the store already appears in `madder list`.
// The madder list output format is "<store-id>: <type> <characteristic>",
// one store per line, so we compare against the token before the colon.
func (s *Store) Exists(ctx context.Context) (bool, error) {
	listed, err := exec.CommandContext(ctx, "madder", "list").Output()
	if err != nil {
		return false, fmt.Errorf("madder list: %w\nIs madder installed and on PATH?", err)
	}
	for _, line := range strings.Split(string(listed), "\n") {
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

// Init runs `madder init <store-id>`. Output is captured so the caller can
// emit its own structured progress (e.g. TAP) without interleaving with
// madder's own output. The caller is responsible for handling the
// already-exists case (use Exists first).
func (s *Store) Init(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "madder", "init", s.StoreID)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("madder init %s: %w\nstdout: %s\nstderr: %s",
			s.StoreID, err, stdout.String(), stderr.String())
	}
	return nil
}

// ParseDigestFromOutput extracts a markl-id from `madder write` output. It
// handles TAP format where ok lines contain the digest after "ok N - ", and
// falls back to the last non-empty line for plain digest output.
func ParseDigestFromOutput(stdout string) (string, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output")
	}

	var lastOKDigest string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "ok" && fields[2] == "-" {
			lastOKDigest = fields[3]
		}
	}
	if lastOKDigest != "" {
		return lastOKDigest, nil
	}

	last := lines[len(lines)-1]
	fields := strings.Fields(last)
	if len(fields) > 0 {
		return fields[0], nil
	}
	return "", fmt.Errorf("no digest found in output: %q", stdout)
}
