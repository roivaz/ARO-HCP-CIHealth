package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	semanticcontracts "ci-failure-atlas/pkg/semantic/contracts"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

type LoadWeekDataOptions struct {
	IncludeRawFailures     bool
	RawFailureWindowStart  time.Time
	RawFailureWindowEnd    time.Time
	RawFailureEnvironments []string
}

type WeekData struct {
	WeekSchemaVersion         string
	SourceFailurePatterns     []semanticcontracts.FailurePatternRecord
	FailurePatterns           []semanticcontracts.FailurePatternRecord
	ReviewQueue               []semanticcontracts.ReviewItemRecord
	RawFailures               []storecontracts.RawFailureRecord
	TestClusterCountsByEnv    map[string]int
	ReviewQueueCountsByEnv    map[string]int
	FailurePatternCountsByEnv map[string]int
	OccurrenceTotalsByEnv     map[string]int
	AvailableEnvironments     []string
}

func InferStoreWeekSchemaVersion(ctx context.Context, store storecontracts.Store) (string, error) {
	if store == nil {
		return "", fmt.Errorf("store is required")
	}
	sourceFailurePatterns, err := store.ListFailurePatterns(ctx)
	if err != nil {
		return "", fmt.Errorf("list failure patterns: %w", err)
	}
	reviewQueue, err := store.ListReviewQueue(ctx)
	if err != nil {
		return "", fmt.Errorf("list review queue: %w", err)
	}
	weekSchemaVersion, err := semanticcontracts.InferWeekSchemaVersion(sourceFailurePatterns, reviewQueue)
	if err != nil {
		return "", fmt.Errorf("infer semantic schema version: %w", err)
	}
	return weekSchemaVersion, nil
}

func LoadWeekData(ctx context.Context, store storecontracts.Store, opts LoadWeekDataOptions) (WeekData, error) {
	if store == nil {
		return WeekData{}, fmt.Errorf("store is required")
	}

	sourceFailurePatterns, err := store.ListFailurePatterns(ctx)
	if err != nil {
		return WeekData{}, fmt.Errorf("list failure patterns: %w", err)
	}
	reviewQueue, err := store.ListReviewQueue(ctx)
	if err != nil {
		return WeekData{}, fmt.Errorf("list review queue: %w", err)
	}
	weekSchemaVersion, err := semanticcontracts.InferWeekSchemaVersion(sourceFailurePatterns, reviewQueue)
	if err != nil {
		return WeekData{}, fmt.Errorf("infer semantic schema version: %w", err)
	}
	if err := semanticcontracts.RequireCurrentSchemaVersion(weekSchemaVersion, "semantic week data load"); err != nil {
		return WeekData{}, err
	}
	summary, err := store.GetSemanticWeekSummary(ctx)
	if err != nil {
		return WeekData{}, fmt.Errorf("get semantic week summary: %w", err)
	}

	rawFailures := []storecontracts.RawFailureRecord(nil)
	if opts.IncludeRawFailures {
		if err := validateWeekRawFailureRange(opts); err != nil {
			return WeekData{}, err
		}
		rawFailures, err = loadWeekRawFailuresByRange(
			ctx,
			store,
			opts,
			summary.AvailableEnvironments,
		)
		if err != nil {
			return WeekData{}, err
		}
	}

	return WeekData{
		WeekSchemaVersion:         weekSchemaVersion,
		SourceFailurePatterns:     append([]semanticcontracts.FailurePatternRecord(nil), sourceFailurePatterns...),
		FailurePatterns:           append([]semanticcontracts.FailurePatternRecord(nil), sourceFailurePatterns...),
		ReviewQueue:               reviewQueue,
		RawFailures:               rawFailures,
		TestClusterCountsByEnv:    summary.TestClusterCountsByEnv,
		ReviewQueueCountsByEnv:    summary.ReviewQueueCountsByEnv,
		FailurePatternCountsByEnv: summary.FailurePatternCountsByEnv,
		OccurrenceTotalsByEnv:     summary.OccurrenceTotalsByEnv,
		AvailableEnvironments:     summary.AvailableEnvironments,
	}, nil
}

func validateWeekRawFailureRange(opts LoadWeekDataOptions) error {
	hasStart := !opts.RawFailureWindowStart.IsZero()
	hasEnd := !opts.RawFailureWindowEnd.IsZero()
	switch {
	case hasStart != hasEnd:
		return fmt.Errorf("raw failure time window requires both start and end timestamps")
	case !hasStart:
		return fmt.Errorf("raw failure time window is required when raw failures are included")
	case !opts.RawFailureWindowStart.Before(opts.RawFailureWindowEnd):
		return fmt.Errorf("raw failure time window requires start before end")
	default:
		return nil
	}
}

func loadWeekRawFailuresByRange(
	ctx context.Context,
	store storecontracts.Store,
	opts LoadWeekDataOptions,
	availableEnvironments []string,
) ([]storecontracts.RawFailureRecord, error) {
	targetEnvironments := normalizeEnvironments(opts.RawFailureEnvironments)
	if len(targetEnvironments) == 0 {
		targetEnvironments = normalizeEnvironments(availableEnvironments)
	}
	if len(targetEnvironments) == 0 {
		return nil, nil
	}

	out := make([]storecontracts.RawFailureRecord, 0)
	for _, environment := range targetEnvironments {
		rows, err := store.ListRawFailuresByDateRange(
			ctx,
			environment,
			opts.RawFailureWindowStart.UTC(),
			opts.RawFailureWindowEnd.UTC(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"list raw failures for env=%q in range %s..%s: %w",
				environment,
				opts.RawFailureWindowStart.UTC().Format(time.RFC3339),
				opts.RawFailureWindowEnd.UTC().Format(time.RFC3339),
				err,
			)
		}
		out = append(out, rows...)
	}
	return out, nil
}

func ResolveTargetEnvironments(configured []string, data WeekData) []string {
	normalizedConfigured := normalizeEnvironments(configured)
	if len(normalizedConfigured) > 0 {
		return normalizedConfigured
	}
	return append([]string(nil), data.AvailableEnvironments...)
}

func RawFailureTextByEnvironmentRow(rows []storecontracts.RawFailureRecord) map[string]string {
	byRowKey := map[string]string{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		rowID := strings.TrimSpace(row.RowID)
		rawText := strings.TrimSpace(row.RawText)
		if environment == "" || rowID == "" || rawText == "" {
			continue
		}
		rowKey := EnvironmentRowKey(environment, rowID)
		if rowKey == "" {
			continue
		}
		if _, exists := byRowKey[rowKey]; !exists {
			byRowKey[rowKey] = rawText
		}
	}
	return byRowKey
}

func EnvironmentRowKey(environment string, rowID string) string {
	normalizedEnvironment := normalizeEnvironment(environment)
	trimmedRowID := strings.TrimSpace(rowID)
	if normalizedEnvironment == "" || trimmedRowID == "" {
		return ""
	}
	return normalizedEnvironment + "|" + trimmedRowID
}

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEnvironments(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeEnvironment(value)
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
