package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type BlobStore interface {
	Write(data []byte) (digest string, err error)
	Read(digest string) ([]byte, error)
}

type CommandBlobStore struct {
	ReadCmd  []string
	WriteCmd []string
}

func (s *CommandBlobStore) Write(data []byte) (string, error) {
	if len(s.WriteCmd) == 0 {
		return "", fmt.Errorf("no write-cmd configured")
	}

	cmd := exec.Command(s.WriteCmd[0], s.WriteCmd[1:]...)
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("write-cmd failed: %w\nstderr: %s", err, stderr.String())
	}

	digest, err := parseDigestFromOutput(stdout.String())
	if err != nil {
		return "", fmt.Errorf("parsing write output: %w", err)
	}
	return digest, nil
}

func (s *CommandBlobStore) Read(digest string) ([]byte, error) {
	if len(s.ReadCmd) == 0 {
		return nil, fmt.Errorf("no read-cmd configured")
	}

	args := append(s.ReadCmd[1:], digest)
	cmd := exec.Command(s.ReadCmd[0], args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read-cmd failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// parseDigestFromOutput extracts a markl-id from write command output.
// It handles TAP format where ok lines contain the digest after "ok N - ",
// and falls back to the last non-empty line for plain digest output.
func parseDigestFromOutput(stdout string) (string, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output")
	}

	// Scan for TAP ok lines: "ok N - <digest> <path>"
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
