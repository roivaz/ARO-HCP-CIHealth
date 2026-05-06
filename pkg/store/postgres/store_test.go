package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/initdb"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/migrations"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/testsupport/pgtest"
)

func TestNewRequiresPool(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Options{}); err == nil {
		t.Fatalf("expected error when creating postgres store with nil pool")
	}
}

func TestMethodsRequireContext(t *testing.T) {
	t.Parallel()

	store := &Store{}
	if err := store.UpsertRuns(nil, nil); err == nil {
		t.Fatalf("expected context validation error")
	}
}

func TestNormalizeWeekAcceptsMonday(t *testing.T) {
	t.Parallel()

	week, err := NormalizeWeek(" 2026-03-16 ")
	if err != nil {
		t.Fatalf("normalize week: %v", err)
	}
	if got, want := week, "2026-03-16"; got != want {
		t.Fatalf("unexpected normalized week: got=%q want=%q", got, want)
	}
}

func TestNormalizeWeekRejectsNonMonday(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeWeek("2026-03-15"); err == nil {
		t.Fatalf("expected validation error for non-Monday week")
	}
}

func TestMigrationsDropDeprecatedPhase3Tables(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()
	for _, table := range []string{
		"cfa_phase3_issues",
		"cfa_phase3_links",
		"cfa_phase3_events",
	} {
		var registered string
		err := store.pool.QueryRow(ctx, "SELECT COALESCE(to_regclass($1)::text, '')", "public."+table).Scan(&registered)
		if err != nil {
			t.Fatalf("check dropped table %q: %v", table, err)
		}
		if registered != "" {
			t.Fatalf("expected deprecated table %q to be absent after migrations, got %q", table, registered)
		}
	}
}

