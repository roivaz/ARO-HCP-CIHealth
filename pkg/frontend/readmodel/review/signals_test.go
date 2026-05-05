package review_test

import (
	"context"
	"testing"

	readmodelreview "ci-failure-atlas/pkg/frontend/readmodel/review"
	"ci-failure-atlas/pkg/frontend/readmodel/testsupport"
)

func TestBuildWindowUsesDateWindowSignals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), testsupport.PreviousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelreview.BuildWindow(ctx, fixture.Service, readmodelreview.WindowQuery{
		StartDate: "2026-03-10",
		EndDate:   "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build review signals window: %v", err)
	}
	if got, want := data.Meta.StartDate, "2026-03-10"; got != want {
		t.Fatalf("unexpected start date: got=%q want=%q", got, want)
	}
	if got, want := data.Meta.EndDate, "2026-03-16"; got != want {
		t.Fatalf("unexpected end date: got=%q want=%q", got, want)
	}
	if got, want := data.Meta.Timezone, "UTC"; got != want {
		t.Fatalf("unexpected timezone: got=%q want=%q", got, want)
	}
	if got, want := data.SignalsByReason["new_pattern"], 1; got != want {
		t.Fatalf("unexpected new-pattern count: got=%d want=%d", got, want)
	}
	if got, want := data.SignalsByReason["recurrence"], 0; got != want {
		t.Fatalf("unexpected recurrence count: got=%d want=%d", got, want)
	}
	if got, want := data.TotalSignals, 1; got != want {
		t.Fatalf("unexpected total signal count: got=%d want=%d", got, want)
	}

	rowsByReason := map[string]readmodelreview.ReviewSignalRow{}
	for _, row := range data.Rows {
		rowsByReason[row.Reason] = row
	}
	if got, want := rowsByReason["new_pattern"].ProposedFailurePattern, "Installer failed to reach bootstrap machine"; got != want {
		t.Fatalf("unexpected new-pattern phrase: got=%q want=%q", got, want)
	}
	if _, ok := rowsByReason["recurrence"]; ok {
		t.Fatalf("did not expect recurrence row: %+v", rowsByReason["recurrence"])
	}
}

func TestBuildWindowMarksRecurrenceWhenPriorPatternFallsOutsideComparisonWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), testsupport.PreviousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelreview.BuildWindow(ctx, fixture.Service, readmodelreview.WindowQuery{
		StartDate: "2026-03-16",
		EndDate:   "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build review signals window: %v", err)
	}
	if got, want := data.SignalsByReason["new_pattern"], 1; got != want {
		t.Fatalf("unexpected new-pattern count: got=%d want=%d", got, want)
	}
	if got, want := data.SignalsByReason["recurrence"], 1; got != want {
		t.Fatalf("unexpected recurrence count: got=%d want=%d", got, want)
	}

	rowsByReason := map[string]readmodelreview.ReviewSignalRow{}
	for _, row := range data.Rows {
		rowsByReason[row.Reason] = row
	}
	if got, want := rowsByReason["recurrence"].ProposedFailurePattern, "OAuth timeout while waiting for cluster <cluster>"; got != want {
		t.Fatalf("unexpected recurrence phrase: got=%q want=%q", got, want)
	}
	if got, want := rowsByReason["recurrence"].Phase, "crosswindow"; got != want {
		t.Fatalf("unexpected recurrence phase: got=%q want=%q", got, want)
	}
}
