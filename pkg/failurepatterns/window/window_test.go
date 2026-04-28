package window

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	failureextractor "ci-failure-atlas/pkg/failurepatterns/extractor"
	sourcelanes "ci-failure-atlas/pkg/source/lanes"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
	postgresstore "ci-failure-atlas/pkg/store/postgres"
	"ci-failure-atlas/pkg/store/postgres/initdb"
	"ci-failure-atlas/pkg/store/postgres/migrations"
	"ci-failure-atlas/pkg/testsupport/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

type windowIntegrationFixture struct {
	pool *pgxpool.Pool
}

func TestComputeMatchesStoredFailurePatternForSingleWeek(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/1",
			JobName:        "periodic-ci",
			PRNumber:       4101,
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	rawFailures := []storecontracts.RawFailureRecord{
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
			RowID:          "row-2",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:05:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}
	if err := store.UpsertRawFailures(ctx, rawFailures); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments: []string{"dev"},
		StartTime:    mustTime(t, "2026-03-16"),
		EndTime:      mustTime(t, "2026-03-23"),
	})
	if err != nil {
		t.Fatalf("compute failure patterns: %v", err)
	}

	if got, want := len(result.ExtractedRows), 2; got != want {
		t.Fatalf("unexpected extracted row count: got=%d want=%d", got, want)
	}
	if got, want := len(result.FailurePatterns), 1; got != want {
		t.Fatalf("unexpected failure pattern count: got=%d want=%d", got, want)
	}

	evidence := failureextractor.Extract(rawFailures[0].RawText)
	cluster := result.FailurePatterns[0]
	if got, want := cluster.Phase2ClusterID, fingerprint("dev|phase2|"+failureextractor.FailurePatternKey(evidence)); got != want {
		t.Fatalf("unexpected cluster id: got=%q want=%q", got, want)
	}
	if got, want := cluster.CanonicalEvidencePhrase, evidence.CanonicalEvidencePhrase; got != want {
		t.Fatalf("unexpected canonical phrase: got=%q want=%q", got, want)
	}
	if got, want := cluster.SearchQueryPhrase, evidence.SearchQueryPhrase; got != want {
		t.Fatalf("unexpected search phrase: got=%q want=%q", got, want)
	}
	if got, want := cluster.SupportCount, 2; got != want {
		t.Fatalf("unexpected support count: got=%d want=%d", got, want)
	}
	if got, want := cluster.PostGoodCommitCount, 2; got != want {
		t.Fatalf("unexpected post-good count: got=%d want=%d", got, want)
	}
	if got, want := cluster.ContributingTestsCount, 1; got != want {
		t.Fatalf("unexpected contributing test count: got=%d want=%d", got, want)
	}
	if got, want := len(cluster.References), 2; got != want {
		t.Fatalf("unexpected reference count: got=%d want=%d", got, want)
	}
	if got, want := cluster.ContributingTests[0].Lane, string(sourcelanes.ClassifyLane("dev", "suite-a", "should oauth")); got != want {
		t.Fatalf("unexpected contributing lane: got=%q want=%q", got, want)
	}
	if got, want := result.Diagnostics.RunsLoaded, 1; got != want {
		t.Fatalf("unexpected diagnostics run count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.RawFailuresLoaded, 2; got != want {
		t.Fatalf("unexpected diagnostics raw failure count: got=%d want=%d", got, want)
	}
}

func TestComputeAggregatesAcrossWeekBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-23")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/1",
			JobName:        "periodic-ci",
			PRNumber:       4101,
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/22",
			JobName:        "periodic-ci",
			PRNumber:       4102,
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-23T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
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
			RowID:          "row-2",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:05:00Z",
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
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments: []string{"dev"},
		StartTime:    mustTime(t, "2026-03-16"),
		EndTime:      mustTime(t, "2026-03-24"),
	})
	if err != nil {
		t.Fatalf("compute cross-week failure patterns: %v", err)
	}

	if got, want := len(result.FailurePatterns), 1; got != want {
		t.Fatalf("unexpected merged failure pattern count: got=%d want=%d", got, want)
	}
	cluster := result.FailurePatterns[0]
	if got, want := cluster.SupportCount, 3; got != want {
		t.Fatalf("unexpected cross-week support count: got=%d want=%d", got, want)
	}
	if got, want := len(cluster.References), 3; got != want {
		t.Fatalf("unexpected cross-week reference count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.RawFailuresLoaded, 3; got != want {
		t.Fatalf("unexpected cross-week diagnostics raw failure count: got=%d want=%d", got, want)
	}
}

