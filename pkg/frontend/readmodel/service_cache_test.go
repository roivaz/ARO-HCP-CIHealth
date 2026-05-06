package readmodel

import (
	"context"
	"fmt"
	"testing"
	"time"

	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
	postgresstore "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/initdb"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/migrations"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/testsupport/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPrepareFailurePatternWindowUsesFreshCacheSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newPreparedWindowCacheFixture(t)
	service := newPreparedWindowCacheTestService(t, fixture.Pool)
	cacheAnchor := time.Date(2026, time.May, 5, 9, 0, 0, 0, time.UTC)
	primaryOpts := service.preparedWindowCache.primaryPrepareOptions(cacheAnchor)

	store := fixture.OpenStore(t)
	seedPreparedWindowCacheFacts(t, ctx, store, []preparedWindowCacheFactSeed{
		{
			RunURL:      "https://prow.example.com/view/cache-hit-1",
			OccurredAt:  "2026-05-01T08:00:00Z",
			RowID:       "row-cache-hit-1",
			SignatureID: "sig-cache-hit-1",
		},
	})

	preparedWindow, err := service.prepareFailurePatternWindowDirect(ctx, primaryOpts)
	if err != nil {
		t.Fatalf("prepare initial cache snapshot: %v", err)
	}
	service.preparedWindowCache.store(primaryOpts, preparedWindow, time.Now().UTC())

	seedPreparedWindowCacheFacts(t, ctx, store, []preparedWindowCacheFactSeed{
		{
			RunURL:      "https://prow.example.com/view/cache-hit-2",
			OccurredAt:  "2026-05-02T08:00:00Z",
			RowID:       "row-cache-hit-2",
			SignatureID: "sig-cache-hit-2",
		},
	})

	cachedPreparedWindow, err := service.PrepareFailurePatternWindow(ctx, primaryOpts)
	if err != nil {
		t.Fatalf("prepare cached window: %v", err)
	}
	result, err := cachedPreparedWindow.ResultForWindow(primaryOpts.StartTime, primaryOpts.EndTime, false)
	if err != nil {
		t.Fatalf("result for cached window: %v", err)
	}
	if got, want := result.Diagnostics.RowsExtracted, 1; got != want {
		t.Fatalf("expected fresh cache snapshot to hide newly inserted row: got=%d want=%d", got, want)
	}
}

func TestPrepareFailurePatternWindowFallsBackWhenCacheSnapshotExpires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newPreparedWindowCacheFixture(t)
	service := newPreparedWindowCacheTestService(t, fixture.Pool)
	cacheAnchor := time.Date(2026, time.May, 5, 9, 0, 0, 0, time.UTC)
	primaryOpts := service.preparedWindowCache.primaryPrepareOptions(cacheAnchor)

	store := fixture.OpenStore(t)
	seedPreparedWindowCacheFacts(t, ctx, store, []preparedWindowCacheFactSeed{
		{
			RunURL:      "https://prow.example.com/view/cache-stale-1",
			OccurredAt:  "2026-05-01T08:00:00Z",
			RowID:       "row-cache-stale-1",
			SignatureID: "sig-cache-stale-1",
		},
	})

	preparedWindow, err := service.prepareFailurePatternWindowDirect(ctx, primaryOpts)
	if err != nil {
		t.Fatalf("prepare initial stale snapshot: %v", err)
	}
	service.preparedWindowCache.store(primaryOpts, preparedWindow, time.Now().UTC())

	seedPreparedWindowCacheFacts(t, ctx, store, []preparedWindowCacheFactSeed{
		{
			RunURL:      "https://prow.example.com/view/cache-stale-2",
			OccurredAt:  "2026-05-02T08:00:00Z",
			RowID:       "row-cache-stale-2",
			SignatureID: "sig-cache-stale-2",
		},
	})

	service.preparedWindowCache.mu.Lock()
	service.preparedWindowCache.snapshot.refreshedAt = time.Now().UTC().Add(-(service.preparedWindowCache.ttl + time.Second))
	service.preparedWindowCache.mu.Unlock()

	freshPreparedWindow, err := service.PrepareFailurePatternWindow(ctx, primaryOpts)
	if err != nil {
		t.Fatalf("prepare stale cache fallback window: %v", err)
	}
	result, err := freshPreparedWindow.ResultForWindow(primaryOpts.StartTime, primaryOpts.EndTime, false)
	if err != nil {
		t.Fatalf("result for stale cache fallback window: %v", err)
	}
	if got, want := result.Diagnostics.RowsExtracted, 2; got != want {
		t.Fatalf("expected stale cache fallback to reload new rows: got=%d want=%d", got, want)
	}
}

