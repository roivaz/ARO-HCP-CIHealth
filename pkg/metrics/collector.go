package metrics

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

const (
	namespace = "cihealth"

	dbMetricRunCount              = "run_count"
	dbMetricFailureCount          = "failure_count"
	dbMetricFailedCIInfraRunCount = "failed_ci_infra_run_count"
	dbMetricFailedProvisionCount  = "failed_provision_run_count"
	dbMetricFailedE2ERunCount     = "failed_e2e_run_count"
)

var environmentLabel = []string{"environment"}

// snapshot holds pre-computed gauge values grouped by environment.
type snapshot struct {
	perEnv    map[string]envMetrics
	refreshAt time.Time
	ok        bool
}

type envMetrics struct {
	runs               float64
	failures           float64
	failuresProvision  float64
	failuresE2E        float64
	failuresCIInfra    float64
	successRate        float64
	lastSuccessSeconds float64
}

type dayCount struct {
	runs     float64
	failures float64
}

// MetricsStoreReader is the subset of contracts.Store needed by the collector.
type MetricsStoreReader interface {
	ListMetricsDailyForDates(ctx context.Context, environments []string, dates []string) ([]contracts.MetricDailyRecord, error)
	ListRunsByDateRange(ctx context.Context, environment string, startTime time.Time, endTime time.Time) ([]contracts.RunRecord, error)
}

// CollectorOptions configures the Collector.
type CollectorOptions struct {
	Logger            logr.Logger
	Store             MetricsStoreReader
	Environments      []string
	RollingWindowDays int
	RefreshInterval   time.Duration
}

// Collector implements prometheus.Collector by serving pre-cached gauge values
// that a background goroutine refreshes periodically from PostgreSQL.
type Collector struct {
	logger            logr.Logger
	store             MetricsStoreReader
	environments      []string
	rollingWindowDays int
	refreshInterval   time.Duration
	cache             atomic.Pointer[snapshot]

	runs              *prometheus.Desc
	failures          *prometheus.Desc
	failuresProvision *prometheus.Desc
	failuresE2E       *prometheus.Desc
	failuresCIInfra   *prometheus.Desc
	successRate       *prometheus.Desc
	lastSuccess       *prometheus.Desc
	cacheRefreshTime  *prometheus.Desc
	cacheRefreshOK    *prometheus.Desc
}

// NewCollector creates a Collector but does not start the background refresh loop.
// Call Start(ctx) to begin refreshing.
func NewCollector(opts CollectorOptions) (*Collector, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("metrics collector: store is required")
	}
	if len(opts.Environments) == 0 {
		return nil, fmt.Errorf("metrics collector: at least one environment is required")
	}
	if opts.RollingWindowDays < 0 {
		return nil, fmt.Errorf("metrics collector: rolling window days must not be negative")
	}
	if opts.RollingWindowDays == 0 {
		opts.RollingWindowDays = 7
	}
	if opts.RefreshInterval < 0 {
		return nil, fmt.Errorf("metrics collector: refresh interval must not be negative")
	}
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = 60 * time.Second
	}

	envs := make([]string, 0, len(opts.Environments))
	for _, e := range opts.Environments {
		if trimmed := strings.TrimSpace(e); trimmed != "" {
			envs = append(envs, trimmed)
		}
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("metrics collector: no valid environments after normalization")
	}

	c := &Collector{
		logger:            opts.Logger,
		store:             opts.Store,
		environments:      envs,
		rollingWindowDays: opts.RollingWindowDays,
		refreshInterval:   opts.RefreshInterval,

		runs: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "runs"),
			"Total CI runs in the rolling window.",
			environmentLabel, nil,
		),
		failures: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "failures"),
			"Failed CI runs in the rolling window.",
			environmentLabel, nil,
		),
		failuresProvision: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "failures", "provision"),
			"Provision failures in the rolling window.",
			environmentLabel, nil,
		),
		failuresE2E: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "failures", "e2e"),
			"E2E failures in the rolling window.",
			environmentLabel, nil,
		),
		failuresCIInfra: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "failures", "ci_infra"),
			"CI/infra failures in the rolling window.",
			environmentLabel, nil,
		),
		successRate: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "success_rate"),
			"Rolling window success rate (0.0 to 1.0).",
			environmentLabel, nil,
		),
		lastSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "last_success_timestamp_seconds"),
			"Unix timestamp of the most recent successful run.",
			environmentLabel, nil,
		),
		cacheRefreshTime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "metrics_cache", "last_refresh_timestamp_seconds"),
			"Unix timestamp of the last successful cache refresh.",
			nil, nil,
		),
		cacheRefreshOK: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "metrics_cache", "refresh_success"),
			"1 if the last cache refresh succeeded, 0 otherwise.",
			nil, nil,
		),
	}

	empty := &snapshot{perEnv: map[string]envMetrics{}}
	c.cache.Store(empty)

	return c, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.runs
	ch <- c.failures
	ch <- c.failuresProvision
	ch <- c.failuresE2E
	ch <- c.failuresCIInfra
	ch <- c.successRate
	ch <- c.lastSuccess
	ch <- c.cacheRefreshTime
	ch <- c.cacheRefreshOK
}

