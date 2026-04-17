package main

import "testing"

func TestParseDigestTAPFormat(t *testing.T) {
	got, err := parseDigestFromOutput("ok - blake2b256:abc123def456 -\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "blake2b256:abc123def456" {
		t.Errorf("got %q, want blake2b256:abc123def456", got)
	}
}

func TestParseDigestPlain(t *testing.T) {
	got, err := parseDigestFromOutput("sha256:deadbeef\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sha256:deadbeef" {
		t.Errorf("got %q, want sha256:deadbeef", got)
	}
}

func TestParseDigestMultilineTAP(t *testing.T) {
	input := "TAP version 14\nok - blake2b256:fff000 /dev/stdin\n"
	got, err := parseDigestFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "blake2b256:fff000" {
		t.Errorf("got %q, want blake2b256:fff000", got)
	}
}

func TestParseDigestFallbackNoColon(t *testing.T) {
	got, err := parseDigestFromOutput("justahash\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "justahash" {
		t.Errorf("got %q, want justahash", got)
	}
}

func TestParseDigestEmpty(t *testing.T) {
	_, err := parseDigestFromOutput("")
	if err == nil {
		t.Error("expected error for empty output")
	}
}

func TestCommandBlobStoreRoundTrip(t *testing.T) {
	store := &CommandBlobStore{
		WriteCmd: []string{"sh", "-c", "cat >/dev/null; echo 'ok - blake2b256:testdigest -'"},
		ReadCmd:  []string{"echo", "hello from blob"},
	}

	digest, err := store.Write([]byte("test data"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if digest != "blake2b256:testdigest" {
		t.Errorf("digest = %q, want blake2b256:testdigest", digest)
	}

	data, err := store.Read(digest)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) == 0 {
		t.Error("Read returned empty data")
	}
}
