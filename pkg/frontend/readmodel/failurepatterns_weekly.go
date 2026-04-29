package readmodel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ci-failure-atlas/pkg/failurepatterns"
	semanticcontracts "ci-failure-atlas/pkg/semantic/contracts"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
	postgresstore "ci-failure-atlas/pkg/store/postgres"
)

const failurePatternReportFullErrorExamplesLimit = 3

type FailurePatternReportBuildOptions struct {
	Week                string
	Environments        []string
	HistoryHorizonWeeks int
	HistoryResolver     failurepatterns.PresenceResolver
}

type FailurePatternReportReference struct {
	RowID          string `json:"-"`
	RunURL         string `json:"run_url"`
	OccurredAt     string `json:"occurred_at"`
	SignatureID    string `json:"signature_id"`
	PRNumber       int    `json:"pr_number"`
	PostGoodCommit bool   `json:"after_last_push_of_merged_pr"`
}

type FailurePatternReportContributingTest struct {
	Lane         string `json:"failed_at"`
	JobName      string `json:"job_name"`
	TestName     string `json:"test_name"`
	SupportCount int    `json:"occurrences"`
}

type FailurePatternReportCluster struct {
	Environment             string                                 `json:"environment"`
	SchemaVersion           string                                 `json:"schema_version"`
	Phase2ClusterID         string                                 `json:"failure_pattern_id"`
	CanonicalEvidencePhrase string                                 `json:"failure_pattern"`
	SearchQueryPhrase       string                                 `json:"search_query"`
	SupportCount            int                                    `json:"occurrences"`
	SeenPostGoodCommit      bool                                   `json:"after_last_push_seen"`
	PostGoodCommitCount     int                                    `json:"after_last_push_count"`
	ContributingTestsCount  int                                    `json:"contributing_test_count"`
	ContributingTests       []FailurePatternReportContributingTest `json:"contributing_tests"`
	MemberPhase1ClusterIDs  []string                               `json:"member_phase1_cluster_ids"`
	MemberSignatureIDs      []string                               `json:"member_signature_ids"`
	References              []FailurePatternReportReference        `json:"affected_runs"`
	FullErrorSamples        []string                               `json:"full_error_samples,omitempty"`
	LinkedChildren          []FailurePatternReportCluster          `json:"linked_failure_patterns,omitempty"`
}

type FailurePatternReportData struct {
	WeekSchemaVersion              string
	FailurePatternClusters         []FailurePatternReportCluster
	TargetEnvironments             []string
	OverallJobsByEnvironment       map[string]int
	WindowStartRaw                 string
	WindowEndRaw                   string
	HistoryResolver                failurepatterns.PresenceResolver
	GeneratedAt                    time.Time
	TestClusterCountsByEnvironment map[string]int
	ReviewItemCountsByEnvironment  map[string]int
}

func BuildFailurePatternReportData(ctx context.Context, store storecontracts.Store, opts FailurePatternReportBuildOptions) (FailurePatternReportData, error) {
	if store == nil {
		return FailurePatternReportData{}, fmt.Errorf("store is required")
	}

	metricWindowStart, metricWindowEnd := failurePatternReportMetricWindowBounds(opts.Week)
	windowStartRaw, windowEndRaw := failurePatternReportMetricWindowStrings(metricWindowStart, metricWindowEnd)
	weekData, err := failurepatterns.LoadStoredWeek(ctx, store, failurepatterns.LoadStoredWeekOptions{
		IncludeRawFailures:     true,
		RawFailureWindowStart:  metricWindowStart,
		RawFailureWindowEnd:    metricWindowEnd,
		RawFailureEnvironments: append([]string(nil), opts.Environments...),
	})
	if err != nil {
		return FailurePatternReportData{}, err
	}

	reportRows := toFailurePatternReportClusters(weekData.FailurePatterns)
	rawFailuresByRun := failurePatternReportIndexRawFailuresByEnvironmentRun(weekData.RawFailures)

	targetEnvironments := failurepatterns.ResolveTargetEnvironments(opts.Environments, weekData)
	overallJobsByEnvironment, err := failurePatternReportMetricRunTotalsByEnvironment(
		ctx,
		store,
		targetEnvironments,
		metricWindowStart,
		metricWindowEnd,
	)
	if err != nil {
		return FailurePatternReportData{}, fmt.Errorf("load overall metric run counts: %w", err)
	}

	historyResolver := opts.HistoryResolver
	if historyResolver == nil {
		lookbackWeeks := opts.HistoryHorizonWeeks
		if lookbackWeeks <= 0 {
			lookbackWeeks = DefaultHistoryWeeks
		}
		historyResolver, err = failurepatterns.BuildPresenceResolver(ctx, failurepatterns.BuildPresenceOptions{
			CurrentWeek:          strings.TrimSpace(opts.Week),
			CurrentSchemaVersion: weekData.WeekSchemaVersion,
			LookbackWeeks:        lookbackWeeks,
		})
		if err != nil {
			return FailurePatternReportData{}, fmt.Errorf("build failure-pattern history resolver: %w", err)
		}
	}

	failurePatternRows := failurePatternReportAttachFullErrorSamples(reportRows, failurePatternReportFullErrorExamplesLimit, rawFailuresByRun)

	return FailurePatternReportData{
		WeekSchemaVersion:              weekData.WeekSchemaVersion,
		FailurePatternClusters:         failurePatternRows,
		TargetEnvironments:             append([]string(nil), targetEnvironments...),
		OverallJobsByEnvironment:       cloneIntMap(overallJobsByEnvironment),
		WindowStartRaw:                 windowStartRaw,
		WindowEndRaw:                   windowEndRaw,
		HistoryResolver:                historyResolver,
		GeneratedAt:                    time.Now().UTC(),
		TestClusterCountsByEnvironment: cloneIntMap(weekData.TestClusterCountsByEnv),
		ReviewItemCountsByEnvironment:  cloneIntMap(weekData.ReviewQueueCountsByEnv),
	}, nil
}