// Collect reads the latest snapshot from the atomic cache and emits per-environment
// gauges (runs, failures by lane, success rate, last success timestamp) plus
// cache health self-metrics. It never queries the database directly.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	snap := c.cache.Load()
	if snap == nil {
		return
	}

	for env, m := range snap.perEnv {
		ch <- prometheus.MustNewConstMetric(c.runs, prometheus.GaugeValue, m.runs, env)
		ch <- prometheus.MustNewConstMetric(c.failures, prometheus.GaugeValue, m.failures, env)
		ch <- prometheus.MustNewConstMetric(c.failuresProvision, prometheus.GaugeValue, m.failuresProvision, env)
		ch <- prometheus.MustNewConstMetric(c.failuresE2E, prometheus.GaugeValue, m.failuresE2E, env)
		ch <- prometheus.MustNewConstMetric(c.failuresCIInfra, prometheus.GaugeValue, m.failuresCIInfra, env)
		ch <- prometheus.MustNewConstMetric(c.successRate, prometheus.GaugeValue, m.successRate, env)
		ch <- prometheus.MustNewConstMetric(c.lastSuccess, prometheus.GaugeValue, m.lastSuccessSeconds, env)
	}

	refreshOK := float64(0)
	if snap.ok {
		refreshOK = 1
	}
	ch <- prometheus.MustNewConstMetric(c.cacheRefreshOK, prometheus.GaugeValue, refreshOK)
	if !snap.refreshAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.cacheRefreshTime, prometheus.GaugeValue, float64(snap.refreshAt.Unix()))
	}
}

// Start launches the background refresh loop. It blocks until ctx is cancelled.
func (c *Collector) Start(ctx context.Context) {
	c.logger.Info("Starting metrics collector background loop.",
		"environments", c.environments,
		"rolling_window_days", c.rollingWindowDays,
		"refresh_interval", c.refreshInterval,
	)

	c.refresh(ctx)

	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Stopping metrics collector background loop.")
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *Collector) refresh(ctx context.Context) {
	snap, err := c.buildSnapshot(ctx)
	if err != nil {
		c.logger.Error(err, "Failed to refresh metrics cache.")
		prev := c.cache.Load()
		failed := &snapshot{
			perEnv:    prev.perEnv,
			refreshAt: prev.refreshAt,
			ok:        false,
		}
		c.cache.Store(failed)
		return
	}
	c.cache.Store(snap)
	c.logger.V(1).Info("Refreshed metrics cache.", "environments", len(snap.perEnv))
}

