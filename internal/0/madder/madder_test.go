package madder

import "testing"

func TestParseDigestTAPFormat(t *testing.T) {
	got, err := ParseDigestFromOutput("ok 1 - blake2b256-abc123def456 -\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "blake2b256-abc123def456" {
		t.Errorf("got %q, want blake2b256-abc123def456", got)
	}
}

func TestParseDigestPlain(t *testing.T) {
	got, err := ParseDigestFromOutput("blake2b256-deadbeef\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "blake2b256-deadbeef" {
		t.Errorf("got %q, want blake2b256-deadbeef", got)
	}
}

func TestParseDigestMultilineTAP(t *testing.T) {
	input := "TAP version 14\n# switched to blob store: maneater\nok 1 - blake2b256-fff000aaa111 /dev/stdin\n1..1\n"
	got, err := ParseDigestFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "blake2b256-fff000aaa111" {
		t.Errorf("got %q, want blake2b256-fff000aaa111", got)
	}
}

func TestParseDigestFallbackPlainToken(t *testing.T) {
	got, err := ParseDigestFromOutput("justahash\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "justahash" {
		t.Errorf("got %q, want justahash", got)
	}
}

func TestParseDigestEmpty(t *testing.T) {
	_, err := ParseDigestFromOutput("")
	if err == nil {
		t.Error("expected error for empty output")
	}
}
