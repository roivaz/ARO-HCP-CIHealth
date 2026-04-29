package readmodel

import (
	"context"
	"testing"
)

func TestBuildReviewSignalsWeekUsesInlineHistorySignals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(sampleRunsFixture(), previousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, append(sampleRawFailuresFixture(), previousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := fixture.service.BuildReviewSignalsWeek(ctx, "2026-03-16")
	if err != nil {
		t.Fatalf("build review signals week: %v", err)
	}
	if got, want := data.Week, "2026-03-16"; got != want {
		t.Fatalf("unexpected week: got=%q want=%q", got, want)
	}
	if got, want := data.PreviousWeek, "2026-03-09"; got != want {
		t.Fatalf("unexpected previous week: got=%q want=%q", got, want)
	}
	if got, want := data.Timezone, "UTC"; got != want {
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

	rowsByReason := map[string]ReviewSignalRow{}
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
