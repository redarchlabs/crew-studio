package memory

import (
	"math"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func TestCosine(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{2, 0}, 1},
		{[]float32{1, 0}, []float32{0, 1}, 0},
		{[]float32{1, 0}, []float32{-1, 0}, 0}, // clamped
		{[]float32{1, 1}, []float32{1, 0}, 1 / math.Sqrt2},
		{[]float32{1}, []float32{1, 0}, 0},
		{nil, nil, 0},
		{[]float32{0, 0}, []float32{1, 0}, 0},
	}
	for _, tt := range tests {
		if got := Cosine(tt.a, tt.b); math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("Cosine(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidVector(t *testing.T) {
	if ValidVector(nil) || ValidVector(make([]float32, MaxVectorLen+1)) || ValidVector([]float32{float32(math.NaN())}) ||
		ValidVector([]float32{float32(math.Inf(1))}) {
		t.Error("bad vectors accepted")
	}
	if !ValidVector([]float32{0.1, -0.2}) {
		t.Error("good vector refused")
	}
}

func TestRankAndNearDuplicate(t *testing.T) {
	now := time.Now()
	rows := []model.MemoryRecord{
		{ID: "far", Vector: []float32{0, 1}, CreatedAt: now},
		{ID: "near-old", Vector: []float32{1, 0.01}, CreatedAt: now.Add(-time.Hour)},
		{ID: "near-new", Vector: []float32{1, 0.01}, CreatedAt: now},
		{ID: "mid", Vector: []float32{1, 1}, CreatedAt: now},
	}
	got := Rank([]float32{1, 0}, rows, 0.5, 2)
	if len(got) != 2 || got[0].Record.ID != "near-new" || got[1].Record.ID != "near-old" {
		t.Errorf("rank = %+v", got)
	}
	if all := Rank([]float32{1, 0}, rows, 0, 0); len(all) != 4 || all[3].Record.ID != "far" {
		t.Errorf("all = %+v", all)
	}
	if dup, ok := NearDuplicate([]float32{1, 0}, rows, NearDuplicateThreshold); !ok || dup.ID != "near-new" {
		t.Errorf("dup = %+v %v", dup, ok)
	}
	if _, ok := NearDuplicate([]float32{1, 1.5}, rows[:1], NearDuplicateThreshold); ok {
		t.Error("unrelated row is not a duplicate")
	}
	// The threshold is the embedder's: mid (cosine 0.86) is a duplicate only at a lower one
	if _, ok := NearDuplicate([]float32{1, 0.25}, rows[3:], NearDuplicateThreshold); ok {
		t.Error("mid is not a duplicate at the default threshold")
	}
	if dup, ok := NearDuplicate([]float32{1, 0.25}, rows[3:], 0.8); !ok || dup.ID != "mid" {
		t.Errorf("low threshold = %+v %v", dup, ok)
	}
}