func TestRefreshPreparedWindowCacheStoresPrimarySnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newPreparedWindowCacheFixture(t)
	service := newPreparedWindowCacheTestService(t, fixture.Pool)
	cacheAnchor := time.Date(2026, time.May, 5, 9, 0, 0, 0, time.UTC)
	primaryOpts := service.preparedWindowCache.primaryPrepareOptions(cacheAnchor)

	store := fixture.OpenStore(t)
	seedPreparedWindowCacheFacts(t, ctx, store, []preparedWindowCacheFactSeed{
		{
			RunURL:      "https://prow.example.com/view/cache-refresh-1",
			OccurredAt:  "2026-05-01T08:00:00Z",
			RowID:       "row-cache-refresh-1",
			SignatureID: "sig-cache-refresh-1",
		},
	})

	service.refreshPreparedWindowCache(ctx, cacheAnchor)

	service.preparedWindowCache.mu.RLock()
	snapshot := service.preparedWindowCache.snapshot
	service.preparedWindowCache.mu.RUnlock()
	if snapshot == nil {
		t.Fatalf("expected refresh to populate the prepared window cache snapshot")
	}
	if got, want := snapshot.startTime, primaryOpts.StartTime; !got.Equal(want) {
		t.Fatalf("unexpected cached snapshot start time: got=%s want=%s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got, want := snapshot.endTime, primaryOpts.EndTime; !got.Equal(want) {
		t.Fatalf("unexpected cached snapshot end time: got=%s want=%s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	result, err := snapshot.preparedWindow.ResultForWindow(primaryOpts.StartTime, primaryOpts.EndTime, false)
	if err != nil {
		t.Fatalf("result for refreshed snapshot: %v", err)
	}
	if got, want := result.Diagnostics.RowsExtracted, 1; got != want {
		t.Fatalf("unexpected refreshed snapshot extracted rows: got=%d want=%d", got, want)
	}
}

func newPreparedWindowCacheTestService(t testing.TB, pool *pgxpool.Pool) *Service {
	t.Helper()

	service, err := New(Options{
		FailurePatternsEngine: FailurePatternsEngineInline,
		PostgresPool:          pool,
		PreparedWindowCache: PreparedWindowCacheOptions{
			Enabled:          true,
			EnvelopeDuration: DefaultPreparedWindowCacheEnvelopeDuration,
			RefreshInterval:  DefaultPreparedWindowCacheRefreshInterval,
			TTL:              DefaultPreparedWindowCacheTTL,
		},
	})
	if err != nil {
		t.Fatalf("create prepared window cache service: %v", err)
	}
	return service
}

type preparedWindowCacheFixture struct {
	Pool *pgxpool.Pool
}

func newPreparedWindowCacheFixture(t testing.TB) *preparedWindowCacheFixture {
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
	return &preparedWindowCacheFixture{Pool: pool}
}

func (f *preparedWindowCacheFixture) OpenStore(t testing.TB) storecontracts.Store {
	t.Helper()

	store, err := postgresstore.New(f.Pool, postgresstore.Options{})
	if err != nil {
		t.Fatalf("create postgres store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type preparedWindowCacheFactSeed struct {
	RunURL      string
	OccurredAt  string
	RowID       string
	SignatureID string
}

func seedPreparedWindowCacheFacts(
	t testing.TB,
	ctx context.Context,
	store storecontracts.Store,
	rows []preparedWindowCacheFactSeed,
) {
	t.Helper()
	if len(rows) == 0 {
		return
	}

	runRecords := make([]storecontracts.RunRecord, 0, len(rows))
	rawFailureRecords := make([]storecontracts.RawFailureRecord, 0, len(rows))
	for _, row := range rows {
		runRecords = append(runRecords, storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      row.RunURL,
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  row.OccurredAt,
		})
		rawFailureRecords = append(rawFailureRecords, storecontracts.RawFailureRecord{
			Environment:    "dev",
			RowID:          row.RowID,
			RunURL:         row.RunURL,
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    row.SignatureID,
			OccurredAt:     row.OccurredAt,
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		})
	}
	if err := store.UpsertRuns(ctx, runRecords); err != nil {
		t.Fatalf("seed cache run records: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, rawFailureRecords); err != nil {
		t.Fatalf("seed cache raw failure records: %v", err)
	}
}