func TestPreparedWindowResultForSubwindowMatchesDirectCompute(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-23")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/1",
			JobName:        "periodic-ci",
			PRNumber:       4101,
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/22",
			JobName:        "periodic-ci",
			PRNumber:       4102,
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-23T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
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
			RowID:          "row-2",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:05:00Z",
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
		{
			Environment:       "dev",
			RowID:             "row-nonartifact",
			RunURL:            "https://prow.example.com/view/22",
			NonArtifactBacked: true,
			TestName:          "nonartifact failure",
			TestSuite:         "suite-a",
			SignatureID:       "sig-nonartifact",
			OccurredAt:        "2026-03-23T08:10:00Z",
			RawText:           "run failed without junit rows",
			NormalizedText:    "run failed without junit rows",
		},
		{
			Environment:    "dev",
			RowID:          "row-missing-run",
			RunURL:         "https://prow.example.com/view/missing",
			TestName:       "missing run failure",
			TestSuite:      "suite-a",
			SignatureID:    "sig-missing",
			OccurredAt:     "2026-03-23T08:12:00Z",
			RawText:        "missing run metadata",
			NormalizedText: "missing run metadata",
		},
		{
			Environment:    "dev",
			RowID:          "row-invalid",
			RunURL:         "https://prow.example.com/view/22",
			TestName:       "invalid failure",
			TestSuite:      "suite-a",
			SignatureID:    "",
			OccurredAt:     "2026-03-23T08:15:00Z",
			RawText:        "invalid failure row missing signature",
			NormalizedText: "invalid failure row missing signature",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	prepared, err := Prepare(ctx, store, PrepareOptions{
		Environments: []string{"dev"},
		StartTime:    mustTime(t, "2026-03-16"),
		EndTime:      mustTime(t, "2026-03-24"),
	})
	if err != nil {
		t.Fatalf("prepare horizon window: %v", err)
	}

	subwindowResult, err := prepared.ResultForWindow(
		mustTime(t, "2026-03-23"),
		mustTime(t, "2026-03-24"),
		true,
	)
	if err != nil {
		t.Fatalf("compute prepared subwindow result: %v", err)
	}

	directSubwindowResult, err := Compute(ctx, store, ComputeOptions{
		Environments:  []string{"dev"},
		StartTime:     mustTime(t, "2026-03-23"),
		EndTime:       mustTime(t, "2026-03-24"),
		IncludeReview: true,
	})
	if err != nil {
		t.Fatalf("compute direct subwindow result: %v", err)
	}

	if !reflect.DeepEqual(
		windowResultWithoutStageTimings(subwindowResult),
		windowResultWithoutStageTimings(directSubwindowResult),
	) {
		t.Fatalf(
			"prepared subwindow result diverged from direct compute\nprepared=%#v\ndirect=%#v",
			windowResultWithoutStageTimings(subwindowResult),
			windowResultWithoutStageTimings(directSubwindowResult),
		)
	}

	fullWindowResult, err := prepared.ResultForWindow(
		mustTime(t, "2026-03-16"),
		mustTime(t, "2026-03-24"),
		true,
	)
	if err != nil {
		t.Fatalf("compute prepared full-window result: %v", err)
	}

	directFullWindowResult, err := Compute(ctx, store, ComputeOptions{
		Environments:  []string{"dev"},
		StartTime:     mustTime(t, "2026-03-16"),
		EndTime:       mustTime(t, "2026-03-24"),
		IncludeReview: true,
	})
	if err != nil {
		t.Fatalf("compute direct full-window result: %v", err)
	}

	if !reflect.DeepEqual(
		windowResultWithoutStageTimings(fullWindowResult),
		windowResultWithoutStageTimings(directFullWindowResult),
	) {
		t.Fatalf(
			"prepared full-window result diverged from direct compute\nprepared=%#v\ndirect=%#v",
			windowResultWithoutStageTimings(fullWindowResult),
			windowResultWithoutStageTimings(directFullWindowResult),
		)
	}
}

func TestComputeLoadsMultipleEnvironmentsDeterministically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment:    "int",
			RunURL:         "https://prow.example.com/view/int-1",
			JobName:        "periodic-ci-int",
			PRNumber:       5101,
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-16T09:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/dev-1",
			JobName:        "periodic-ci-dev",
			PRNumber:       4101,
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "int",
			RowID:          "row-int-1",
			RunURL:         "https://prow.example.com/view/int-1",
			TestName:       "should oauth int",
			TestSuite:      "suite-a",
			SignatureID:    "sig-int-1",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
		{
			Environment:    "dev",
			RowID:          "row-dev-1",
			RunURL:         "https://prow.example.com/view/dev-1",
			TestName:       "should oauth dev",
			TestSuite:      "suite-a",
			SignatureID:    "sig-dev-1",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments: []string{"int", "dev"},
		StartTime:    mustTime(t, "2026-03-16"),
		EndTime:      mustTime(t, "2026-03-17"),
	})
	if err != nil {
		t.Fatalf("compute multi-environment failure patterns: %v", err)
	}

	if got, want := len(result.FailurePatterns), 2; got != want {
		t.Fatalf("unexpected multi-environment failure pattern count: got=%d want=%d", got, want)
	}
	if got, want := result.FailurePatterns[0].Environment, "dev"; got != want {
		t.Fatalf("unexpected first failure pattern environment: got=%q want=%q", got, want)
	}
	if got, want := result.FailurePatterns[1].Environment, "int"; got != want {
		t.Fatalf("unexpected second failure pattern environment: got=%q want=%q", got, want)
	}
	if got, want := result.Diagnostics.RunsByEnvironment["dev"], 1; got != want {
		t.Fatalf("unexpected dev run count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.RunsByEnvironment["int"], 1; got != want {
		t.Fatalf("unexpected int run count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.RawFailuresByEnvironment["dev"], 1; got != want {
		t.Fatalf("unexpected dev raw failure count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.RawFailuresByEnvironment["int"], 1; got != want {
		t.Fatalf("unexpected int raw failure count: got=%d want=%d", got, want)
	}
}

func TestComputeWeakCanonicalsMergeAndEmitReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
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
			RowID:          "row-a",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should install",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "context deadline exceeded",
			NormalizedText: "context deadline exceeded",
		},
		{
			Environment:    "dev",
			RowID:          "row-b",
			RunURL:         "https://prow.example.com/view/2",
			TestName:       "should upgrade",
			TestSuite:      "suite-b",
			SignatureID:    "sig-b",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "context deadline exceeded",
			NormalizedText: "context deadline exceeded",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments:  []string{"dev"},
		StartTime:     mustTime(t, "2026-03-16"),
		EndTime:       mustTime(t, "2026-03-17"),
		IncludeReview: true,
	})
	if err != nil {
		t.Fatalf("compute weak-canonical window: %v", err)
	}

	if got, want := len(result.FailurePatterns), 1; got != want {
		t.Fatalf("unexpected weak-canonical failure pattern count: got=%d want=%d", got, want)
	}
	for _, cluster := range result.FailurePatterns {
		if got, want := cluster.SupportCount, 2; got != want {
			t.Fatalf("unexpected merged support count: got=%d want=%d", got, want)
		}
	}
	if got, want := len(result.ReviewItems), 1; got != want {
		t.Fatalf("unexpected review item count: got=%d want=%d", got, want)
	}
	for _, reviewItem := range result.ReviewItems {
		if got, want := reviewItem.Reason, "weak_canonical_needs_review"; got != want {
			t.Fatalf("unexpected review reason: got=%q want=%q", got, want)
		}
	}
	if got, want := result.Diagnostics.WeakCanonicalRows, 2; got != want {
		t.Fatalf("unexpected weak canonical count: got=%d want=%d", got, want)
	}
}

