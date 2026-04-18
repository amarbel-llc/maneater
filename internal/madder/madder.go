// Package madder is maneater's thin wrapper around the madder CLI for blob
// storage. All shell-outs to madder live here.
package madder

import (
	"bytes"
	"fmt"
	"os"
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
func (s *Store) Read(digest string) ([]byte, error) {
	cmd := exec.Command("madder", "cat", digest)

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
func (s *Store) Write(data []byte) (string, error) {
	cmd := exec.Command("madder", "write", s.StoreID, "-")
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("madder write %s: %w\nstderr: %s", s.StoreID, err, stderr.String())
	}

	digest, err := parseDigestFromOutput(stdout.String())
	if err != nil {
		return "", fmt.Errorf("parsing madder write output: %w", err)
	}
	return digest, nil
}

// Exists reports whether the store already appears in `madder list`.
func (s *Store) Exists() (bool, error) {
	listed, err := exec.Command("madder", "list").Output()
	if err != nil {
		return false, fmt.Errorf("madder list: %w\nIs madder installed and on PATH?", err)
	}
	for _, line := range strings.Split(string(listed), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == s.StoreID {
			return true, nil
		}
	}
	return false, nil
}

// Init runs `madder init <store-id>`, forwarding output to os.Stdout/Stderr.
// The caller is responsible for handling the already-exists case (use Exists
// first).
func (s *Store) Init() error {
	cmd := exec.Command("madder", "init", s.StoreID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("madder init %s: %w", s.StoreID, err)
	}
	return nil
}

// parseDigestFromOutput extracts a markl-id from `madder write` output. It
// handles TAP format where ok lines contain the digest after "ok N - ", and
// falls back to the last non-empty line for plain digest output.
func parseDigestFromOutput(stdout string) (string, error) {
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
