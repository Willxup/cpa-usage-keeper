package test

import (
	"math"
	"slices"
	"testing"

	"cpa-usage-keeper/internal/repository/percentile"
)

func TestNearestRankMatchesSortedReference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		values     []int64
		percentile float64
	}{
		{name: "empty", percentile: 0.95},
		{name: "single", values: []int64{42}, percentile: 0.95},
		{name: "unordered", values: []int64{900, 120, 450, 450, 300}, percentile: 0.95},
		{name: "many equal values", values: []int64{400, 400, 400, 100, 900, 400, 400}, percentile: 0.95},
		{name: "lower bound", values: []int64{9, 1, 5, 3, 7}, percentile: 0},
		{name: "upper bound", values: []int64{9, 1, 5, 3, 7}, percentile: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			values := slices.Clone(testCase.values)
			got := percentile.NearestRank(values, testCase.percentile)
			want := sortedNearestRank(testCase.values, testCase.percentile)
			if got != want {
				t.Fatalf("NearestRank(%v, %v) = %d, want %d", testCase.values, testCase.percentile, got, want)
			}
		})
	}
}

func TestNearestRankHandlesAdversarialOrder(t *testing.T) {
	t.Parallel()

	const count = 200000
	values := make([]int64, count)
	for index := range values {
		value := int64(index*2 + 1)
		if index >= count/2 {
			value = int64((index - count/2 + 1) * 2)
		}
		values[index] = value
	}

	if got := percentile.NearestRank(values, 0.95); got != 190000 {
		t.Fatalf("NearestRank adversarial p95 = %d, want 190000", got)
	}
}

func sortedNearestRank(values []int64, target float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	index := int(math.Ceil(target*float64(len(sorted)))) - 1
	index = max(0, min(index, len(sorted)-1))
	return sorted[index]
}
