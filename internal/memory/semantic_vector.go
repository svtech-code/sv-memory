package memory

import (
	"database/sql"
	"math"
	"sort"
	"strings"
	"unicode"
)

// SubwordVector represents an L2-normalized sparse vector for subword/character n-grams.
type SubwordVector map[string]float64

// extractSubwords builds a term-frequency map of words and character 3-grams.
func extractSubwords(text string) SubwordVector {
	vec := make(SubwordVector)
	if text == "" {
		return vec
	}

	text = strings.ToLower(text)
	var current strings.Builder
	var words []string

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	for _, w := range words {
		if stopWords[w] {
			continue
		}
		// Word unigram weight
		vec["w:"+w] += 2.0

		// Character 3-grams for substring and morphological similarity
		runes := []rune(w)
		if len(runes) >= 3 {
			for i := 0; i <= len(runes)-3; i++ {
				gram := string(runes[i : i+3])
				vec["g:"+gram] += 0.5
			}
		}
	}

	// L2 Normalize
	var sumSq float64
	for _, val := range vec {
		sumSq += val * val
	}
	if sumSq > 0 {
		norm := math.Sqrt(sumSq)
		for k, val := range vec {
			vec[k] = val / norm
		}
	}

	return vec
}

// CosineSimilarity computes the cosine similarity between two normalized SubwordVectors (0.0 to 1.0).
func CosineSimilarity(a, b SubwordVector) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Iterate over the smaller vector for speed
	if len(a) > len(b) {
		a, b = b, a
	}

	var dot float64
	for k, valA := range a {
		if valB, ok := b[k]; ok {
			dot += valA * valB
		}
	}

	if dot > 1.0 {
		return 1.0
	}
	if dot < 0.0 {
		return 0.0
	}
	return dot
}

// SearchMemoriesHybrid performs hybrid search: it runs Tri-Factor FTS5 BM25 search,
// and if recall is low or terms vary, expands candidate evaluation using local vector
// cosine similarity to ensure relevant memories are never missed.
func SearchMemoriesHybrid(db *sql.DB, projectID, searchTerm, category, pathFilter, matchMode string, paths []string, limit, offset int) ([]*MemorySearchResult, error) {
	// 1. Primary Tri-Factor FTS5 Search
	results, err := searchMemoriesCompact(db, projectID, searchTerm, category, pathFilter, matchMode, paths, limit, offset)
	if err != nil {
		return nil, err
	}

	// If we got enough results or query is empty, return primary results
	if len(results) >= limit || searchTerm == "" {
		return results, nil
	}

	// 2. Query expansion fallback: if matchMode was "all" and returned fewer than limit results,
	// try "any" match mode to broaden candidate pool
	seen := make(map[string]bool, len(results))
	for _, r := range results {
		seen[r.ID] = true
	}

	broadResults, err := searchMemoriesCompact(db, projectID, searchTerm, category, pathFilter, "any", paths, limit*2, 0)
	if err != nil {
		return results, nil
	}

	queryVec := extractSubwords(searchTerm)
	var additional []*MemorySearchResult

	for _, r := range broadResults {
		if seen[r.ID] {
			continue
		}
		memVec := extractSubwords(r.What)
		sim := CosineSimilarity(queryVec, memVec)
		if sim >= 0.15 { // Relevant semantic similarity threshold
			// Adjust composite score with semantic similarity
			if r.Score == 0 {
				r.Score = -sim
			} else {
				r.Score = r.Score * (1.0 + sim*0.5)
			}
			additional = append(additional, r)
			seen[r.ID] = true
		}
	}

	if len(additional) > 0 {
		sort.SliceStable(additional, func(i, j int) bool {
			return additional[i].Score < additional[j].Score // lower/more negative score ranks higher
		})
		for _, r := range additional {
			results = append(results, r)
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}
