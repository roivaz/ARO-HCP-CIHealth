package readmodel

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestFilterAvailableWeeks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested []string
		available []string
		want      []string
	}{
		{
			name:      "all available",
			requested: []string{"2026-03-09", "2026-03-16"},
			available: []string{"2026-03-09", "2026-03-16"},
			want:      []string{"2026-03-09", "2026-03-16"},
		},
		{
			name:      "trailing missing",
			requested: []string{"2026-03-09", "2026-03-16"},
			available: []string{"2026-03-09"},
			want:      []string{"2026-03-09"},
		},
		{
			name:      "leading missing",
			requested: []string{"2026-03-09", "2026-03-16"},
			available: []string{"2026-03-16"},
			want:      []string{"2026-03-16"},
		},
		{
			name:      "none available",
			requested: []string{"2026-03-09", "2026-03-16"},
			available: []string{"2026-03-02"},
			want:      []string{},
		},
		{
			name:      "middle of three available",
			requested: []string{"2026-03-09", "2026-03-16", "2026-03-23"},
			available: []string{"2026-03-16"},
			want:      []string{"2026-03-16"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterAvailableWeeks(tc.requested, tc.available)
			if len(got) != len(tc.want) {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d]=%q want=%q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestInteriorGap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		weeks []string
		want  string
	}{
		{name: "empty", weeks: nil, want: ""},
		{name: "single", weeks: []string{"2026-03-09"}, want: ""},
		{name: "contiguous two", weeks: []string{"2026-03-09", "2026-03-16"}, want: ""},
		{name: "contiguous three", weeks: []string{"2026-03-09", "2026-03-16", "2026-03-23"}, want: ""},
		{
			name:  "gap in middle of three",
			weeks: []string{"2026-03-09", "2026-03-23"},
			want:  "2026-03-16",
		},
		{
			name:  "gap in middle of four",
			weeks: []string{"2026-03-02", "2026-03-09", "2026-03-23"},
			want:  "2026-03-16",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := interiorGap(tc.weeks)
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestResolveWindowUsesCalendarWeeksForAnyRange(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationFixture(t, "")
	scope, err := fixture.service.ResolveWindow(context.Background(), WindowRequest{
		StartDate: "2026-03-10",
		EndDate:   "2026-03-26",
	})
	if err != nil {
		t.Fatalf("resolve window: %v", err)
	}
	if got, want := scope.SemanticWeeks, []string{"2026-03-09", "2026-03-16", "2026-03-23"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected semantic weeks: got=%v want=%v", got, want)
	}
	if got, want := scope.AnchorWeek, "2026-03-23"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
}

func TestResolveWindowDoesNotRequireStoredWeeks(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationFixture(t, "")
	scope, err := fixture.service.ResolveWindow(context.Background(), WindowRequest{
		StartDate: "2026-03-16",
		EndDate:   "2026-03-22",
	})
	if err != nil {
		t.Fatalf("resolve window without fact weeks: %v", err)
	}
	if got, want := scope.SemanticWeeks, []string{"2026-03-16"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected semantic weeks: got=%v want=%v", got, want)
	}
}

func TestResolveWindowSprintLikeRangeKeepsCalendarWeekBoundaries(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationFixture(t, "")
	scope, err := fixture.service.ResolveWindow(context.Background(), WindowRequest{
		StartDate: "2026-03-13",
		EndDate:   "2026-03-26",
		Now:       time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("resolve sprint-like window: %v", err)
	}
	if got, want := scope.SemanticWeeks, []string{"2026-03-09", "2026-03-16", "2026-03-23"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected semantic weeks: got=%v want=%v", got, want)
	}
	if got, want := scope.EndDate, "2026-03-26"; got != want {
		t.Fatalf("end date should be preserved: got=%q want=%q", got, want)
	}
}