func cloneIntMap(source map[string]int) map[string]int {
	if len(source) == 0 {
		return map[string]int{}
	}
	out := make(map[string]int, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func failurePatternReportMetricWindowBounds(week string) (time.Time, time.Time) {
	normalizedWeek, err := postgresstore.NormalizeWeek(week)
	if err != nil || normalizedWeek == "" {
		return time.Time{}, time.Time{}
	}
	start, err := time.Parse("2006-01-02", normalizedWeek)
	if err != nil {
		return time.Time{}, time.Time{}
	}
	start = start.UTC()
	return start, start.AddDate(0, 0, 7)
}

func failurePatternReportMetricWindowStrings(start time.Time, end time.Time) (string, string) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return "", ""
	}
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func failurePatternReportMetricRunTotalsByEnvironment(
	ctx context.Context,
	store storecontracts.Store,
	environments []string,
	windowStart time.Time,
	windowEnd time.Time,
) (map[string]int, error) {
	totals := map[string]int{}
	normalizedEnvironments := normalizeStringSlice(environments)
	if len(normalizedEnvironments) == 0 {
		return totals, nil
	}
	metricDates := metricDateLabelsFromWindow(windowStart, windowEnd)
	if len(metricDates) == 0 {
		return totals, nil
	}
	return sumMetricByEnvironmentForDates(ctx, store, "run_count", normalizedEnvironments, metricDates)
}

func toFailurePatternReportClusters(rows []semanticcontracts.FailurePatternRecord) []FailurePatternReportCluster {
	out := make([]FailurePatternReportCluster, 0, len(rows))
	for _, row := range rows {
		out = append(out, FailurePatternReportCluster{
			Environment:             normalizeEnvironment(row.Environment),
			SchemaVersion:           strings.TrimSpace(row.SchemaVersion),
			Phase2ClusterID:         strings.TrimSpace(row.Phase2ClusterID),
			CanonicalEvidencePhrase: strings.TrimSpace(row.CanonicalEvidencePhrase),
			SearchQueryPhrase:       strings.TrimSpace(row.SearchQueryPhrase),
			SupportCount:            row.SupportCount,
			SeenPostGoodCommit:      row.SeenPostGoodCommit,
			PostGoodCommitCount:     row.PostGoodCommitCount,
			ContributingTestsCount:  row.ContributingTestsCount,
			ContributingTests:       toFailurePatternReportContributingTests(row.ContributingTests),
			MemberPhase1ClusterIDs:  append([]string(nil), row.MemberPhase1ClusterIDs...),
			MemberSignatureIDs:      append([]string(nil), row.MemberSignatureIDs...),
			References:              toFailurePatternReportReferences(row.References),
		})
	}
	return out
}

func toFailurePatternReportContributingTests(rows []semanticcontracts.ContributingTestRecord) []FailurePatternReportContributingTest {
	out := make([]FailurePatternReportContributingTest, 0, len(rows))
	for _, row := range rows {
		out = append(out, FailurePatternReportContributingTest{
			Lane:         strings.TrimSpace(row.Lane),
			JobName:      strings.TrimSpace(row.JobName),
			TestName:     strings.TrimSpace(row.TestName),
			SupportCount: row.SupportCount,
		})
	}
	return out
}

func toFailurePatternReportReferences(rows []semanticcontracts.ReferenceRecord) []FailurePatternReportReference {
	out := make([]FailurePatternReportReference, 0, len(rows))
	for _, row := range rows {
		out = append(out, FailurePatternReportReference{
			RowID:          strings.TrimSpace(row.RowID),
			RunURL:         strings.TrimSpace(row.RunURL),
			OccurredAt:     strings.TrimSpace(row.OccurredAt),
			SignatureID:    strings.TrimSpace(row.SignatureID),
			PRNumber:       row.PRNumber,
			PostGoodCommit: row.PostGoodCommit,
		})
	}
	return out
}

func failurePatternReportIndexRawFailuresByEnvironmentRun(rows []storecontracts.RawFailureRecord) map[string][]storecontracts.RawFailureRecord {
	byRun := map[string][]storecontracts.RawFailureRecord{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		runURL := strings.TrimSpace(row.RunURL)
		if environment == "" || runURL == "" {
			continue
		}
		key := environment + "|" + runURL
		byRun[key] = append(byRun[key], row)
	}
	for key := range byRun {
		runRows := byRun[key]
		sort.Slice(runRows, func(i, j int) bool {
			if runRows[i].OccurredAt != runRows[j].OccurredAt {
				return runRows[i].OccurredAt < runRows[j].OccurredAt
			}
			if runRows[i].RowID != runRows[j].RowID {
				return runRows[i].RowID < runRows[j].RowID
			}
			return runRows[i].SignatureID < runRows[j].SignatureID
		})
		byRun[key] = runRows
	}
	return byRun
}

func failurePatternReportAttachFullErrorSamples(
	clusters []FailurePatternReportCluster,
	limit int,
	runFailuresByRun map[string][]storecontracts.RawFailureRecord,
) []FailurePatternReportCluster {
	if len(clusters) == 0 || limit <= 0 {
		return append([]FailurePatternReportCluster(nil), clusters...)
	}
	out := append([]FailurePatternReportCluster(nil), clusters...)
	for index := range out {
		cluster := out[index]
		referencesByKey := failurePatternsReferenceMatchMap(cluster.References)
		if len(referencesByKey) == 0 {
			continue
		}

		samples := make([]string, 0, limit)
		orderedRefs := append([]FailurePatternReportReference(nil), cluster.References...)
		sort.Slice(orderedRefs, func(i, j int) bool {
			ti, okI := ParseReferenceTimestamp(orderedRefs[i].OccurredAt)
			tj, okJ := ParseReferenceTimestamp(orderedRefs[j].OccurredAt)
			switch {
			case okI && okJ && !ti.Equal(tj):
				return ti.After(tj)
			case okI != okJ:
				return okI
			}
			return strings.TrimSpace(orderedRefs[i].RunURL) < strings.TrimSpace(orderedRefs[j].RunURL)
		})

		environment := normalizeEnvironment(cluster.Environment)
		for _, ref := range orderedRefs {
			if len(samples) >= limit {
				break
			}
			runURL := strings.TrimSpace(ref.RunURL)
			if runURL == "" || environment == "" {
				continue
			}
			runRows := runFailuresByRun[environment+"|"+runURL]
			for _, runRow := range runRows {
				if len(samples) >= limit {
					break
				}
				if _, ok := failurePatternsStoredReferenceForRawFailure(runRow, referencesByKey); !ok {
					continue
				}
				sample := strings.TrimSpace(runRow.RawText)
				if sample == "" {
					sample = strings.TrimSpace(runRow.NormalizedText)
				}
				samples = failurePatternReportAppendUniqueLimitedSample(samples, sample, limit)
			}
		}
		out[index].FullErrorSamples = samples
	}
	return out
}

func failurePatternReportAppendUniqueLimitedSample(existing []string, candidate string, limit int) []string {
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedCandidate == "" {
		return existing
	}
	for _, value := range existing {
		if value == trimmedCandidate {
			return existing
		}
	}
	if limit > 0 && len(existing) >= limit {
		return existing
	}
	return append(existing, trimmedCandidate)
}