func TestListRunsByDateRangeUsesUTCDateProjection(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/utc-prev",
			OccurredAt:  "2026-03-15T23:59:59Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/offset-next",
			OccurredAt:  "2026-03-15T23:30:00-05:00",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/invalid",
			OccurredAt:  "not-a-timestamp",
		},
	}); err != nil {
		t.Fatalf("upsert runs: %v", err)
	}

	dates, err := store.ListRunDates(ctx)
	if err != nil {
		t.Fatalf("list run dates: %v", err)
	}
	if got, want := dates, []string{"2026-03-15", "2026-03-16"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected run dates: got=%v want=%v", got, want)
	}

	rows, err := store.ListRunsByDateRange(
		ctx,
		"dev",
		time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 17, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("list runs by date range: %v", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("unexpected runs by date count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].RunURL, "https://prow.example.com/run/offset-next"; got != want {
		t.Fatalf("unexpected run url: got=%q want=%q", got, want)
	}
}

func TestListRunsByDateRangeUsesTimestampWindow(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/before",
			OccurredAt:  "2026-03-16T03:59:59Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/offset-inside",
			OccurredAt:  "2026-03-15T23:30:00-05:00",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/morning",
			OccurredAt:  "2026-03-16T11:59:59Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/boundary",
			OccurredAt:  "2026-03-16T12:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/run/invalid",
			OccurredAt:  "not-a-timestamp",
		},
	}); err != nil {
		t.Fatalf("upsert runs: %v", err)
	}

	rows, err := store.ListRunsByDateRange(
		ctx,
		"dev",
		time.Date(2026, time.March, 16, 4, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("list runs by date range: %v", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("unexpected runs by date range count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].RunURL, "https://prow.example.com/run/offset-inside"; got != want {
		t.Fatalf("unexpected first run url: got=%q want=%q", got, want)
	}
	if got, want := rows[1].RunURL, "https://prow.example.com/run/morning"; got != want {
		t.Fatalf("unexpected second run url: got=%q want=%q", got, want)
	}
}

func TestListRawFailuresByDateRangeUsesUTCDateProjection(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment: "dev",
			RowID:       "row-1",
			RunURL:      "https://prow.example.com/run/1",
			SignatureID: "sig-1",
			OccurredAt:  "2026-03-15T23:30:00-05:00",
			RawText:     "offset failure",
		},
		{
			Environment: "dev",
			RowID:       "row-2",
			RunURL:      "https://prow.example.com/run/2",
			SignatureID: "sig-2",
			OccurredAt:  "2026-03-15T12:00:00Z",
			RawText:     "same day failure",
		},
		{
			Environment: "dev",
			RowID:       "row-3",
			RunURL:      "https://prow.example.com/run/3",
			SignatureID: "sig-3",
			OccurredAt:  "bad-timestamp",
			RawText:     "ignored failure",
		},
	}); err != nil {
		t.Fatalf("upsert raw failures: %v", err)
	}

	rows, err := store.ListRawFailuresByDateRange(
		ctx,
		"dev",
		time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 17, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("list raw failures by date range: %v", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("unexpected raw failures by date count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].RowID, "row-1"; got != want {
		t.Fatalf("unexpected row id: got=%q want=%q", got, want)
	}
}

func TestListRawFailuresByDateRangeUsesTimestampWindow(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment: "dev",
			RowID:       "row-before",
			RunURL:      "https://prow.example.com/run/before",
			SignatureID: "sig-before",
			OccurredAt:  "2026-03-16T06:59:59Z",
			RawText:     "before failure",
		},
		{
			Environment: "dev",
			RowID:       "row-1",
			RunURL:      "https://prow.example.com/run/1",
			SignatureID: "sig-1",
			OccurredAt:  "2026-03-15T23:30:00-08:00",
			RawText:     "offset failure",
		},
		{
			Environment: "dev",
			RowID:       "row-2",
			RunURL:      "https://prow.example.com/run/2",
			SignatureID: "sig-2",
			OccurredAt:  "2026-03-16T11:59:59Z",
			RawText:     "morning failure",
		},
		{
			Environment: "dev",
			RowID:       "row-boundary",
			RunURL:      "https://prow.example.com/run/boundary",
			SignatureID: "sig-boundary",
			OccurredAt:  "2026-03-16T12:00:00Z",
			RawText:     "boundary failure",
		},
		{
			Environment: "dev",
			RowID:       "row-invalid",
			RunURL:      "https://prow.example.com/run/invalid",
			SignatureID: "sig-invalid",
			OccurredAt:  "bad-timestamp",
			RawText:     "ignored failure",
		},
	}); err != nil {
		t.Fatalf("upsert raw failures: %v", err)
	}

	rows, err := store.ListRawFailuresByDateRange(
		ctx,
		"dev",
		time.Date(2026, time.March, 16, 7, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("list raw failures by date range: %v", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("unexpected raw failures by date range count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].RowID, "row-1"; got != want {
		t.Fatalf("unexpected first row id: got=%q want=%q", got, want)
	}
	if got, want := rows[1].RowID, "row-2"; got != want {
		t.Fatalf("unexpected second row id: got=%q want=%q", got, want)
	}
}

func TestMetricsQueriesProvideDateScopedQueries(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-15", Metric: "run_count", Value: 5},
		{Environment: "dev", Date: "2026-03-15", Metric: "failure_count", Value: 2},
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 7},
		{Environment: "int", Date: "2026-03-16", Metric: "run_count", Value: 3},
		{Environment: "int", Date: "2026-03-17", Metric: "run_count", Value: 11},
	}); err != nil {
		t.Fatalf("upsert metrics daily: %v", err)
	}

	dates, err := store.ListMetricDates(ctx)
	if err != nil {
		t.Fatalf("list metric dates: %v", err)
	}
	if got, want := dates, []string{"2026-03-15", "2026-03-16", "2026-03-17"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("unexpected metric dates: got=%v want=%v", got, want)
	}

	rows, err := store.ListMetricsDailyForDates(ctx, []string{"int", "dev"}, []string{"2026-03-16", "2026-03-15"})
	if err != nil {
		t.Fatalf("list metrics daily for dates: %v", err)
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("unexpected metrics row count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].Environment, "dev"; got != want {
		t.Fatalf("unexpected first row environment: got=%q want=%q", got, want)
	}
	if got, want := rows[0].Date, "2026-03-15"; got != want {
		t.Fatalf("unexpected first row date: got=%q want=%q", got, want)
	}
	if got, want := rows[0].Metric, "failure_count"; got != want {
		t.Fatalf("unexpected first row metric: got=%q want=%q", got, want)
	}

	sums, err := store.SumMetricByEnvironmentForDates(ctx, "run_count", []string{"int", "dev"}, []string{"2026-03-15", "2026-03-16"})
	if err != nil {
		t.Fatalf("sum metric by environment for dates: %v", err)
	}
	if got, want := sums["dev"], 12.0; got != want {
		t.Fatalf("unexpected dev run_count sum: got=%v want=%v", got, want)
	}
	if got, want := sums["int"], 3.0; got != want {
		t.Fatalf("unexpected int run_count sum: got=%v want=%v", got, want)
	}
}