func TestComputeInterruptedByUserMergesWithoutWeakReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
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
			RowID:          "row-a",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should install",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "Interrupted by User",
			NormalizedText: "Interrupted by User",
		},
		{
			Environment:    "dev",
			RowID:          "row-b",
			RunURL:         "https://prow.example.com/view/2",
			TestName:       "should upgrade",
			TestSuite:      "suite-b",
			SignatureID:    "sig-b",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "Interrupted by User",
			NormalizedText: "Interrupted by User",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments:  []string{"dev"},
		StartTime:     mustTime(t, "2026-03-16"),
		EndTime:       mustTime(t, "2026-03-17"),
		IncludeReview: true,
	})
	if err != nil {
		t.Fatalf("compute interrupted-by-user window: %v", err)
	}

	if got, want := len(result.FailurePatterns), 1; got != want {
		t.Fatalf("unexpected interrupted-by-user failure pattern count: got=%d want=%d", got, want)
	}
	if got, want := result.FailurePatterns[0].SupportCount, 2; got != want {
		t.Fatalf("unexpected interrupted-by-user support count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.WeakCanonicalRows, 0; got != want {
		t.Fatalf("unexpected interrupted-by-user weak canonical count: got=%d want=%d", got, want)
	}
	if got := len(result.ReviewItems); got != 0 {
		t.Fatalf("did not expect weak-canonical review items for interrupted-by-user, got=%d", got)
	}
}

