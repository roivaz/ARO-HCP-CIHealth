package runlog_test

import (
	"context"
	"strings"
	"testing"

	readmodelrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/runlog"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/testsupport"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestBuildDayBuildsMatchedAndUnmatchedRuns(t *testing.T) {
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

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	if got, want := data.Meta.AnchorWeek, "2026-03-16"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
	environment := jobHistoryEnvironmentByName(t, data, "dev")
	if got, want := environment.Summary.TotalRuns, 2; got != want {
		t.Fatalf("unexpected total runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.FailedRuns, 2; got != want {
		t.Fatalf("unexpected failed runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsWithRawFailures, 2; got != want {
		t.Fatalf("unexpected runs with raw failures: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsWithSemanticAttachment, 2; got != want {
		t.Fatalf("unexpected runs with semantic attachment: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsUnmatchedSignatures, 0; got != want {
		t.Fatalf("unexpected runs with unmatched signatures: got=%d want=%d", got, want)
	}

	matchedRun := jobHistoryRunByURL(t, environment, "https://prow.example.com/view/1")
	if got, want := matchedRun.SemanticRollups.AttachmentSummary, "single_clustered"; got != want {
		t.Fatalf("unexpected matched run summary: got=%q want=%q", got, want)
	}
	if got, want := matchedRun.SemanticRollups.SignatureCount, 1; got != want {
		t.Fatalf("unexpected matched run signature count: got=%d want=%d", got, want)
	}
	if got, want := matchedRun.FailedTestCount, 1; got != want {
		t.Fatalf("unexpected matched run failed test count: got=%d want=%d", got, want)
	}
	if got, want := matchedRun.SemanticRollups.ClusteredRows, 2; got != want {
		t.Fatalf("unexpected clustered row count: got=%d want=%d", got, want)
	}
	if got, want := len(matchedRun.Lanes), 1; got != want {
		t.Fatalf("unexpected matched run lane count: got=%d want=%d", got, want)
	}
	if got, want := matchedRun.Lanes[0], "unknown"; got != want {
		t.Fatalf("unexpected matched run lane: got=%q want=%q", got, want)
	}
	if got, want := len(matchedRun.FailureRows), 2; got != want {
		t.Fatalf("unexpected matched run failure row count: got=%d want=%d", got, want)
	}
	if got, want := matchedRun.FailureRows[0].Lane, "unknown"; got != want {
		t.Fatalf("unexpected matched failure row lane: got=%q want=%q", got, want)
	}
	if got, want := matchedRun.FailureRows[0].SemanticAttachment.CanonicalEvidencePhrase, "OAuth timeout while waiting for cluster <cluster>"; got != want {
		t.Fatalf("unexpected matched phrase: got=%q want=%q", got, want)
	}

	unmatchedRun := jobHistoryRunByURL(t, environment, "https://prow.example.com/view/2")
	if got, want := unmatchedRun.SemanticRollups.AttachmentSummary, "single_clustered"; got != want {
		t.Fatalf("unexpected unmatched run summary: got=%q want=%q", got, want)
	}
	if got, want := unmatchedRun.SemanticRollups.UnmatchedRows, 0; got != want {
		t.Fatalf("unexpected unmatched row count: got=%d want=%d", got, want)
	}
	if got, want := unmatchedRun.FailedTestCount, 1; got != want {
		t.Fatalf("unexpected unmatched run failed test count: got=%d want=%d", got, want)
	}
	if got, want := unmatchedRun.FailureRows[0].SemanticAttachment.Status, "clustered"; got != want {
		t.Fatalf("unexpected unmatched row status: got=%q want=%q", got, want)
	}
}

func TestBuildDayUsesCalendarAnchorWeekForNewerDates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/newer",
			JobName:     "periodic-ci-newer",
			Failed:      true,
			OccurredAt:  "2026-04-21T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-newer",
			RunURL:         "https://prow.example.com/view/newer",
			TestName:       "newer failure",
			TestSuite:      "suite-newer",
			SignatureID:    "sig-newer",
			OccurredAt:     "2026-04-21T08:00:00Z",
			RawText:        "newer failure text",
			NormalizedText: "newer failure text",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-04-21",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	if got, want := data.Meta.Date, "2026-04-21"; got != want {
		t.Fatalf("unexpected date: got=%q want=%q", got, want)
	}
	if got, want := data.Meta.AnchorWeek, "2026-04-20"; got != want {
		t.Fatalf("unexpected fact-derived anchor week: got=%q want=%q", got, want)
	}

	environment := jobHistoryEnvironmentByName(t, data, "dev")
	if got, want := environment.Summary.TotalRuns, 1; got != want {
		t.Fatalf("unexpected total runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsWithRawFailures, 1; got != want {
		t.Fatalf("unexpected runs with raw failures: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsWithSemanticAttachment, 1; got != want {
		t.Fatalf("unexpected runs with semantic attachment: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsUnmatchedSignatures, 0; got != want {
		t.Fatalf("unexpected runs with unmatched signatures: got=%d want=%d", got, want)
	}

	run := jobHistoryRunByURL(t, environment, "https://prow.example.com/view/newer")
	if got, want := run.SemanticRollups.AttachmentSummary, "single_clustered"; got != want {
		t.Fatalf("unexpected attachment summary: got=%q want=%q", got, want)
	}
}

func TestBuildDayHandlesMultipleSignaturesOnOneRun(t *testing.T) {
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
	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	run := jobHistoryRunByURL(t, jobHistoryEnvironmentByName(t, data, "dev"), "https://prow.example.com/view/1")
	if got, want := run.SemanticRollups.AttachmentSummary, "multiple_clustered"; got != want {
		t.Fatalf("unexpected attachment summary: got=%q want=%q", got, want)
	}
	if got, want := run.SemanticRollups.SignatureCount, 2; got != want {
		t.Fatalf("unexpected signature count: got=%d want=%d", got, want)
	}
	if got, want := run.FailedTestCount, 2; got != want {
		t.Fatalf("unexpected failed test count: got=%d want=%d", got, want)
	}
	if got, want := len(run.Lanes), 1; got != want {
		t.Fatalf("unexpected lane count: got=%d want=%d", got, want)
	}
	if got, want := run.Lanes[0], "unknown"; got != want {
		t.Fatalf("unexpected lane value: got=%q want=%q", got, want)
	}
	if got, want := len(run.SemanticRollups.DistinctClusterIDs), 2; got != want {
		t.Fatalf("unexpected distinct cluster count: got=%d want=%d", got, want)
	}
	if got, want := run.SemanticRollups.ClusteredRows, 3; got != want {
		t.Fatalf("unexpected clustered row count: got=%d want=%d", got, want)
	}
}

func TestBuildDayUsesRowLevelReferencesWhenClustersShareSignature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
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
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
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

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	run := jobHistoryRunByURL(t, jobHistoryEnvironmentByName(t, data, "dev"), "https://prow.example.com/view/shared")
	if got, want := run.SemanticRollups.ClusteredRows, 2; got != want {
		t.Fatalf("unexpected clustered row count: got=%d want=%d", got, want)
	}
	if got, want := len(run.SemanticRollups.DistinctClusterIDs), 2; got != want {
		t.Fatalf("unexpected distinct cluster count: got=%d want=%d", got, want)
	}

	rowsByID := map[string]readmodelrunlog.JobHistoryFailureRow{}
	for _, row := range run.FailureRows {
		rowsByID[row.RowID] = row
	}

	finalizeRow := rowsByID["row-finalize"]
	if strings.TrimSpace(finalizeRow.SemanticAttachment.ClusterID) == "" {
		t.Fatalf("expected finalize cluster id to be populated")
	}
	if got, want := finalizeRow.SemanticAttachment.CanonicalEvidencePhrase, "failed post-install: resource not ready, name: finalize-mce-config"; got != want {
		t.Fatalf("unexpected finalize phrase: got=%q want=%q", got, want)
	}

	propagatorRow := rowsByID["row-propagator"]
	if strings.TrimSpace(propagatorRow.SemanticAttachment.ClusterID) == "" {
		t.Fatalf("expected propagator cluster id to be populated")
	}
	if got, want := propagatorRow.SemanticAttachment.CanonicalEvidencePhrase, "resource not ready, name: grc-policy-propagator"; got != want {
		t.Fatalf("unexpected propagator phrase: got=%q want=%q", got, want)
	}
	if finalizeRow.SemanticAttachment.ClusterID == propagatorRow.SemanticAttachment.ClusterID {
		t.Fatalf("expected distinct cluster ids for different row-level references")
	}
}

func TestBuildDayFlagsFailedRunsWithoutRawRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	runs := append(testsupport.SampleRunsFixture(), storecontracts.RunRecord{
		Environment: "dev",
		RunURL:      "https://prow.example.com/view/3",
		JobName:     "periodic-ci-missing-raw",
		Failed:      true,
		OccurredAt:  "2026-03-16T10:00:00Z",
	})
	if err := store.UpsertRuns(ctx, runs); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, testsupport.SampleRawFailuresFixture()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	environment := jobHistoryEnvironmentByName(t, data, "dev")
	if got, want := environment.Summary.FailedRunsWithoutRawRows, 1; got != want {
		t.Fatalf("unexpected failed runs without raw rows: got=%d want=%d", got, want)
	}
	run := jobHistoryRunByURL(t, environment, "https://prow.example.com/view/3")
	if got, want := run.SemanticRollups.AttachmentSummary, "failed_without_raw_rows"; got != want {
		t.Fatalf("unexpected attachment summary: got=%q want=%q", got, want)
	}
	if len(run.FailureRows) != 0 {
		t.Fatalf("expected no failure rows for raw-gap run, got=%d", len(run.FailureRows))
	}
}

func TestBuildDaySuppressesBadPRScoreWhenDayHasPostGoodEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	runs := testsupport.SampleRunsFixture()
	runs[0].PRNumber = 123
	runs[0].PRState = "open"
	if err := store.UpsertRuns(ctx, runs); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, testsupport.SampleRawFailuresFixture()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	run := jobHistoryRunByURL(t, jobHistoryEnvironmentByName(t, data, "dev"), "https://prow.example.com/view/1")
	if got := run.BadPRScore; got != 0 {
		t.Fatalf("expected same-day post-good evidence to suppress bad PR score, got=%d", got)
	}
}

func TestBuildDayUsesBackwardSupportWindowForBadPRScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/support-post-good",
			JobName:        "periodic-ci",
			PRNumber:       123,
			PRState:        "open",
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/day-target",
			JobName:        "periodic-ci",
			PRNumber:       123,
			PRState:        "open",
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-17T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-support-post-good",
			RunURL:         "https://prow.example.com/view/support-post-good",
			TestName:       "should install",
			TestSuite:      "rp-api-compat-all/parallel",
			SignatureID:    "sig-support-post-good",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
		{
			Environment:    "dev",
			RowID:          "row-day-target",
			RunURL:         "https://prow.example.com/view/day-target",
			TestName:       "should install",
			TestSuite:      "rp-api-compat-all/parallel",
			SignatureID:    "sig-day-target",
			OccurredAt:     "2026-03-17T08:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-17",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	run := jobHistoryRunByURL(t, jobHistoryEnvironmentByName(t, data, "dev"), "https://prow.example.com/view/day-target")
	if got := run.BadPRScore; got != 0 {
		t.Fatalf("expected backward support window to suppress bad PR score, got=%d", got)
	}
}

func TestBuildDayUsesDayScopedBadPRScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/day-target",
			JobName:        "periodic-ci",
			PRNumber:       123,
			PRState:        "open",
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-17T08:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/week-post-good",
			JobName:        "periodic-ci",
			PRNumber:       123,
			PRState:        "open",
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-18T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-day-target",
			RunURL:         "https://prow.example.com/view/day-target",
			TestName:       "should install",
			TestSuite:      "rp-api-compat-all/parallel",
			SignatureID:    "sig-day-target",
			OccurredAt:     "2026-03-17T08:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
		{
			Environment:    "dev",
			RowID:          "row-week-post-good",
			RunURL:         "https://prow.example.com/view/week-post-good",
			TestName:       "should install",
			TestSuite:      "rp-api-compat-all/parallel",
			SignatureID:    "sig-week-post-good",
			OccurredAt:     "2026-03-18T08:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelrunlog.BuildDay(ctx, fixture.Service, readmodelrunlog.RunLogDayQuery{
		Date:         "2026-03-17",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build job history day: %v", err)
	}

	run := jobHistoryRunByURL(t, jobHistoryEnvironmentByName(t, data, "dev"), "https://prow.example.com/view/day-target")
	if got, want := run.BadPRScore, 3; got != want {
		t.Fatalf("expected day-scoped bad PR score, got=%d want=%d", got, want)
	}
	if len(run.BadPRReasons) == 0 {
		t.Fatalf("expected day-scoped bad PR reasons")
	}
}

func jobHistoryEnvironmentByName(
	t *testing.T,
	data readmodelrunlog.RunLogDayData,
	environment string,
) readmodelrunlog.RunLogDayEnvironment {
	t.Helper()
	for _, row := range data.Environments {
		if strings.TrimSpace(row.Environment) == strings.TrimSpace(environment) {
			return row
		}
	}
	t.Fatalf("environment %q not found", environment)
	return readmodelrunlog.RunLogDayEnvironment{}
}

func jobHistoryRunByURL(
	t *testing.T,
	environment readmodelrunlog.RunLogDayEnvironment,
	runURL string,
) readmodelrunlog.JobHistoryRunRow {
	t.Helper()
	for _, row := range environment.Runs {
		if strings.TrimSpace(row.Run.RunURL) == strings.TrimSpace(runURL) {
			return row
		}
	}
	t.Fatalf("run %q not found", runURL)
	return readmodelrunlog.JobHistoryRunRow{}
}
