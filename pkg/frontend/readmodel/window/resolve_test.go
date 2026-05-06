package window_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	readmodelwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/window"
)

type stubWeekWindowResolver struct{}

func (stubWeekWindowResolver) ResolveWeekWindow(context.Context, string, time.Time) (readmodelwindow.WeekWindow, error) {
	return readmodelwindow.WeekWindow{CurrentWeek: "2026-03-16"}, nil
}

func TestResolveSupportsExactTimestampWindow(t *testing.T) {
	t.Parallel()

	scope, err := readmodelwindow.Resolve(context.Background(), stubWeekWindowResolver{}, readmodelwindow.Request{
		StartAt: "2026-03-16T23:30",
		EndAt:   "2026-03-17T00:30",
	})
	if err != nil {
		t.Fatalf("resolve exact window: %v", err)
	}
	if !scope.HasExactBounds {
		t.Fatalf("expected exact bounds flag to be set")
	}
	if got, want := scope.StartAt, "2026-03-16T23:30:00Z"; got != want {
		t.Fatalf("unexpected normalized start_at: got=%q want=%q", got, want)
	}
	if got, want := scope.EndAt, "2026-03-17T00:30:00Z"; got != want {
		t.Fatalf("unexpected normalized end_at: got=%q want=%q", got, want)
	}
	if got, want := scope.StartDate, "2026-03-16"; got != want {
		t.Fatalf("unexpected derived start date: got=%q want=%q", got, want)
	}
	if got, want := scope.EndDate, "2026-03-17"; got != want {
		t.Fatalf("unexpected derived end date: got=%q want=%q", got, want)
	}
	wantDates := []string{"2026-03-16", "2026-03-17"}
	if !reflect.DeepEqual(scope.DateLabels, wantDates) {
		t.Fatalf("unexpected metric date labels: got=%v want=%v", scope.DateLabels, wantDates)
	}
}

func TestResolveRejectsMixedExactAndDateBounds(t *testing.T) {
	t.Parallel()

	_, err := readmodelwindow.Resolve(context.Background(), stubWeekWindowResolver{}, readmodelwindow.Request{
		StartAt:   "2026-03-16T08:30",
		EndAt:     "2026-03-16T12:30",
		StartDate: "2026-03-16",
		EndDate:   "2026-03-16",
	})
	if err == nil {
		t.Fatalf("expected mixed exact/date bounds to fail")
	}
	if !strings.Contains(err.Error(), "start_at/end_at cannot be combined") {
		t.Fatalf("unexpected mixed-bound error: %v", err)
	}
}

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
