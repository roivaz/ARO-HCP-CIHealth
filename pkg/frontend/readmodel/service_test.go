package readmodel

import (
	"context"
	"testing"
)

func TestDiscoverSemanticWeeksUsesFactDates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(sampleRunsFixture(), previousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}

	weeks, err := fixture.service.DiscoverSemanticWeeks(ctx)
	if err != nil {
		t.Fatalf("discover semantic weeks: %v", err)
	}
	if len(weeks) != 2 || weeks[0] != "2026-03-09" || weeks[1] != "2026-03-16" {
		t.Fatalf("unexpected loadable weeks: %+v", weeks)
	}
}
