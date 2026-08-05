package metrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

// fakeStore is a test double for MetricsStoreReader.
type fakeStore struct {
	dailyRows []contracts.MetricDailyRecord
	dailyErr  error
	runs      map[string][]contracts.RunRecord // key: "env/YYYY-MM-DD"
	runsErr   error
}

func (f *fakeStore) ListMetricsDailyForDates(_ context.Context, _ []string, _ []string) ([]contracts.MetricDailyRecord, error) {
	return f.dailyRows, f.dailyErr
}

func (f *fakeStore) ListRunsByDateRange(_ context.Context, env string, start time.Time, _ time.Time) ([]contracts.RunRecord, error) {
	if f.runsErr != nil {
		return nil, f.runsErr
	}
	key := env + "/" + start.Format("2006-01-02")
	return f.runs[key], nil
}

func validOpts(store MetricsStoreReader) CollectorOptions {
	return CollectorOptions{
		Logger:            logr.Discard(),
		Store:             store,
		Environments:      []string{"dev"},
		RollingWindowDays: 3,
		RefreshInterval:   time.Minute,
	}
}

// today and yesterday return date strings relative to now, so buildSnapshot
// tests don't break as calendar time advances.
func today() string     { return time.Now().UTC().Format("2006-01-02") }
func yesterday() string { return time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02") }

// todayAt returns an RFC3339 timestamp for today at the given hour (UTC).
func todayAt(hour int) string {
	now := time.Now().UTC()
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	return t.Format(time.RFC3339)
}

// parseTodayAt parses todayAt output back to time.Time for expected-value assertions.
func parseTodayAt(hour int) time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
}

// --- NewCollector validation ---

func TestNewCollector_NilStore(t *testing.T) {
	t.Parallel()
	opts := validOpts(nil)
	opts.Store = nil
	if _, err := NewCollector(opts); err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestNewCollector_NoEnvironments(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.Environments = nil
	if _, err := NewCollector(opts); err == nil {
		t.Fatal("expected error for empty environments")
	}
}

func TestNewCollector_WhitespaceOnlyEnvironments(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.Environments = []string{" ", ""}
	if _, err := NewCollector(opts); err == nil {
		t.Fatal("expected error for whitespace-only environments")
	}
}

func TestNewCollector_NegativeWindowDays(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.RollingWindowDays = -1
	if _, err := NewCollector(opts); err == nil {
		t.Fatal("expected error for negative rolling window days")
	}
}

func TestNewCollector_NegativeRefreshInterval(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.RefreshInterval = -1
	if _, err := NewCollector(opts); err == nil {
		t.Fatal("expected error for negative refresh interval")
	}
}

func TestNewCollector_ZeroDefaults(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.RollingWindowDays = 0
	opts.RefreshInterval = 0
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.rollingWindowDays != 7 {
		t.Fatalf("expected default rolling window 7, got %d", c.rollingWindowDays)
	}
	if c.refreshInterval != 60*time.Second {
		t.Fatalf("expected default refresh interval 60s, got %v", c.refreshInterval)
	}
}

func TestNewCollector_TrimsEnvironments(t *testing.T) {
	t.Parallel()
	opts := validOpts(&fakeStore{})
	opts.Environments = []string{" dev ", "", " int "}
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.environments) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(c.environments))
	}
	if c.environments[0] != "dev" || c.environments[1] != "int" {
		t.Fatalf("unexpected environments: %v", c.environments)
	}
}

// --- rollingWindowDates ---