func (c *Collector) buildSnapshot(ctx context.Context) (*snapshot, error) {
	now := time.Now().UTC()
	dates := rollingWindowDates(now, c.rollingWindowDays)
	if len(dates) == 0 {
		return &snapshot{perEnv: map[string]envMetrics{}, refreshAt: now, ok: true}, nil
	}

	// Step 1: Fetch all daily metric rows for the rolling window in a single query.
	rows, err := c.store.ListMetricsDailyForDates(ctx, c.environments, dates)
	if err != nil {
		return nil, fmt.Errorf("list metrics daily: %w", err)
	}

	envSet := make(map[string]struct{}, len(c.environments))
	for _, e := range c.environments {
		envSet[e] = struct{}{}
	}

	type envAccum struct {
		runs              float64
		failures          float64
		failuresProvision float64
		failuresE2E       float64
		failuresCIInfra   float64
	}

	accum := map[string]*envAccum{}
	dailyCounts := map[string]map[string]*dayCount{} // env -> date -> counts

	// Step 2: Accumulate totals per environment and per-day run/failure counts
	// (the latter used by findLastSuccessTimestamp to avoid re-querying).
	for _, row := range rows {
		env := strings.TrimSpace(row.Environment)
		if env == "" {
			continue
		}
		if _, ok := envSet[env]; !ok {
			continue
		}
		a, ok := accum[env]
		if !ok {
			a = &envAccum{}
			accum[env] = a
		}
		metric := strings.TrimSpace(row.Metric)
		switch metric {
		case dbMetricRunCount:
			a.runs += row.Value
		case dbMetricFailureCount:
			a.failures += row.Value
		case dbMetricFailedProvisionCount:
			a.failuresProvision += row.Value
		case dbMetricFailedE2ERunCount:
			a.failuresE2E += row.Value
		case dbMetricFailedCIInfraRunCount:
			a.failuresCIInfra += row.Value
		}

		// Track per-day run/failure counts so findLastSuccessTimestamp can
		// identify days with successes without re-querying the database.
		if metric == dbMetricRunCount || metric == dbMetricFailureCount {
			date := strings.TrimSpace(row.Date)
			if date == "" {
				continue
			}
			if dailyCounts[env] == nil {
				dailyCounts[env] = map[string]*dayCount{}
			}
			dayCounts := dailyCounts[env][date]
			if dayCounts == nil {
				dayCounts = &dayCount{}
				dailyCounts[env][date] = dayCounts
			}
			if metric == dbMetricRunCount {
				dayCounts.runs += row.Value
			} else {
				dayCounts.failures += row.Value
			}
		}
	}

	// Step 3: Compute derived metrics (success rate, last success timestamp) per environment.
	// Seed all configured environments so metrics are always emitted, even with zero rows.
	perEnv := make(map[string]envMetrics, len(c.environments))
	for _, env := range c.environments {
		perEnv[env] = envMetrics{}
	}
	for env, a := range accum {
		m := envMetrics{
			runs:              a.runs,
			failures:          a.failures,
			failuresProvision: a.failuresProvision,
			failuresE2E:       a.failuresE2E,
			failuresCIInfra:   a.failuresCIInfra,
		}
		if a.runs > 0 {
			m.successRate = 1 - (a.failures / a.runs)
			if m.successRate < 0 {
				m.successRate = 0
			}
		}

		lastSuccess, err := c.findLastSuccessTimestamp(ctx, env, now, dailyCounts[env])
		if err != nil {
			c.logger.Error(err, "Failed to find last success timestamp.", "environment", env)
		} else if lastSuccess > 0 {
			m.lastSuccessSeconds = lastSuccess
		}

		perEnv[env] = m
	}

	return &snapshot{perEnv: perEnv, refreshAt: now, ok: true}, nil
}

// findLastSuccessTimestamp scans backward through pre-computed daily counts to
// find the most recent day with at least one successful run, then queries
// individual runs to get the exact timestamp. Returns 0 if no success is found.
func (c *Collector) findLastSuccessTimestamp(ctx context.Context, env string, from time.Time, envDailyCounts map[string]*dayCount) (float64, error) {
	for daysBack := 0; daysBack < c.rollingWindowDays; daysBack++ {
		day := from.AddDate(0, 0, -daysBack)
		dayStr := day.Format("2006-01-02")

		dayCounts := envDailyCounts[dayStr]
		if dayCounts == nil || dayCounts.runs <= dayCounts.failures {
			continue
		}

		startTime, err := time.Parse("2006-01-02", dayStr)
		if err != nil {
			return 0, fmt.Errorf("parse date %q: %w", dayStr, err)
		}
		startTime = startTime.UTC()
		endTime := startTime.AddDate(0, 0, 1)

		runs, err := c.store.ListRunsByDateRange(ctx, env, startTime, endTime)
		if err != nil {
			return 0, fmt.Errorf("list runs for %s/%s: %w", env, dayStr, err)
		}

		var latest time.Time
		for _, run := range runs {
			if run.Failed {
				continue
			}
			occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(run.OccurredAt))
			if err != nil {
				c.logger.V(1).Info("Skipping run with malformed OccurredAt.",
					"environment", env, "run_url", run.RunURL, "occurred_at", run.OccurredAt, "error", err)
				continue
			}
			if occurredAt.After(latest) {
				latest = occurredAt
			}
		}
		if !latest.IsZero() {
			return float64(latest.Unix()), nil
		}
	}
	return 0, nil
}

// rollingWindowDates returns date strings (YYYY-MM-DD) for the last N days
// ending at now (inclusive of today), For example: ["2026-06-22", "2026-06-21", "2026-06-20"].
func rollingWindowDates(from time.Time, days int) []string {
	if days <= 0 {
		return nil
	}
	out := make([]string, 0, days)
	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, -i)
		out = append(out, day.Format("2006-01-02"))
	}
	return out
}
