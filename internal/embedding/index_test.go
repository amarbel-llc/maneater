package embedding

import (
	"testing"
)

func TestIndexSearchRanking(t *testing.T) {
	idx := NewIndex(3)
	idx.Add("similar", []float32{1, 2, 3})
	idx.Add("orthogonal", []float32{0, 0, 1})
	idx.Add("opposite", []float32{-1, -2, -3})

	results := idx.Search([]float32{1, 2, 3}, 3)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Page != "similar" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "similar")
	}
	if results[2].Page != "opposite" {
		t.Errorf("bottom result: got %q, want %q", results[2].Page, "opposite")
	}
}

func TestIndexSearchTopK(t *testing.T) {
	idx := NewIndex(2)
	idx.Add("a", []float32{1, 0})
	idx.Add("b", []float32{0, 1})
	idx.Add("c", []float32{1, 1})

	results := idx.Search([]float32{1, 0}, 1)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Page != "a" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "a")
	}
}

func TestIndexSearchDeduplicatesByPageName(t *testing.T) {
	idx := NewIndex(2)
	// Two entries for "sed" with different embeddings
	idx.Add("sed", []float32{0.5, 0.5}) // weaker match
	idx.Add("sed", []float32{1, 0})     // stronger match for query [1,0]
	idx.Add("ls", []float32{0, 1})      // different page

	results := idx.Search([]float32{1, 0}, 10)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (deduped)", len(results))
	}
	if results[0].Page != "sed" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "sed")
	}
	// The score should be from the stronger match [1,0], not the weaker [0.5,0.5]
	if results[0].Score < 0.99 {
		t.Errorf("sed score %.4f too low — dedup should keep the best match", results[0].Score)
	}
}

func TestIndexSearchEmpty(t *testing.T) {
	idx := NewIndex(3)
	results := idx.Search([]float32{1, 2, 3}, 5)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}