func TestComputeMergesStructuredCreateHCPTimeoutCanonicals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newWindowIntegrationFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
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
			RowID:          "row-a",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should install",
			TestSuite:      "suite-a",
			SignatureID:    "sig-a",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        `failed waiting for cluster="cluster-ver-4-19" in resourcegroup="rg-cluster-back-version-g5hsfc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterAndWait for cluster cluster-ver-4-19 in resource group rg-cluster-back-version-g5hsfc, error: context deadline exceeded`,
			NormalizedText: `failed waiting for cluster="cluster-ver-4-19" in resourcegroup="rg-cluster-back-version-g5hsfc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterAndWait for cluster cluster-ver-4-19 in resource group rg-cluster-back-version-g5hsfc, error: context deadline exceeded`,
		},
		{
			Environment:    "dev",
			RowID:          "row-b",
			RunURL:         "https://prow.example.com/view/2",
			TestName:       "should upgrade",
			TestSuite:      "suite-b",
			SignatureID:    "sig-b",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        `failed to create HCP cluster hcp-cluster, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: failed waiting for cluster="hcp-cluster" in resourcegroup="rg-abc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: context deadline exceeded`,
			NormalizedText: `failed to create HCP cluster hcp-cluster, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: failed waiting for cluster="hcp-cluster" in resourcegroup="rg-abc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: context deadline exceeded`,
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	result, err := Compute(ctx, store, ComputeOptions{
		Environments:  []string{"dev"},
		StartTime:     mustTime(t, "2026-03-16"),
		EndTime:       mustTime(t, "2026-03-17"),
		IncludeReview: true,
	})
	if err != nil {
		t.Fatalf("compute structured timeout window: %v", err)
	}

	if got, want := len(result.FailurePatterns), 1; got != want {
		t.Fatalf("unexpected structured-timeout failure pattern count: got=%d want=%d", got, want)
	}
	cluster := result.FailurePatterns[0]
	if got, want := cluster.CanonicalEvidencePhrase, "timeout during CreateHCPClusterAndWait; context deadline exceeded"; got != want {
		t.Fatalf("unexpected structured-timeout canonical: got=%q want=%q", got, want)
	}
	if got, want := cluster.SupportCount, 2; got != want {
		t.Fatalf("unexpected structured-timeout support count: got=%d want=%d", got, want)
	}
	if got, want := result.Diagnostics.WeakCanonicalRows, 0; got != want {
		t.Fatalf("unexpected structured-timeout weak canonical count: got=%d want=%d", got, want)
	}
	if got := len(result.ReviewItems); got != 0 {
		t.Fatalf("did not expect weak-canonical review items for structured timeout, got=%d", got)
	}
}

func newWindowIntegrationFixture(t *testing.T) *windowIntegrationFixture {
	t.Helper()

	server, err := pgtest.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Stop()
	})

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		server.User,
		server.Password,
		server.Host,
		server.Port,
		server.Database,
	)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := initdb.Initialize(context.Background(), pool); err != nil {
		t.Fatalf("initialize postgres schema: %v", err)
	}
	if err := migrations.Run(context.Background(), pool); err != nil {
		t.Fatalf("run postgres migrations: %v", err)
	}

	return &windowIntegrationFixture{pool: pool}
}

func (f *windowIntegrationFixture) openWeekStore(t *testing.T, week string) storecontracts.Store {
	t.Helper()

	store, err := postgresstore.New(f.pool, postgresstore.Options{Week: week})
	if err != nil {
		t.Fatalf("create postgres store for %s: %v", week, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse date %s: %v", value, err)
	}
	return parsed.UTC()
}

func windowResultWithoutStageTimings(result FailurePatternWindowResult) FailurePatternWindowResult {
	result.Diagnostics.StageTimings = FailurePatternWindowStageTimings{}
	return result
}
