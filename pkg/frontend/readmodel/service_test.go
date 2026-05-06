package readmodel_test

import (
	"context"
	"testing"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/testsupport"
)

func TestDiscoverAvailableWeeksUsesFactDates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}

	weeks, err := fixture.Service.DiscoverAvailableWeeks(ctx)
	if err != nil {
		t.Fatalf("discover available weeks: %v", err)
	}
	if len(weeks) != 2 || weeks[0] != "2026-03-09" || weeks[1] != "2026-03-16" {
		t.Fatalf("unexpected loadable weeks: %+v", weeks)
	}
}