func TestTestMetadataQueriesProvideBelowTargetQueries(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := context.Background()

	if err := store.UpsertTestMetadataDaily(ctx, []storecontracts.TestMetadataDailyRecord{
		{Environment: "dev", Date: "2026-03-15", Period: "weekly", TestSuite: "suite-a", TestName: "test-a", CurrentPassPercentage: 91.0, CurrentRuns: 10},
		{Environment: "dev", Date: "2026-03-16", Period: "weekly", TestSuite: "suite-a", TestName: "test-a", CurrentPassPercentage: 89.0, CurrentRuns: 12},
		{Environment: "dev", Date: "2026-03-16", Period: "weekly", TestSuite: "suite-b", TestName: "test-b", CurrentPassPercentage: 70.0, CurrentRuns: 8},
		{Environment: "dev", Date: "2026-03-16", Period: "weekly", TestSuite: "suite-c", TestName: "test-c", CurrentPassPercentage: 99.0, CurrentRuns: 20},
		{Environment: "dev", Date: "2026-03-17", Period: "weekly", TestSuite: "suite-d", TestName: "test-d", CurrentPassPercentage: 60.0, CurrentRuns: 2},
		{Environment: "dev", Date: "2026-03-17", Period: "daily", TestSuite: "suite-e", TestName: "test-e", CurrentPassPercentage: 10.0, CurrentRuns: 50},
		{Environment: "int", Date: "2026-03-16", Period: "weekly", TestSuite: "suite-z", TestName: "test-z", CurrentPassPercentage: 80.0, CurrentRuns: 15},
	}); err != nil {
		t.Fatalf("upsert test metadata daily: %v", err)
	}

	dates, err := store.ListTestMetadataDatesByEnvironment(ctx, "dev", "weekly")
	if err != nil {
		t.Fatalf("list test metadata dates by environment: %v", err)
	}
	if got, want := dates, []string{"2026-03-15", "2026-03-16", "2026-03-17"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("unexpected test metadata dates: got=%v want=%v", got, want)
	}

	rows, err := store.ListBelowTargetTestMetadataByDate(ctx, "dev", "2026-03-16", "weekly", 95.0, 5, 2)
	if err != nil {
		t.Fatalf("list below-target test metadata by date: %v", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("unexpected below-target row count: got=%d want=%d", got, want)
	}
	if got, want := rows[0].TestName, "test-b"; got != want {
		t.Fatalf("unexpected first below-target test: got=%q want=%q", got, want)
	}
	if got, want := rows[1].TestName, "test-a"; got != want {
		t.Fatalf("unexpected second below-target test: got=%q want=%q", got, want)
	}
}

func newIntegrationStore(t *testing.T) *Store {
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

	store, err := New(pool, Options{})
	if err != nil {
		t.Fatalf("create postgres store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
