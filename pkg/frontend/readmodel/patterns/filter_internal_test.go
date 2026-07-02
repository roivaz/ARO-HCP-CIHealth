package patterns

import (
	"reflect"
	"testing"
)

func TestNormalizeFailurePatternsSources(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "empty", input: nil, want: nil},
		{name: "single", input: []string{"alert"}, want: []string{"alert"}},
		{name: "trims and lowercases", input: []string{" E2E "}, want: []string{"e2e"}},
		{name: "drops unknown values", input: []string{"alert", "bogus", "unknown"}, want: []string{"alert"}},
		{name: "dedupes preserving order", input: []string{"e2e", "alert", "e2e"}, want: []string{"e2e", "alert"}},
		{name: "only invalid collapses to nil", input: []string{"bogus"}, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := normalizeFailurePatternsSources(testCase.input)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("normalizeFailurePatternsSources(%v) = %v, want %v", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestFilterFailurePatternsRowsBySource(t *testing.T) {
	t.Parallel()

	rows := []FailurePatternsRow{
		{ClusterID: "p", Lane: "provision"},
		{ClusterID: "e", Lane: "e2e"},
		{ClusterID: "a", Lane: "alert"},
		{ClusterID: "u", Lane: "unknown"},
	}

	if got := filterFailurePatternsRowsBySource(rows, nil); len(got) != len(rows) {
		t.Fatalf("empty filter should keep all rows, got %d want %d", len(got), len(rows))
	}

	got := filterFailurePatternsRowsBySource(rows, []string{"alert"})
	if len(got) != 1 || got[0].ClusterID != "a" {
		t.Fatalf("alert filter = %+v, want single alert row", got)
	}

	// "unknown" lane maps to the "other" bucket.
	otherRows := filterFailurePatternsRowsBySource(rows, []string{"other"})
	if len(otherRows) != 1 || otherRows[0].ClusterID != "u" {
		t.Fatalf("other filter = %+v, want single unknown-lane row", otherRows)
	}

	multi := filterFailurePatternsRowsBySource(rows, []string{"provision", "e2e"})
	if len(multi) != 2 {
		t.Fatalf("multi filter = %+v, want 2 rows", multi)
	}
}
