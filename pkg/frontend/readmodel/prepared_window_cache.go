package readmodel

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	failurepatternwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/window"
	readmodelwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/window"
	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"

	"golang.org/x/sync/singleflight"
)

const (
	DefaultPreparedWindowCacheEnvelopeDuration = 35 * 24 * time.Hour
	DefaultPreparedWindowCacheRefreshInterval  = 10 * time.Minute
	DefaultPreparedWindowCacheTTL              = 12 * time.Minute
)

type PreparedWindowCacheOptions struct {
	Enabled          bool
	EnvelopeDuration time.Duration
	RefreshInterval  time.Duration
	TTL              time.Duration
}

type preparedWindowCacheManager struct {
	enabled             bool
	envelopeDuration    time.Duration
	refreshInterval     time.Duration
	ttl                 time.Duration
	primaryEnvironments []string

	refreshLoop  sync.Once
	prepareGroup singleflight.Group

	mu       sync.RWMutex
	snapshot *preparedWindowCacheSnapshot
}

type preparedWindowCacheSnapshot struct {
	preparedWindow failurepatternwindow.PreparedWindow
	environments   []string
	startTime      time.Time
	endTime        time.Time
	refreshedAt    time.Time
}

func newPreparedWindowCacheManager(opts PreparedWindowCacheOptions) (*preparedWindowCacheManager, error) {
	normalized, err := normalizePreparedWindowCacheOptions(opts)
	if err != nil {
		return nil, err
	}
	return &preparedWindowCacheManager{
		enabled:             normalized.Enabled,
		envelopeDuration:    normalized.EnvelopeDuration,
		refreshInterval:     normalized.RefreshInterval,
		ttl:                 normalized.TTL,
		primaryEnvironments: normalizePreparedWindowCacheEnvironmentSet(sourceoptions.SupportedEnvironments()),
	}, nil
}

func normalizePreparedWindowCacheOptions(opts PreparedWindowCacheOptions) (PreparedWindowCacheOptions, error) {
	if !opts.Enabled {
		return PreparedWindowCacheOptions{}, nil
	}
	if opts.EnvelopeDuration <= 0 {
		opts.EnvelopeDuration = DefaultPreparedWindowCacheEnvelopeDuration
	}
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = DefaultPreparedWindowCacheRefreshInterval
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultPreparedWindowCacheTTL
	}
	if opts.EnvelopeDuration <= 0 {
		return PreparedWindowCacheOptions{}, fmt.Errorf("prepared window cache envelope duration must be > 0")
	}
	if opts.RefreshInterval <= 0 {
		return PreparedWindowCacheOptions{}, fmt.Errorf("prepared window cache refresh interval must be > 0")
	}
	if opts.TTL <= 0 {
		return PreparedWindowCacheOptions{}, fmt.Errorf("prepared window cache ttl must be > 0")
	}
	return opts, nil
}

func (s *Service) StartPreparedWindowCache(ctx context.Context) {
	if s == nil || s.preparedWindowCache == nil || !s.preparedWindowCache.enabled || ctx == nil {
		return
	}
	s.preparedWindowCache.refreshLoop.Do(func() {
		go s.runPreparedWindowCacheLoop(ctx)
	})
}

func (s *Service) runPreparedWindowCacheLoop(ctx context.Context) {
	s.refreshPreparedWindowCache(ctx, time.Now().UTC())
	ticker := time.NewTicker(s.preparedWindowCache.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tickAt := <-ticker.C:
			s.refreshPreparedWindowCache(ctx, tickAt.UTC())
		}
	}
}

func (s *Service) PrepareFailurePatternWindow(
	ctx context.Context,
	opts failurepatternwindow.PrepareOptions,
) (failurepatternwindow.PreparedWindow, error) {
	if s == nil {
		return failurepatternwindow.PreparedWindow{}, fmt.Errorf("service is required")
	}
	normalizedOpts, err := normalizePreparedWindowPrepareOptions(opts)
	if err != nil {
		return failurepatternwindow.PreparedWindow{}, err
	}
	now := time.Now().UTC()
	if snapshot, reason, ok := s.preparedWindowCache.lookup(normalizedOpts, now); ok {
		logPreparedWindowCacheRequest("hit", reason, normalizedOpts, snapshot, false)
		return snapshot.preparedWindow, nil
	} else if s.preparedWindowCache != nil && s.preparedWindowCache.enabled {
		logPreparedWindowCacheRequest("miss", reason, normalizedOpts, nil, false)
	}

	preparedWindow, shared, err := s.prepareFailurePatternWindowSingleflight(ctx, normalizedOpts)
	if err != nil {
		return failurepatternwindow.PreparedWindow{}, err
	}
	logPreparedWindowCacheRequest("compute", "on_demand", normalizedOpts, nil, shared)
	s.maybeStorePrimaryPreparedWindow(normalizedOpts, preparedWindow, now)
	return preparedWindow, nil
}

