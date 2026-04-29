package window_test

import (
	"context"
	"errors"
	"testing"
	"time"

	readmodelwindow "ci-failure-atlas/pkg/frontend/readmodel/window"
)

type unexpectedWeekResolver struct{}

func (unexpectedWeekResolver) ResolveWeekWindow(context.Context, string, time.Time) (readmodelwindow.WeekWindow, error) {
	return readmodelwindow.WeekWindow{}, errors.New("ResolveWeekWindow should not be called")
}

func TestResolveUsesCalendarAnchorWeekForAnyRange(t *testing.T) {
	t.Parallel()

	scope, err := readmodelwindow.Resolve(context.Background(), unexpectedWeekResolver{}, readmodelwindow.Request{
		StartDate: "2026-03-10",
		EndDate:   "2026-03-26",
	})
	if err != nil {
		t.Fatalf("resolve window: %v", err)
	}
	if got, want := scope.AnchorWeek, "2026-03-23"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
	if got, want := scope.StartDate, "2026-03-10"; got != want {
		t.Fatalf("unexpected start date: got=%q want=%q", got, want)
	}
	if got, want := scope.EndDate, "2026-03-26"; got != want {
		t.Fatalf("unexpected end date: got=%q want=%q", got, want)
	}
}

func TestResolveDoesNotRequireStoredWeeks(t *testing.T) {
	t.Parallel()

	scope, err := readmodelwindow.Resolve(context.Background(), unexpectedWeekResolver{}, readmodelwindow.Request{
		StartDate: "2026-03-16",
		EndDate:   "2026-03-22",
	})
	if err != nil {
		t.Fatalf("resolve window without fact weeks: %v", err)
	}
	if got, want := scope.AnchorWeek, "2026-03-16"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
}

func TestResolveSprintLikeRangeKeepsCalendarAnchorWeek(t *testing.T) {
	t.Parallel()

	scope, err := readmodelwindow.Resolve(context.Background(), unexpectedWeekResolver{}, readmodelwindow.Request{
		StartDate: "2026-03-13",
		EndDate:   "2026-03-26",
		Now:       time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("resolve sprint-like window: %v", err)
	}
	if got, want := scope.AnchorWeek, "2026-03-23"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
	if got, want := scope.EndDate, "2026-03-26"; got != want {
		t.Fatalf("end date should be preserved: got=%q want=%q", got, want)
	}
}
