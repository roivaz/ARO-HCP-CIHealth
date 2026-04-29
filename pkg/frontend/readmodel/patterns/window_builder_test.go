package patterns_test

import (
	"context"
	"testing"

	readmodelmodel "ci-failure-atlas/pkg/frontend/readmodel/model"
	readmodelpatterns "ci-failure-atlas/pkg/frontend/readmodel/patterns"
	"ci-failure-atlas/pkg/frontend/readmodel/testsupport"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

func TestBuildWindowDataBuildsWindowRowsFromFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")

	if err := currentStore.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), testsupport.PreviousSampleRunsFixture()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), testsupport.PreviousSampleRawFailuresFixture()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 4},
	}); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure patterns: %v", err)
	}

	if got, want := data.Meta.AnchorWeek, "2026-03-16"; got != want {
		t.Fatalf("unexpected anchor week: got=%q want=%q", got, want)
	}
	if got, want := len(data.Environments), 1; got != want {
		t.Fatalf("unexpected environment count: got=%d want=%d", got, want)
	}

	environment := data.Environments[0]
	if got, want := environment.Environment, "dev"; got != want {
		t.Fatalf("unexpected environment: got=%q want=%q", got, want)
	}
	if got, want := environment.Summary.TotalRuns, 4; got != want {
		t.Fatalf("unexpected total runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.FailedRuns, 2; got != want {
		t.Fatalf("unexpected failed runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RawFailureCount, 3; got != want {
		t.Fatalf("unexpected raw failure count: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.MatchedFailureCount, 3; got != want {
		t.Fatalf("unexpected matched failure count: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.JobsAffected, 2; got != want {
		t.Fatalf("unexpected jobs affected summary: got=%d want=%d", got, want)
	}
	if got, want := len(environment.Rows), 2; got != want {
		t.Fatalf("unexpected row count: got=%d want=%d", got, want)
	}

	rowsByPhrase := map[string]readmodelpatterns.FailurePatternsRow{}
	for _, row := range environment.Rows {
		rowsByPhrase[row.CanonicalEvidencePhrase] = row
	}
	oauthRow, ok := rowsByPhrase["OAuth timeout while waiting for cluster <cluster>"]
	if !ok {
		t.Fatalf("missing oauth row: %+v", environment.Rows)
	}
	if got, want := oauthRow.WindowFailureCount, 2; got != want {
		t.Fatalf("unexpected oauth window failure count: got=%d want=%d", got, want)
	}
	if got, want := oauthRow.WeeklyPostGoodCount, 2; got != want {
		t.Fatalf("unexpected oauth post-good count: got=%d want=%d", got, want)
	}
	if got, want := oauthRow.PriorWeeksPresent, 1; got != want {
		t.Fatalf("unexpected oauth prior weeks present: got=%d want=%d", got, want)
	}
	installerRow, ok := rowsByPhrase["Installer failed to reach bootstrap machine"]
	if !ok {
		t.Fatalf("missing installer row: %+v", environment.Rows)
	}
	if got, want := installerRow.WindowFailureCount, 1; got != want {
		t.Fatalf("unexpected installer window failure count: got=%d want=%d", got, want)
	}
	if len(oauthRow.FullErrorSamples) == 0 || len(installerRow.FullErrorSamples) == 0 {
		t.Fatalf("expected full error samples for inline rows")
	}
}

func TestBuildWindowDataIgnoresDeprecatedPhase3Links(t *testing.T) {
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
	if err := store.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 4},
	}); err != nil {
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

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure patterns: %v", err)
	}

	environment := data.Environments[0]
	if got, want := len(environment.Rows), 3; got != want {
		t.Fatalf("unexpected row count after deprecated phase3 seeding: got=%d want=%d", got, want)
	}
	phrases := map[string]struct{}{}
	for _, row := range environment.Rows {
		phrases[row.CanonicalEvidencePhrase] = struct{}{}
	}
	if _, ok := phrases["OAuth timeout while waiting for cluster <cluster>"]; !ok {
		t.Fatalf("missing oauth phrase: %+v", environment.Rows)
	}
	if _, ok := phrases["Installer failed to reach bootstrap machine"]; !ok {
		t.Fatalf("missing installer phrase: %+v", environment.Rows)
	}
	if _, ok := phrases["API throttling while reconciling install state"]; !ok {
		t.Fatalf("missing throttling phrase: %+v", environment.Rows)
	}
	for _, row := range environment.Rows {
		if len(row.LinkedChildren) != 0 {
			t.Fatalf("expected deprecated phase3 links to produce no linked children, got=%d for %s", len(row.LinkedChildren), row.ClusterID)
		}
	}
}