func (s *Service) prepareFailurePatternWindowSingleflight(
	ctx context.Context,
	opts failurepatternwindow.PrepareOptions,
) (failurepatternwindow.PreparedWindow, bool, error) {
	result, err, shared := s.preparedWindowCache.prepareGroup.Do(preparedWindowCacheRequestKey(opts), func() (any, error) {
		return s.prepareFailurePatternWindowDirect(ctx, opts)
	})
	if err != nil {
		return failurepatternwindow.PreparedWindow{}, shared, err
	}
	preparedWindow, ok := result.(failurepatternwindow.PreparedWindow)
	if !ok {
		return failurepatternwindow.PreparedWindow{}, shared, fmt.Errorf("unexpected prepared window result type %T", result)
	}
	return preparedWindow, shared, nil
}

func (s *Service) prepareFailurePatternWindowDirect(
	ctx context.Context,
	opts failurepatternwindow.PrepareOptions,
) (failurepatternwindow.PreparedWindow, error) {
	store, err := s.OpenStore()
	if err != nil {
		return failurepatternwindow.PreparedWindow{}, err
	}
	defer func() {
		_ = store.Close()
	}()
	return failurepatternwindow.Prepare(ctx, store, opts)
}

func (s *Service) refreshPreparedWindowCache(ctx context.Context, now time.Time) {
	if s == nil || s.preparedWindowCache == nil || !s.preparedWindowCache.enabled {
		return
	}
	primaryOpts := s.preparedWindowCache.primaryPrepareOptions(now)
	if primaryOpts.StartTime.IsZero() || primaryOpts.EndTime.IsZero() {
		return
	}
	refreshCtx, cancel := context.WithTimeout(ctx, s.preparedWindowCache.refreshTimeout())
	defer cancel()

	startedAt := time.Now()
	preparedWindow, shared, err := s.prepareFailurePatternWindowSingleflight(refreshCtx, primaryOpts)
	if err != nil {
		log.Printf(
			"failure-pattern-cache refresh status=error envs=%s window=%s duration=%s err=%v",
			strings.Join(primaryOpts.Environments, ","),
			preparedWindowCacheWindowLabel(primaryOpts.StartTime, primaryOpts.EndTime),
			time.Since(startedAt),
			err,
		)
		return
	}
	s.preparedWindowCache.store(primaryOpts, preparedWindow, now)
	log.Printf(
		"failure-pattern-cache refresh status=success envs=%s window=%s refreshed_at=%s duration=%s shared=%t",
		strings.Join(primaryOpts.Environments, ","),
		preparedWindowCacheWindowLabel(primaryOpts.StartTime, primaryOpts.EndTime),
		now.UTC().Format(time.RFC3339),
		time.Since(startedAt),
		shared,
	)
}

func (s *Service) maybeStorePrimaryPreparedWindow(
	opts failurepatternwindow.PrepareOptions,
	preparedWindow failurepatternwindow.PreparedWindow,
	now time.Time,
) {
	if s == nil || s.preparedWindowCache == nil || !s.preparedWindowCache.enabled {
		return
	}
	primaryOpts := s.preparedWindowCache.primaryPrepareOptions(now)
	if !preparedWindowCacheOptionsEqual(opts, primaryOpts) {
		return
	}
	s.preparedWindowCache.store(primaryOpts, preparedWindow, now)
}

func (m *preparedWindowCacheManager) lookup(
	opts failurepatternwindow.PrepareOptions,
	now time.Time,
) (*preparedWindowCacheSnapshot, string, bool) {
	if m == nil || !m.enabled {
		return nil, "disabled", false
	}
	if !equalPreparedWindowCacheEnvironmentSets(opts.Environments, m.primaryEnvironments) {
		return nil, "env_mismatch", false
	}

	m.mu.RLock()
	snapshot := m.snapshot
	m.mu.RUnlock()
	if snapshot == nil {
		return nil, "cold", false
	}
	if snapshot.refreshedAt.IsZero() || now.UTC().Sub(snapshot.refreshedAt) > m.ttl {
		return snapshot, "stale", false
	}
	if opts.StartTime.Before(snapshot.startTime) || opts.EndTime.After(snapshot.endTime) {
		return snapshot, "outside_window", false
	}
	return snapshot, "fresh", true
}

