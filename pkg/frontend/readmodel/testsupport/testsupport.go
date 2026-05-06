package testsupport

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	frontreadmodel "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
	postgresstore "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/initdb"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/migrations"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/testsupport/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

type IntegrationFixture struct {
	Service *frontreadmodel.Service
	Pool    *pgxpool.Pool
}

func NewIntegrationFixture(t testing.TB, defaultWeek string) *IntegrationFixture {
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

	service, err := frontreadmodel.New(frontreadmodel.Options{
		DefaultWeek:           defaultWeek,
		FailurePatternsEngine: frontreadmodel.FailurePatternsEngineInline,
		PostgresPool:          pool,
	})
	if err != nil {
		t.Fatalf("create frontend service: %v", err)
	}
	return &IntegrationFixture{
		Service: service,
		Pool:    pool,
	}
}

func (f *IntegrationFixture) OpenWeekStore(t testing.TB, week string) storecontracts.Store {
	t.Helper()

	store, err := postgresstore.New(f.Pool, postgresstore.Options{})
	if err != nil {
		t.Fatalf("create postgres store for %s: %v", week, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type DeprecatedPhase3LinkRecord struct {
	IssueID     string `json:"issue_id"`
	Environment string `json:"environment"`
	RunURL      string `json:"run_url"`
	RowID       string `json:"row_id"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func (f *IntegrationFixture) SeedDeprecatedPhase3Links(t testing.TB, rows ...DeprecatedPhase3LinkRecord) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	ctx := context.Background()
	_, err := f.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS cfa_phase3_links (
  environment TEXT NOT NULL,
  run_url TEXT NOT NULL,
  row_id TEXT NOT NULL,
  issue_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL,
  PRIMARY KEY (environment, run_url, row_id)
)`)
	if err != nil {
		t.Fatalf("ensure deprecated phase3 link table: %v", err)
	}
	for _, row := range rows {
		payload, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal deprecated phase3 link payload: %v", err)
		}
		_, err = f.Pool.Exec(ctx, `
INSERT INTO cfa_phase3_links (environment, run_url, row_id, issue_id, updated_at, payload)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (environment, run_url, row_id)
DO UPDATE SET issue_id = EXCLUDED.issue_id, updated_at = EXCLUDED.updated_at, payload = EXCLUDED.payload
`, row.Environment, row.RunURL, row.RowID, row.IssueID, row.UpdatedAt, payload)
		if err != nil {
			t.Fatalf("insert deprecated phase3 link: %v", err)
		}
	}
}

func SampleRunsFixture() []storecontracts.RunRecord {
	return []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/1",
			JobName:        "periodic-ci",
			PostGoodCommit: true,
			Failed:         true,
			OccurredAt:     "2026-03-16T08:00:00Z",
		},
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/2",
			JobName:        "periodic-ci",
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-16T09:00:00Z",
		},
	}
}

func SampleRawFailuresFixture() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
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
			RowID:          "row-3",
			RunURL:         "https://prow.example.com/view/2",
			TestName:       "should install",
			TestSuite:      "suite-b",
			SignatureID:    "sig-b",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}
}

func PreviousSampleRunsFixture() []storecontracts.RunRecord {
	return []storecontracts.RunRecord{
		{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/prev-1",
			JobName:        "periodic-ci",
			PostGoodCommit: false,
			Failed:         true,
			OccurredAt:     "2026-03-09T08:00:00Z",
		},
	}
}

func PreviousSampleRawFailuresFixture() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "prev-row-1",
			RunURL:         "https://prow.example.com/view/prev-1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-old",
			OccurredAt:     "2026-03-09T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}
}

func ReportMetricsDaily() []storecontracts.MetricDailyRecord {
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

func ReportTestMetadataDaily() []storecontracts.TestMetadataDailyRecord {
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
