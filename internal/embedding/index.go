package embedding

import (
	"sort"
)

type Entry struct {
	Page      string
	Embedding []float32
}

type Index struct {
	Entries []Entry
	Dim     int
}

type Result struct {
	Page  string
	Score float64
}

func NewIndex(dim int) *Index {
	return &Index{Dim: dim}
}

func (idx *Index) Add(page string, embedding []float32) {
	idx.Entries = append(idx.Entries, Entry{
		Page:      page,
		Embedding: embedding,
	})
}

func (idx *Index) Search(query []float32, topK int) []Result {
	// Compute scores, keeping only the best score per page name.
	best := make(map[string]float64)
	for _, e := range idx.Entries {
		score := CosineSimilarity(query, e.Embedding)
		if prev, ok := best[e.Page]; !ok || score > prev {
			best[e.Page] = score
		}
	}

	results := make([]Result, 0, len(best))
	for page, score := range best {
		results = append(results, Result{Page: page, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	return results
}