func TestBuildWindowDataComposesCrossWeekWindows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")
	if err := currentStore.UpsertRuns(ctx, append(testsupport.SampleRunsFixture(), storecontracts.RunRecord{
		Environment: "dev",
		RunURL:      "https://prow.example.com/view/22",
		JobName:     "periodic-ci",
		Failed:      true,
		OccurredAt:  "2026-03-23T08:00:00Z",
	})); err != nil {
		t.Fatalf("seed cross-week runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, append(testsupport.SampleRawFailuresFixture(), storecontracts.RawFailureRecord{
		Environment:    "dev",
		RowID:          "row-22",
		RunURL:         "https://prow.example.com/view/22",
		TestName:       "should oauth",
		TestSuite:      "suite-a",
		SignatureID:    "sig-a",
		OccurredAt:     "2026-03-23T08:00:00Z",
		RawText:        "OAuth timeout while waiting for cluster operator",
		NormalizedText: "oauth timeout while waiting for cluster operator",
	})); err != nil {
		t.Fatalf("seed cross-week raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 4},
		{Environment: "dev", Date: "2026-03-23", Metric: "run_count", Value: 1},
	}); err != nil {
		t.Fatalf("seed cross-week metrics daily: %v", err)
	}

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-23",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("expected cross-week query to succeed: %v", err)
	}

	environment := data.Environments[0]
	if got, want := len(environment.Rows), 2; got != want {
		t.Fatalf("unexpected row count across cross-week window: got=%d want=%d", got, want)
	}
	rowsByPhrase := map[string]readmodelpatterns.FailurePatternsRow{}
	for _, row := range environment.Rows {
		rowsByPhrase[row.CanonicalEvidencePhrase] = row
	}
	oauthRow, ok := rowsByPhrase["OAuth timeout while waiting for cluster <cluster>"]
	if !ok {
		t.Fatalf("missing oauth row in cross-week window: %+v", environment.Rows)
	}
	if got, want := oauthRow.WindowFailureCount, 3; got != want {
		t.Fatalf("unexpected merged failure count: got=%d want=%d", got, want)
	}
	if got, want := oauthRow.JobsAffected, 2; got != want {
		t.Fatalf("unexpected merged jobs affected: got=%d want=%d", got, want)
	}
	if got, want := oauthRow.WeeklyPostGoodCount, 2; got != want {
		t.Fatalf("unexpected merged post-good count: got=%d want=%d", got, want)
	}
	if got, want := len(oauthRow.ScoringReferences), 3; got != want {
		t.Fatalf("unexpected merged scoring reference count: got=%d want=%d", got, want)
	}
}

func TestBuildWindowDataUsesCalendarAnchorWeekWithoutStoredSchemas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")
	if err := currentStore.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/22",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-23T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-22",
			RunURL:         "https://prow.example.com/view/22",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-23T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-23",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure patterns: %v", err)
	}
	if got, want := data.Meta.AnchorWeek, "2026-03-23"; got != want {
		t.Fatalf("unexpected calendar anchor week: got=%q want=%q", got, want)
	}
}

func TestBuildWindowDataBadPRScoreUsesWindowReferenceSpread(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	currentStore := fixture.OpenWeekStore(t, "2026-03-16")

	if err := currentStore.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			PRNumber:    4101,
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/22",
			JobName:     "periodic-ci",
			PRNumber:    4102,
			Failed:      true,
			OccurredAt:  "2026-03-23T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed cross-week runs: %v", err)
	}
	if err := currentStore.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-1",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
		{
			Environment:    "dev",
			RowID:          "row-22",
			RunURL:         "https://prow.example.com/view/22",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-23T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}); err != nil {
		t.Fatalf("seed cross-week raw failures: %v", err)
	}
	if err := currentStore.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 1},
		{Environment: "dev", Date: "2026-03-23", Metric: "run_count", Value: 1},
	}); err != nil {
		t.Fatalf("seed cross-week metrics daily: %v", err)
	}

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-23",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure patterns: %v", err)
	}

	row := data.Environments[0].Rows[0]
	score, reasons := readmodelmodel.BadPRScoreAndReasons(readmodelmodel.FailurePatternRow{
		Environment:        row.Environment,
		AfterLastPushCount: row.WeeklyPostGoodCount,
		AlsoIn:             row.SeenIn,
		AffectedRuns:       testRunReferences(row.References),
		ScoringReferences:  testRunReferences(row.ScoringReferences),
	})
	if got, want := row.WeeklyPostGoodCount, 0; got != want {
		t.Fatalf("unexpected window post-good count: got=%d want=%d", got, want)
	}
	if got, want := score, 0; got != want {
		t.Fatalf("unexpected bad PR score: got=%d want=%d reasons=%v", got, want, reasons)
	}
	if len(reasons) != 0 {
		t.Fatalf("expected cross-week reference spread to suppress bad PR reasons, got=%v", reasons)
	}
	for _, reason := range reasons {
		if reason == "only seen in one PR" {
			t.Fatalf("did not expect single-PR reason for multi-PR windowed row: %v", reasons)
		}
	}
}

