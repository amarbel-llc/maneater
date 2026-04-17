package embedding

import (
	"testing"
)

func TestBlobRoundTrip(t *testing.T) {
	meta := IndexMeta{
		ModelPath:      "/nix/store/abc.gguf",
		DocumentPrefix: "query: ",
		ConfigHash:     "abc123def456",
	}
	entries := []CachedEntry{
		{Key: "ls(1)", Hash: "aaa", Embedding: []float32{0.1, 0.2, 0.3}},
		{Key: "sed(1)", Hash: "bbb", Embedding: []float32{0.4, 0.5, 0.6}},
	}

	blob, err := MarshalIndexBlob(meta, entries)
	if err != nil {
		t.Fatalf("MarshalIndexBlob: %v", err)
	}

	gotMeta, gotEntries, err := UnmarshalIndexBlob(blob)
	if err != nil {
		t.Fatalf("UnmarshalIndexBlob: %v", err)
	}

	if gotMeta.ModelPath != meta.ModelPath {
		t.Errorf("ModelPath = %q, want %q", gotMeta.ModelPath, meta.ModelPath)
	}
	if gotMeta.ConfigHash != meta.ConfigHash {
		t.Errorf("ConfigHash = %q, want %q", gotMeta.ConfigHash, meta.ConfigHash)
	}
	if len(gotEntries) != 2 {
		t.Fatalf("got %d entries, want 2", len(gotEntries))
	}
	if gotEntries[0].Key != "ls(1)" || gotEntries[0].Hash != "aaa" {
		t.Errorf("entry 0: got %+v", gotEntries[0])
	}
	if gotEntries[1].Embedding[0] != 0.4 {
		t.Errorf("entry 1 embedding[0] = %f, want 0.4", gotEntries[1].Embedding[0])
	}
}

func TestBlobRoundTripEmpty(t *testing.T) {
	meta := IndexMeta{ConfigHash: "empty"}
	blob, err := MarshalIndexBlob(meta, nil)
	if err != nil {
		t.Fatalf("MarshalIndexBlob: %v", err)
	}

	gotMeta, gotEntries, err := UnmarshalIndexBlob(blob)
	if err != nil {
		t.Fatalf("UnmarshalIndexBlob: %v", err)
	}
	if gotMeta.ConfigHash != "empty" {
		t.Errorf("ConfigHash = %q, want empty", gotMeta.ConfigHash)
	}
	if len(gotEntries) != 0 {
		t.Errorf("got %d entries, want 0", len(gotEntries))
	}
}

func TestUnmarshalBlobEmptyInput(t *testing.T) {
	_, _, err := UnmarshalIndexBlob([]byte{})
	if err == nil {
		t.Error("expected error for empty blob")
	}
}

func TestUnmarshalBlobBadHeader(t *testing.T) {
	_, _, err := UnmarshalIndexBlob([]byte(`{"type":"wrong"}`))
	if err == nil {
		t.Error("expected error for wrong header type")
	}
}

