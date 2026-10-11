package memory

import (
	"math"
	"sort"

	"crewstudio/internal/model"
)

const (
	// NearDuplicateThreshold is the cosine score from which a new memory is the same lesson as an existing one in the
	// same scope, for embedders that do not say their own (see Service.NearDuplicateSimilarity)
	NearDuplicateThreshold = 0.97
	// MaxVectorLen bounds the vectors a run may send to search with
	MaxVectorLen = 4096
)

// ValidVector reports whether v can be searched with: 1 to MaxVectorLen finite numbers.
func ValidVector(v []float32) bool {
	if len(v) == 0 || len(v) > MaxVectorLen {
		return false
	}
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}

// Cosine is the cosine similarity of a and b clamped to 0..1; vectors of different sizes, or of length zero, score 0.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return math.Max(0, math.Min(1, dot/(math.Sqrt(na)*math.Sqrt(nb))))
}

// Scored is a memory with its similarity to a query.
type Scored struct {
	Record model.MemoryRecord `json:"record"`
	Score  float64            `json:"score"`
}

// Rank scores rows against query and returns those at minScore or more, best first (newest first on a tie), at
// most limit of them (all when limit <= 0).
func Rank(query []float32, rows []model.MemoryRecord, minScore float64, limit int) []Scored {
	out := make([]Scored, 0, len(rows))
	for _, r := range rows {
		if s := Cosine(query, r.Vector); s >= minScore {
			out = append(out, Scored{Record: r, Score: s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Record.CreatedAt.After(out[j].Record.CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// NearDuplicate finds the row most similar to vec at threshold or more. Callers pass only the rows that count: the
// same scope, and what the caller may see.
func NearDuplicate(vec []float32, rows []model.MemoryRecord, threshold float64) (model.MemoryRecord, bool) {
	best := Rank(vec, rows, threshold, 1)
	if len(best) == 0 {
		return model.MemoryRecord{}, false
	}
	return best[0].Record, true
}