func TestBuildWindowDataUsesRowLevelReferencesWhenClustersShareSignature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := testsupport.NewIntegrationFixture(t, "")
	store := fixture.OpenWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/finalize",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/propagator",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-finalize",
			RunURL:         "https://prow.example.com/view/finalize",
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
			RunURL:         "https://prow.example.com/view/propagator",
			TestName:       "propagator step",
			TestSuite:      "suite-a",
			SignatureID:    "sig-shared",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "resource not ready, name: grc-policy-propagator",
			NormalizedText: "grc-policy-propagator timeout",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := store.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 2},
	}); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	data, err := readmodelpatterns.BuildWindowData(ctx, fixture.Service, readmodelpatterns.FailurePatternsQuery{
		StartDate:    "2026-03-16",
		EndDate:      "2026-03-16",
		Environments: []string{"dev"},
	})
	if err != nil {
		t.Fatalf("build failure patterns: %v", err)
	}
	if got, want := len(data.Environments), 1; got != want {
		t.Fatalf("unexpected environment count: got=%d want=%d", got, want)
	}
	if got, want := len(data.Environments[0].Rows), 2; got != want {
		t.Fatalf("unexpected row count: got=%d want=%d", got, want)
	}

	rowsByPhrase := map[string]readmodelpatterns.FailurePatternsRow{}
	for _, row := range data.Environments[0].Rows {
		rowsByPhrase[row.CanonicalEvidencePhrase] = row
	}

	finalizeRow, ok := rowsByPhrase["failed post-install: resource not ready, name: finalize-mce-config"]
	if !ok {
		t.Fatalf("expected finalize row in response: %#v", data.Environments[0].Rows)
	}
	if got, want := finalizeRow.WindowFailureCount, 1; got != want {
		t.Fatalf("unexpected finalize window failure count: got=%d want=%d", got, want)
	}
	if got, want := len(finalizeRow.References), 1; got != want {
		t.Fatalf("unexpected finalize reference count: got=%d want=%d", got, want)
	}
	if got, want := finalizeRow.References[0].RunURL, "https://prow.example.com/view/finalize"; got != want {
		t.Fatalf("unexpected finalize run url: got=%q want=%q", got, want)
	}
	if got, want := len(finalizeRow.FullErrorSamples), 1; got != want {
		t.Fatalf("unexpected finalize sample count: got=%d want=%d", got, want)
	}
	if got, want := finalizeRow.FullErrorSamples[0], "failed post-install: resource not ready, name: finalize-mce-config"; got != want {
		t.Fatalf("unexpected finalize sample: got=%q want=%q", got, want)
	}

	propagatorRow, ok := rowsByPhrase["resource not ready, name: grc-policy-propagator"]
	if !ok {
		t.Fatalf("expected propagator row in response: %#v", data.Environments[0].Rows)
	}
	if got, want := propagatorRow.WindowFailureCount, 1; got != want {
		t.Fatalf("unexpected propagator window failure count: got=%d want=%d", got, want)
	}
	if got, want := len(propagatorRow.References), 1; got != want {
		t.Fatalf("unexpected propagator reference count: got=%d want=%d", got, want)
	}
	if got, want := propagatorRow.References[0].RunURL, "https://prow.example.com/view/propagator"; got != want {
		t.Fatalf("unexpected propagator run url: got=%q want=%q", got, want)
	}
	if got, want := len(propagatorRow.FullErrorSamples), 1; got != want {
		t.Fatalf("unexpected propagator sample count: got=%d want=%d", got, want)
	}
	if got, want := propagatorRow.FullErrorSamples[0], "resource not ready, name: grc-policy-propagator"; got != want {
		t.Fatalf("unexpected propagator sample: got=%q want=%q", got, want)
	}
}

func testRunReferences(rows []readmodelpatterns.FailurePatternReportReference) []readmodelmodel.RunReference {
	out := make([]readmodelmodel.RunReference, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelmodel.RunReference{
			RunURL:      row.RunURL,
			OccurredAt:  row.OccurredAt,
			SignatureID: row.SignatureID,
			PRNumber:    row.PRNumber,
		})
	}
	return out
}