func (m *preparedWindowCacheManager) store(
	opts failurepatternwindow.PrepareOptions,
	preparedWindow failurepatternwindow.PreparedWindow,
	refreshedAt time.Time,
) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = &preparedWindowCacheSnapshot{
		preparedWindow: preparedWindow,
		environments:   append([]string(nil), opts.Environments...),
		startTime:      opts.StartTime.UTC(),
		endTime:        opts.EndTime.UTC(),
		refreshedAt:    refreshedAt.UTC(),
	}
}

func (m *preparedWindowCacheManager) primaryPrepareOptions(now time.Time) failurepatternwindow.PrepareOptions {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cacheEnd := preparedWindowCachePrimaryEndTime(now.UTC())
	return failurepatternwindow.PrepareOptions{
		Environments: append([]string(nil), m.primaryEnvironments...),
		StartTime:    cacheEnd.Add(-m.envelopeDuration).UTC(),
		EndTime:      cacheEnd,
	}
}

func (m *preparedWindowCacheManager) refreshTimeout() time.Duration {
	if m == nil || m.refreshInterval <= 0 {
		return DefaultPreparedWindowCacheRefreshInterval
	}
	return m.refreshInterval
}

func preparedWindowCachePrimaryEndTime(now time.Time) time.Time {
	weekStart := readmodelwindow.WeekStartForDate(now.UTC())
	if weekStart.IsZero() {
		date := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
		return date.AddDate(0, 0, 1).UTC()
	}
	return weekStart.AddDate(0, 0, 7).UTC()
}

func normalizePreparedWindowPrepareOptions(
	opts failurepatternwindow.PrepareOptions,
) (failurepatternwindow.PrepareOptions, error) {
	startTime := opts.StartTime.UTC()
	endTime := opts.EndTime.UTC()
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return failurepatternwindow.PrepareOptions{}, fmt.Errorf("valid start and end times are required")
	}
	environments := normalizePreparedWindowCacheEnvironmentSet(opts.Environments)
	return failurepatternwindow.PrepareOptions{
		Environments: environments,
		StartTime:    startTime,
		EndTime:      endTime,
	}, nil
}

func normalizePreparedWindowCacheEnvironmentSet(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func equalPreparedWindowCacheEnvironmentSets(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if strings.TrimSpace(left[idx]) != strings.TrimSpace(right[idx]) {
			return false
		}
	}
	return true
}

func preparedWindowCacheOptionsEqual(
	left failurepatternwindow.PrepareOptions,
	right failurepatternwindow.PrepareOptions,
) bool {
	return left.StartTime.Equal(right.StartTime) &&
		left.EndTime.Equal(right.EndTime) &&
		equalPreparedWindowCacheEnvironmentSets(left.Environments, right.Environments)
}

func preparedWindowCacheRequestKey(opts failurepatternwindow.PrepareOptions) string {
	return strings.Join(opts.Environments, ",") + "|" + opts.StartTime.UTC().Format(time.RFC3339) + "|" + opts.EndTime.UTC().Format(time.RFC3339)
}

func preparedWindowCacheWindowLabel(startTime time.Time, endTime time.Time) string {
	return startTime.UTC().Format(time.RFC3339) + ".." + endTime.UTC().Format(time.RFC3339)
}

func logPreparedWindowCacheRequest(
	status string,
	reason string,
	opts failurepatternwindow.PrepareOptions,
	snapshot *preparedWindowCacheSnapshot,
	shared bool,
) {
	parts := []string{
		"failure-pattern-cache request",
		"status=" + strings.TrimSpace(status),
		"reason=" + strings.TrimSpace(reason),
		"envs=" + strings.Join(opts.Environments, ","),
		"window=" + preparedWindowCacheWindowLabel(opts.StartTime, opts.EndTime),
	}
	if snapshot != nil {
		parts = append(parts,
			"snapshot_window="+preparedWindowCacheWindowLabel(snapshot.startTime, snapshot.endTime),
			"snapshot_refreshed_at="+snapshot.refreshedAt.UTC().Format(time.RFC3339),
			fmt.Sprintf("snapshot_age=%s", time.Since(snapshot.refreshedAt)),
		)
	}
	if shared {
		parts = append(parts, "shared=true")
	}
	log.Print(strings.Join(parts, " "))
}
