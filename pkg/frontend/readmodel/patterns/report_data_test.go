package patterns_test

import (
	"context"
	"testing"

	readmodelpatterns "ci-failure-atlas/pkg/frontend/readmodel/patterns"
	"ci-failure-atlas/pkg/frontend/readmodel/testsupport"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

func TestBuildFailurePatternReportDataIgnoresDeprecatedPhase3Links(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, testsupport.SampleRunsFixture()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	rawFailures := append(testsupport.SampleRawFailuresFixture(), storecontracts.RawFailureRecord{
		Environment:    "dev",
		RowID:          "row-4",
		RunURL:         "https://prow.example.com/view/1",
		TestName:       "should throttle",
		TestSuite:      "suite-c",
		SignatureID:    "sig-c",
		OccurredAt:     "2026-03-16T08:07:00Z",
		RawText:        "API throttling while reconciling install state",
		NormalizedText: "api throttling while reconciling install state",
	})
	if err := store.UpsertRawFailures(ctx, rawFailures); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := store.UpsertMetricsDaily(ctx, testsupport.ReportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	fixture.SeedDeprecatedPhase3Links(t,
		testsupport.DeprecatedPhase3LinkRecord{
			IssueID:     "QE-123",
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			RowID:       "row-1",
			UpdatedAt:   "2026-03-16T12:00:00Z",
		},
		testsupport.DeprecatedPhase3LinkRecord{
			IssueID:     "QE-123",
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			RowID:       "row-4",
			UpdatedAt:   "2026-03-16T12:00:00Z",
		},
	)

	data, err := readmodelpatterns.BuildFailurePatternReportData(ctx, store, readmodelpatterns.FailurePatternReportBuildOptions{
		Week:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure-pattern report data: %v", err)
	}

	if got, want := len(data.FailurePatternClusters), 3; got != want {
		t.Fatalf("unexpected cluster count after deprecated phase3 seeding: got=%d want=%d", got, want)
	}
	phrases := map[string]struct{}{}
	for _, row := range data.FailurePatternClusters {
		phrases[row.CanonicalEvidencePhrase] = struct{}{}
	}
	if _, ok := phrases["OAuth timeout while waiting for cluster <cluster>"]; !ok {
		t.Fatalf("missing oauth phrase: %+v", data.FailurePatternClusters)
	}
	if _, ok := phrases["Installer failed to reach bootstrap machine"]; !ok {
		t.Fatalf("missing installer phrase: %+v", data.FailurePatternClusters)
	}
	if _, ok := phrases["API throttling while reconciling install state"]; !ok {
		t.Fatalf("missing throttling phrase: %+v", data.FailurePatternClusters)
	}
	for _, row := range data.FailurePatternClusters {
		if len(row.LinkedChildren) != 0 {
			t.Fatalf("expected deprecated phase3 links to produce no linked children, got=%d for %s", len(row.LinkedChildren), row.Phase2ClusterID)
		}
	}
}

func TestBuildFailurePatternReportDataProjectsSamplesAndCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, testsupport.SampleRunsFixture()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, testsupport.SampleRawFailuresFixture()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := store.UpsertMetricsDaily(ctx, testsupport.ReportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := readmodelpatterns.BuildFailurePatternReportData(ctx, store, readmodelpatterns.FailurePatternReportBuildOptions{
		Week:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure-pattern report data: %v", err)
	}

	if got, want := len(data.FailurePatternClusters), 2; got != want {
		t.Fatalf("unexpected failure-pattern count: got=%d want=%d", got, want)
	}
	if got, want := data.TargetEnvironments[0], "dev"; got != want {
		t.Fatalf("unexpected target environment: got=%q want=%q", got, want)
	}
	phrases := map[string]bool{}
	for _, cluster := range data.FailurePatternClusters {
		if got, want := cluster.Environment, "dev"; got != want {
			t.Fatalf("unexpected cluster environment: got=%q want=%q", got, want)
		}
		if len(cluster.FullErrorSamples) == 0 {
			t.Fatalf("expected failure pattern to include full error samples")
		}
		phrases[cluster.CanonicalEvidencePhrase] = true
	}
	if !phrases["OAuth timeout while waiting for cluster <cluster>"] {
		t.Fatalf("missing oauth phrase: %+v", data.FailurePatternClusters)
	}
	if !phrases["Installer failed to reach bootstrap machine"] {
		t.Fatalf("missing installer phrase: %+v", data.FailurePatternClusters)
	}
	if got, want := data.OverallJobsByEnvironment["dev"], 7; got != want {
		t.Fatalf("unexpected overall jobs: got=%d want=%d", got, want)
	}
}
