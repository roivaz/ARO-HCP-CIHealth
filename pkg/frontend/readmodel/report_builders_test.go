package readmodel

import (
	"context"
	"testing"
	"time"

	failurepatterncontracts "ci-failure-atlas/pkg/failurepatterns/contracts"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

func TestBuildFailurePatternReportDataIgnoresDeprecatedPhase3Links(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, sampleRunsFixture()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	rawFailures := append(sampleRawFailuresFixture(), storecontracts.RawFailureRecord{
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
	if err := store.UpsertMetricsDaily(ctx, reportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	fixture.seedDeprecatedPhase3Links(t,
		deprecatedPhase3LinkRecord{
			SchemaVersion: failurepatterncontracts.CurrentSchemaVersion,
			IssueID:       "QE-123",
			Environment:   "dev",
			RunURL:        "https://prow.example.com/view/1",
			RowID:         "row-1",
			UpdatedAt:     "2026-03-16T12:00:00Z",
		},
		deprecatedPhase3LinkRecord{
			SchemaVersion: failurepatterncontracts.CurrentSchemaVersion,
			IssueID:       "QE-123",
			Environment:   "dev",
			RunURL:        "https://prow.example.com/view/1",
			RowID:         "row-4",
			UpdatedAt:     "2026-03-16T12:00:00Z",
		},
	)

	data, err := BuildFailurePatternReportData(ctx, store, FailurePatternReportBuildOptions{
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
	fixture := newIntegrationFixture(t, "")
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, sampleRunsFixture()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, sampleRawFailuresFixture()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := store.UpsertMetricsDaily(ctx, reportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := BuildFailurePatternReportData(ctx, store, FailurePatternReportBuildOptions{
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

func TestBuildWeeklyReportDataBuildsCurrentAndPreviousReadModels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	currentStore := fixture.openWeekStore(t, "2026-03-16")
	previousStore := fixture.openWeekStore(t, "2026-03-09")

	if err := currentStore.UpsertRuns(ctx, append(sampleRunsFixture(), previousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(sampleRawFailuresFixture(), previousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, reportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	if err := currentStore.UpsertTestMetadataDaily(ctx, reportTestMetadataDaily()); err != nil {
		t.Fatalf("seed test metadata daily: %v", err)
	}

	data, err := BuildWeeklyReportData(ctx, currentStore, previousStore, WeeklyReportBuildOptions{
		StartDate:  time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		TargetRate: 95.0,
		Week:       "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build weekly report data: %v", err)
	}

	if got, want := data.StartDate.Format("2006-01-02"), "2026-03-16"; got != want {
		t.Fatalf("unexpected start date: got=%q want=%q", got, want)
	}
	if got, want := data.EndDate.Format("2006-01-02"), "2026-03-22"; got != want {
		t.Fatalf("unexpected end date: got=%q want=%q", got, want)
	}
	devReport := weeklyEnvReportByName(t, data.CurrentReports, "dev")
	if got, want := devReport.Days[0].Counts.RunCount, 7; got != want {
		t.Fatalf("unexpected current run count: got=%d want=%d", got, want)
	}
	if got, want := len(data.TopSignaturesByEnv["dev"]), 2; got != want {
		t.Fatalf("unexpected top signature count: got=%d want=%d", got, want)
	}
	if got, want := data.TopSignaturesByEnv["dev"][0].Phrase, "OAuth timeout while waiting for cluster <cluster>"; got != want {
		t.Fatalf("unexpected top signature phrase: got=%q want=%q", got, want)
	}
	if got, want := len(data.TestsBelowTargetByEnv["dev"]), 1; got != want {
		t.Fatalf("unexpected below-target tests count: got=%d want=%d", got, want)
	}
	if got, want := data.TestsBelowTargetByEnv["dev"][0].TestName, "should oauth"; got != want {
		t.Fatalf("unexpected below-target test name: got=%q want=%q", got, want)
	}
	if got, want := data.PreviousSemantic.ByEnvironment["dev"].FailurePatternClusters, 1; got != want {
		t.Fatalf("unexpected previous semantic failure-pattern count: got=%d want=%d", got, want)
	}
}

func TestBuildWeeklyReportDataUsesRowLevelReferencesForTopSignatureSamples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	currentStore := fixture.openWeekStore(t, "2026-03-16")

	if err := currentStore.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-finalize",
			RunURL:         "https://prow.example.com/view/shared",
			TestName:       "finalize step",
			TestSuite:      "suite-a",
			SignatureID:    "sig-shared",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "failed post-install: resource not ready, name: finalize-mce-config",
			NormalizedText: "finalize-mce-config timeout",
		},
		{
			Environment:    "dev",
			RowID:          "row-propagator",
			RunURL:         "https://prow.example.com/view/shared",
			TestName:       "propagator step",
			TestSuite:      "suite-a",
			SignatureID:    "sig-shared",
			OccurredAt:     "2026-03-16T08:05:00Z",
			RawText:        "resource not ready, name: grc-policy-propagator",
			NormalizedText: "grc-policy-propagator timeout",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/shared",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, reportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := BuildWeeklyReportData(ctx, currentStore, nil, WeeklyReportBuildOptions{
		StartDate:  time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		TargetRate: 95.0,
		Week:       "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build weekly report data: %v", err)
	}

	rowsByPhrase := map[string]WeeklyTopSignature{}
	for _, row := range data.TopSignaturesByEnv["dev"] {
		rowsByPhrase[row.Phrase] = row
	}

	finalizeRow, ok := rowsByPhrase["failed post-install: resource not ready, name: finalize-mce-config"]
	if !ok {
		t.Fatalf("missing finalize top signature: %+v", data.TopSignaturesByEnv["dev"])
	}
	if got, want := len(finalizeRow.FullErrorSamples), 1; got != want {
		t.Fatalf("unexpected finalize sample count: got=%d want=%d", got, want)
	}
	if got, want := finalizeRow.FullErrorSamples[0], "failed post-install: resource not ready, name: finalize-mce-config"; got != want {
		t.Fatalf("unexpected finalize sample: got=%q want=%q", got, want)
	}

	propagatorRow, ok := rowsByPhrase["resource not ready, name: grc-policy-propagator"]
	if !ok {
		t.Fatalf("missing propagator top signature: %+v", data.TopSignaturesByEnv["dev"])
	}
	if got, want := len(propagatorRow.FullErrorSamples), 1; got != want {
		t.Fatalf("unexpected propagator sample count: got=%d want=%d", got, want)
	}
	if got, want := propagatorRow.FullErrorSamples[0], "resource not ready, name: grc-policy-propagator"; got != want {
		t.Fatalf("unexpected propagator sample: got=%q want=%q", got, want)
	}
}

func TestBuildWeeklyReportDataBuildsInlineComparisonWithoutStoredSchemas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newIntegrationFixture(t, "")
	currentStore := fixture.openWeekStore(t, "2026-03-16")
	previousStore := fixture.openWeekStore(t, "2026-03-09")

	if err := currentStore.UpsertRuns(ctx, append(sampleRunsFixture(), previousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(sampleRawFailuresFixture(), previousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, reportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	if err := currentStore.UpsertTestMetadataDaily(ctx, reportTestMetadataDaily()); err != nil {
		t.Fatalf("seed test metadata daily: %v", err)
	}

	data, err := BuildWeeklyReportData(ctx, currentStore, previousStore, WeeklyReportBuildOptions{
		StartDate:  time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		TargetRate: 95.0,
		Week:       "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build weekly report data: %v", err)
	}
	if got, want := data.PreviousSemantic.ByEnvironment["dev"].FailurePatternClusters, 1; got != want {
		t.Fatalf("unexpected previous inline failure-pattern count: got=%d want=%d", got, want)
	}
}

func weeklyEnvReportByName(t *testing.T, reports []WeeklyEnvReport, environment string) WeeklyEnvReport {
	t.Helper()
	for _, report := range reports {
		if report.Environment == environment {
			return report
		}
	}
	t.Fatalf("missing environment report %q", environment)
	return WeeklyEnvReport{}
}

func reportMetricsDaily() []storecontracts.MetricDailyRecord {
	return []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 7},
		{Environment: "dev", Date: "2026-03-16", Metric: "failure_count", Value: 2},
		{Environment: "dev", Date: "2026-03-16", Metric: "failed_e2e_run_count", Value: 2},
		{Environment: "dev", Date: "2026-03-16", Metric: "post_good_run_count", Value: 4},
		{Environment: "dev", Date: "2026-03-16", Metric: "post_good_failed_e2e_jobs", Value: 1},
		{Environment: "dev", Date: "2026-03-09", Metric: "run_count", Value: 5},
		{Environment: "dev", Date: "2026-03-09", Metric: "failure_count", Value: 1},
		{Environment: "dev", Date: "2026-03-09", Metric: "failed_e2e_run_count", Value: 1},
	}
}

func reportTestMetadataDaily() []storecontracts.TestMetadataDailyRecord {
	return []storecontracts.TestMetadataDailyRecord{
		{
			Environment:            "dev",
			Date:                   "2026-03-16",
			Period:                 "default",
			TestName:               "should oauth",
			TestSuite:              "suite-a",
			CurrentPassPercentage:  90.0,
			CurrentRuns:            12,
			PreviousPassPercentage: 95.0,
			PreviousRuns:           10,
			NetImprovement:         -5.0,
		},
	}
}