func TestRollingWindowDates(t *testing.T) {
	t.Parallel()
	ref := time.Date(2026, 6, 22, 15, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		days int
		want []string
	}{
		{"zero", 0, nil},
		{"negative", -1, nil},
		{"one", 1, []string{"2026-06-22"}},
		{"three", 3, []string{"2026-06-22", "2026-06-21", "2026-06-20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rollingWindowDates(ref, tc.days)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- buildSnapshot ---

func TestBuildSnapshot_AggregatesMetrics(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		dailyRows: []contracts.MetricDailyRecord{
			{Environment: "dev", Date: today(), Metric: "run_count", Value: 10},
			{Environment: "dev", Date: today(), Metric: "failure_count", Value: 3},
			{Environment: "dev", Date: today(), Metric: "failed_provision_run_count", Value: 1},
			{Environment: "dev", Date: today(), Metric: "failed_e2e_run_count", Value: 1},
			{Environment: "dev", Date: today(), Metric: "failed_ci_infra_run_count", Value: 1},
			{Environment: "dev", Date: yesterday(), Metric: "run_count", Value: 5},
			{Environment: "dev", Date: yesterday(), Metric: "failure_count", Value: 0},
		},
		runs: map[string][]contracts.RunRecord{
			"dev/" + today(): {
				{Failed: false, OccurredAt: todayAt(14)},
				{Failed: true, OccurredAt: todayAt(15)},
			},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	snap, err := c.buildSnapshot(context.Background())
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if !snap.ok {
		t.Fatal("snapshot should be ok")
	}

	m, ok := snap.perEnv["dev"]
	if !ok {
		t.Fatal("expected dev environment in snapshot")
	}
	if m.runs != 15 {
		t.Fatalf("runs: got %v want 15", m.runs)
	}
	if m.failures != 3 {
		t.Fatalf("failures: got %v want 3", m.failures)
	}
	if m.failuresProvision != 1 {
		t.Fatalf("failuresProvision: got %v want 1", m.failuresProvision)
	}
	if m.failuresE2E != 1 {
		t.Fatalf("failuresE2E: got %v want 1", m.failuresE2E)
	}
	if m.failuresCIInfra != 1 {
		t.Fatalf("failuresCIInfra: got %v want 1", m.failuresCIInfra)
	}
	expectedRate := 1 - (3.0 / 15.0)
	if m.successRate < expectedRate-0.001 || m.successRate > expectedRate+0.001 {
		t.Fatalf("successRate: got %v want ~%v", m.successRate, expectedRate)
	}
	expectedLastSuccess := float64(parseTodayAt(14).Unix())
	if m.lastSuccessSeconds != expectedLastSuccess {
		t.Fatalf("lastSuccessSeconds: got %v want %v", m.lastSuccessSeconds, expectedLastSuccess)
	}
}

func TestBuildSnapshot_FiltersUnknownEnvironments(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		dailyRows: []contracts.MetricDailyRecord{
			{Environment: "dev", Date: today(), Metric: "run_count", Value: 10},
			{Environment: "unknown", Date: today(), Metric: "run_count", Value: 99},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	snap, err := c.buildSnapshot(context.Background())
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	if _, ok := snap.perEnv["unknown"]; ok {
		t.Fatal("unknown environment should not appear in snapshot")
	}
	m := snap.perEnv["dev"]
	if m.runs != 10 {
		t.Fatalf("dev runs: got %v want 10", m.runs)
	}
}

func TestBuildSnapshot_SeedsAllConfiguredEnvironments(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		dailyRows: []contracts.MetricDailyRecord{
			{Environment: "dev", Date: today(), Metric: "run_count", Value: 5},
		},
	}

	opts := validOpts(store)
	opts.Environments = []string{"dev", "int"}
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	snap, err := c.buildSnapshot(context.Background())
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	if _, ok := snap.perEnv["int"]; !ok {
		t.Fatal("int environment should be seeded even with no data")
	}
	m := snap.perEnv["int"]
	if m.runs != 0 || m.failures != 0 || m.successRate != 0 {
		t.Fatalf("seeded int should have zero values, got %+v", m)
	}
}

func TestBuildSnapshot_SuccessRateClampsToZero(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		dailyRows: []contracts.MetricDailyRecord{
			{Environment: "dev", Date: today(), Metric: "run_count", Value: 2},
			{Environment: "dev", Date: today(), Metric: "failure_count", Value: 5},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	snap, err := c.buildSnapshot(context.Background())
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	m := snap.perEnv["dev"]
	if m.successRate != 0 {
		t.Fatalf("successRate should be clamped to 0, got %v", m.successRate)
	}
}

func TestBuildSnapshot_DBError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{dailyErr: fmt.Errorf("connection refused")}
	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	if _, err := c.buildSnapshot(context.Background()); err == nil {
		t.Fatal("expected error from buildSnapshot when store fails")
	}
}

// --- findLastSuccessTimestamp ---

func TestFindLastSuccessTimestamp_FindsLatestSuccess(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		runs: map[string][]contracts.RunRecord{
			"dev/2026-06-22": {
				{Failed: true, OccurredAt: "2026-06-22T16:00:00Z"},
				{Failed: false, OccurredAt: "2026-06-22T14:00:00Z"},
				{Failed: false, OccurredAt: "2026-06-22T15:30:00Z"},
			},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	from := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	dailyCounts := map[string]*dayCount{
		"2026-06-22": {runs: 3, failures: 1},
	}

	ts, err := c.findLastSuccessTimestamp(context.Background(), "dev", from, dailyCounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 6, 22, 15, 30, 0, 0, time.UTC)
	if ts != float64(expected.Unix()) {
		t.Fatalf("timestamp: got %v want %v", ts, float64(expected.Unix()))
	}
}

func TestFindLastSuccessTimestamp_SkipsAllFailureDays(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		runs: map[string][]contracts.RunRecord{
			"dev/2026-06-20": {
				{Failed: false, OccurredAt: "2026-06-20T10:00:00Z"},
			},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	from := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	dailyCounts := map[string]*dayCount{
		"2026-06-22": {runs: 5, failures: 5},
		"2026-06-21": {runs: 3, failures: 3},
		"2026-06-20": {runs: 2, failures: 1},
	}

	ts, err := c.findLastSuccessTimestamp(context.Background(), "dev", from, dailyCounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	if ts != float64(expected.Unix()) {
		t.Fatalf("timestamp: got %v want %v (should skip to 2026-06-20)", ts, float64(expected.Unix()))
	}
}

func TestFindLastSuccessTimestamp_NoSuccessReturnsZero(t *testing.T) {
	t.Parallel()

	opts := validOpts(&fakeStore{})
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	from := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	dailyCounts := map[string]*dayCount{
		"2026-06-22": {runs: 2, failures: 2},
	}

	ts, err := c.findLastSuccessTimestamp(context.Background(), "dev", from, dailyCounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts != 0 {
		t.Fatalf("expected 0 when no success found, got %v", ts)
	}
}

func TestFindLastSuccessTimestamp_MalformedOccurredAtSkipped(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		runs: map[string][]contracts.RunRecord{
			"dev/2026-06-22": {
				{Failed: false, OccurredAt: "not-a-timestamp"},
				{Failed: false, OccurredAt: "2026-06-22T12:00:00Z"},
			},
		},
	}

	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	from := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	dailyCounts := map[string]*dayCount{
		"2026-06-22": {runs: 2, failures: 0},
	}

	ts, err := c.findLastSuccessTimestamp(context.Background(), "dev", from, dailyCounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	if ts != float64(expected.Unix()) {
		t.Fatalf("timestamp: got %v want %v", ts, float64(expected.Unix()))
	}
}

// --- refresh ---

func TestRefresh_PreservesTimestampOnError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{dailyErr: fmt.Errorf("db down")}
	opts := validOpts(store)
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	successTime := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	c.cache.Store(&snapshot{
		perEnv:    map[string]envMetrics{"dev": {runs: 5}},
		refreshAt: successTime,
		ok:        true,
	})

	c.refresh(context.Background())

	snap := c.cache.Load()
	if snap.ok {
		t.Fatal("snapshot should not be ok after failed refresh")
	}
	if !snap.refreshAt.Equal(successTime) {
		t.Fatalf("refreshAt should be preserved: got %v want %v", snap.refreshAt, successTime)
	}
	if snap.perEnv["dev"].runs != 5 {
		t.Fatalf("stale data should be preserved: got %v", snap.perEnv["dev"].runs)
	}
}

// --- Collect / Describe ---

func TestDescribe_EmitsAllDescriptors(t *testing.T) {
	t.Parallel()

	c, err := NewCollector(validOpts(&fakeStore{}))
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	ch := make(chan *prometheus.Desc, 20)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 9 {
		t.Fatalf("expected 9 descriptors, got %d", len(descs))
	}
}

func TestCollect_EmitsMetricsPerEnvironment(t *testing.T) {
	t.Parallel()

	opts := validOpts(&fakeStore{})
	opts.Environments = []string{"dev", "int"}
	c, err := NewCollector(opts)
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	now := time.Now().UTC()
	c.cache.Store(&snapshot{
		perEnv: map[string]envMetrics{
			"dev": {runs: 10, failures: 2, successRate: 0.8},
			"int": {runs: 5, failures: 1, successRate: 0.8},
		},
		refreshAt: now,
		ok:        true,
	})

	ch := make(chan prometheus.Metric, 50)
	c.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	// 7 per-env metrics * 2 environments + 2 cache health = 16
	if len(metrics) != 16 {
		t.Fatalf("expected 16 metrics, got %d", len(metrics))
	}
}

func TestCollect_CacheRefreshOKZeroOnFailure(t *testing.T) {
	t.Parallel()

	c, err := NewCollector(validOpts(&fakeStore{}))
	if err != nil {
		t.Fatalf("create collector: %v", err)
	}

	c.cache.Store(&snapshot{
		perEnv:    map[string]envMetrics{},
		refreshAt: time.Now().UTC(),
		ok:        false,
	})

	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	var found bool
	for m := range ch {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			continue
		}
		if m.Desc() == c.cacheRefreshOK && pb.GetGauge().GetValue() == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cache_refresh_success=0 when snapshot is not ok")
	}
}
