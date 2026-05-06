package reports_test

import (
	"context"
	"testing"
	"time"

	readmodelreports "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/reports"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/testsupport"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestBuildWeeklyReportDataBuildsCurrentAndPreviousReadModels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")
	previousStore := fixture.OpenWeekStore(t, "2026-03-09")

	if err := currentStore.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), testsupport.PreviousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, testsupport.ReportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	if err := currentStore.UpsertTestMetadataDaily(ctx, testsupport.ReportTestMetadataDaily()); err != nil {
		t.Fatalf("seed test metadata daily: %v", err)
	}

	data, err := readmodelreports.BuildWeeklyReportData(ctx, currentStore, previousStore, readmodelreports.WeeklyReportBuildOptions{
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
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")

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
	if err := currentStore.UpsertMetricsDaily(ctx, testsupport.ReportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := readmodelreports.BuildWeeklyReportData(ctx, currentStore, nil, readmodelreports.WeeklyReportBuildOptions{
		StartDate:  time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		TargetRate: 95.0,
		Week:       "2026-03-16",
	})
	if err != nil {
		t.Fatalf("build weekly report data: %v", err)
	}

	rowsByPhrase := map[string]readmodelreports.WeeklyTopSignature{}
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
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")
	previousStore := fixture.OpenWeekStore(t, "2026-03-09")

	if err := currentStore.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), testsupport.PreviousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, testsupport.ReportMetricsDaily()); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}
	if err := currentStore.UpsertTestMetadataDaily(ctx, testsupport.ReportTestMetadataDaily()); err != nil {
		t.Fatalf("seed test metadata daily: %v", err)
	}

	data, err := readmodelreports.BuildWeeklyReportData(ctx, currentStore, previousStore, readmodelreports.WeeklyReportBuildOptions{
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

func weeklyEnvReportByName(
	t *testing.T,
	reports []readmodelreports.WeeklyEnvReport,
	environment string,
) readmodelreports.WeeklyEnvReport {
	t.Helper()
	for _, report := range reports {
		if report.Environment == environment {
			return report
		}
	}
	t.Fatalf("missing environment report %q", environment)
	return readmodelreports.WeeklyEnvReport{}
}
