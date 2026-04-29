package failurepatterns

import (
	"context"
	"strings"
	"time"

	semhistory "ci-failure-atlas/pkg/semantic/history"
	semanticquery "ci-failure-atlas/pkg/semantic/query"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

type StoredWeekData = semanticquery.WeekData

type LoadStoredWeekOptions struct {
	IncludeRawFailures     bool
	RawFailureWindowStart  time.Time
	RawFailureWindowEnd    time.Time
	RawFailureEnvironments []string
}

type PatternKey = semhistory.FailurePatternKey
type PatternPresence = semhistory.FailurePatternPresence
type PresenceResolver = semhistory.FailurePatternHistoryResolver

type BuildPresenceOptions struct {
	CurrentWeek          string
	CurrentSchemaVersion string
	LookbackWeeks        int
	ListWeeks            func(context.Context) ([]string, error)
	OpenStore            func(context.Context, string) (storecontracts.Store, error)
}

func InferStoredWeekSchemaVersion(ctx context.Context, store storecontracts.Store) (string, error) {
	return semanticquery.InferStoreWeekSchemaVersion(ctx, store)
}

func LoadStoredWeek(
	ctx context.Context,
	store storecontracts.Store,
	opts LoadStoredWeekOptions,
) (StoredWeekData, error) {
	return semanticquery.LoadWeekData(ctx, store, semanticquery.LoadWeekDataOptions{
		IncludeRawFailures:     opts.IncludeRawFailures,
		RawFailureWindowStart:  opts.RawFailureWindowStart,
		RawFailureWindowEnd:    opts.RawFailureWindowEnd,
		RawFailureEnvironments: append([]string(nil), opts.RawFailureEnvironments...),
	})
}

func ResolveTargetEnvironments(configured []string, data StoredWeekData) []string {
	return semanticquery.ResolveTargetEnvironments(configured, data)
}

func RawFailureTextByEnvironmentRow(rows []storecontracts.RawFailureRecord) map[string]string {
	return semanticquery.RawFailureTextByEnvironmentRow(rows)
}

func EnvironmentRowKey(environment string, rowID string) string {
	return semanticquery.EnvironmentRowKey(environment, rowID)
}

func EnvironmentRunRowKey(environment string, runURL string, rowID string) string {
	normalizedEnvironment := normalizeEnvironment(environment)
	trimmedRunURL := strings.TrimSpace(runURL)
	trimmedRowID := strings.TrimSpace(rowID)
	if normalizedEnvironment == "" || trimmedRunURL == "" || trimmedRowID == "" {
		return ""
	}
	return normalizedEnvironment + "|" + trimmedRunURL + "|" + trimmedRowID
}

func EnvironmentRunSignatureKey(environment string, runURL string, signatureID string) string {
	normalizedEnvironment := normalizeEnvironment(environment)
	trimmedRunURL := strings.TrimSpace(runURL)
	trimmedSignatureID := strings.TrimSpace(signatureID)
	if normalizedEnvironment == "" || trimmedRunURL == "" || trimmedSignatureID == "" {
		return ""
	}
	return normalizedEnvironment + "|" + trimmedRunURL + "|" + trimmedSignatureID
}

func ReferenceRowMatchKey(environment string, rowID string) string {
	rowKey := EnvironmentRowKey(environment, rowID)
	if rowKey == "" {
		return ""
	}
	return "row|" + rowKey
}

func ReferenceTupleMatchKey(environment string, runURL string, occurredAt string, signatureID string) string {
	normalizedEnvironment := normalizeEnvironment(environment)
	trimmedRunURL := strings.TrimSpace(runURL)
	trimmedOccurredAt := strings.TrimSpace(occurredAt)
	trimmedSignatureID := strings.TrimSpace(signatureID)
	if normalizedEnvironment == "" {
		return ""
	}
	if trimmedRunURL == "" && trimmedOccurredAt == "" && trimmedSignatureID == "" {
		return ""
	}
	return "ref|" + normalizedEnvironment + "|" + trimmedRunURL + "|" + trimmedOccurredAt + "|" + trimmedSignatureID
}

func BuildPresenceResolver(
	ctx context.Context,
	opts BuildPresenceOptions,
) (PresenceResolver, error) {
	return semhistory.BuildFailurePatternHistoryResolver(ctx, semhistory.BuildOptions{
		CurrentWeek:                        strings.TrimSpace(opts.CurrentWeek),
		CurrentSchemaVersion:               strings.TrimSpace(opts.CurrentSchemaVersion),
		FailurePatternHistoryLookbackWeeks: opts.LookbackWeeks,
		ListWeeks:                          opts.ListWeeks,
		OpenStore:                          opts.OpenStore,
	})
}

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
