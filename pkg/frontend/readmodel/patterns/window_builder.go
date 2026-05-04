package patterns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ci-failure-atlas/pkg/failurepatterns"
	failurepatternwindow "ci-failure-atlas/pkg/failurepatterns/window"
	readmodelmodel "ci-failure-atlas/pkg/frontend/readmodel/model"
	readmodelwindow "ci-failure-atlas/pkg/frontend/readmodel/window"
	sourceoptions "ci-failure-atlas/pkg/source/options"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

type FailurePatternsQuery struct {
	StartDate    string
	EndDate      string
	StartAt      string
	EndAt        string
	Week         string
	Mode         string
	Environments []string
	GeneratedAt  time.Time
}

type FailurePatternsData struct {
	Meta         FailurePatternsMeta          `json:"meta"`
	Environments []FailurePatternsEnvironment `json:"environments"`
}

type FailurePatternsMeta struct {
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	StartAt      string   `json:"start_at,omitempty"`
	EndAt        string   `json:"end_at,omitempty"`
	AnchorWeek   string   `json:"-"`
	Timezone     string   `json:"timezone"`
	GeneratedAt  string   `json:"generated_at"`
	Environments []string `json:"environments"`
}

type FailurePatternsEnvironment struct {
	Environment string                 `json:"environment"`
	Summary     FailurePatternsSummary `json:"summary"`
	Rows        []FailurePatternsRow   `json:"rows"`
}

type FailurePatternsSummary struct {
	TotalRuns           int `json:"total_runs"`
	FailedRuns          int `json:"failed_runs"`
	RawFailureCount     int `json:"raw_occurrences"`
	MatchedFailureCount int `json:"matched_occurrences"`
	JobsAffected        int `json:"runs_affected"`
}

type FailurePatternsRow struct {
	Environment             string                                 `json:"environment"`
	ClusterID               string                                 `json:"failure_pattern_id"`
	CanonicalEvidencePhrase string                                 `json:"failure_pattern"`
	SearchQueryPhrase       string                                 `json:"search_query,omitempty"`
	Lane                    string                                 `json:"failed_at,omitempty"`
	JobName                 string                                 `json:"job_name,omitempty"`
	TestName                string                                 `json:"test_name,omitempty"`
	TestSuite               string                                 `json:"test_suite,omitempty"`
	WindowFailureCount      int                                    `json:"occurrences"`
	JobsAffected            int                                    `json:"runs_affected"`
	FailedRuns              int                                    `json:"failed_runs"`
	ImpactPercent           float64                                `json:"run_impact_percent"`
	WeeklySupportCount      int                                    `json:"anchor_occurrences"`
	WeeklyPostGoodCount     int                                    `json:"after_last_push_count"`
	SeenIn                  []string                               `json:"also_in,omitempty"`
	TrendCounts             []int                                  `json:"trend_counts,omitempty"`
	TrendRange              string                                 `json:"trend_range,omitempty"`
	PriorWeeksPresent       int                                    `json:"prior_windows_present"`
	PriorWeekStarts         []string                               `json:"prior_window_starts,omitempty"`
	PriorJobsAffected       int                                    `json:"prior_runs_affected"`
	PriorLastSeenAt         string                                 `json:"prior_last_seen_at,omitempty"`
	BadPRScore              int                                    `json:"-"`
	BadPRReasons            []string                               `json:"-"`
	BadPREvaluated          bool                                   `json:"-"`
	ContributingTests       []FailurePatternReportContributingTest `json:"contributing_tests,omitempty"`
	FullErrorSamples        []string                               `json:"full_error_samples,omitempty"`
	References              []FailurePatternReportReference        `json:"affected_runs,omitempty"`
	ScoringReferences       []FailurePatternReportReference        `json:"-"`
	LinkedChildren          []FailurePatternsRow                   `json:"linked_failure_patterns,omitempty"`
	WindowEndDate           string                                 `json:"-"`
	MergeKey                string                                 `json:"-"`
}

type failurePatternsEnvironmentFacts = failurepatternwindow.EnvironmentFacts

type failurePatternsMatch struct {
	FailureCount int
	References   []FailurePatternReportReference
	RawFailures  []storecontracts.RawFailureRecord
	FailedRuns   int
}

const signalHorizonMinWeeks = 3

type WindowBuilderDeps interface {
	readmodelwindow.WeekWindowResolver
	OpenStore() (storecontracts.Store, error)
	BuildHistoryResolver(context.Context, time.Time) (failurepatterns.PresenceResolver, error)
}

func BuildWindowData(ctx context.Context, deps WindowBuilderDeps, query FailurePatternsQuery) (FailurePatternsData, error) {
	if deps == nil {
		return FailurePatternsData{}, fmt.Errorf("service is required")
	}

	scope, err := readmodelwindow.Resolve(ctx, deps, readmodelwindow.Request{
		StartDate:   query.StartDate,
		EndDate:     query.EndDate,
		StartAt:     query.StartAt,
		EndAt:       query.EndAt,
		Week:        query.Week,
		DefaultMode: readmodelwindow.DefaultLatestWeek,
	})
	if err != nil {
		return FailurePatternsData{}, err
	}
	requestedEnvironments := readmodelmodel.NormalizeStringSlice(query.Environments)
	return buildFailurePatternsInline(ctx, deps, query, scope, requestedEnvironments)
}

func buildFailurePatternsInline(
	ctx context.Context,
	deps WindowBuilderDeps,
	query FailurePatternsQuery,
	scope readmodelwindow.Scope,
	requestedEnvironments []string,
) (FailurePatternsData, error) {
	targetEnvironments := resolveFailurePatternsTargetEnvironments(requestedEnvironments)

	factsStore, err := deps.OpenStore()
	if err != nil {
		return FailurePatternsData{}, err
	}
	defer func() {
		_ = factsStore.Close()
	}()

	horizonStart := failurePatternsHorizonStart(scope)
	prepareStart := scope.StartTime
	if horizonStart.Before(prepareStart) {
		prepareStart = horizonStart
	}

	preparedWindow, err := failurepatternwindow.Prepare(ctx, factsStore, failurepatternwindow.PrepareOptions{
		Environments: targetEnvironments,
		StartTime:    prepareStart,
		EndTime:      scope.EndTime,
	})
	if err != nil {
		return FailurePatternsData{}, fmt.Errorf("prepare inline failure patterns for window %s..%s: %w", scope.StartDate, scope.EndDate, err)
	}

	currentResult, err := preparedWindow.ResultForWindow(scope.StartTime, scope.EndTime, false)
	if err != nil {
		return FailurePatternsData{}, fmt.Errorf("compute inline failure patterns for window %s..%s: %w", scope.StartDate, scope.EndDate, err)
	}

	horizonResult := currentResult
	if prepareStart.Before(scope.StartTime) {
		horizonResult, err = preparedWindow.ResultForWindow(prepareStart, scope.EndTime, false)
		if err != nil {
			return FailurePatternsData{}, fmt.Errorf("compute inline signal horizon for window %s..%s: %w", scope.StartDate, scope.EndDate, err)
		}
	}

	historyResolver, err := deps.BuildHistoryResolver(ctx, scope.EndTime)
	if err != nil {
		return FailurePatternsData{}, fmt.Errorf("build signal-horizon history resolver: %w", err)
	}

	metricRunTotals := map[string]int{}
	if scope.HasExactBounds {
		for environment, total := range currentResult.Diagnostics.RunsByEnvironment {
			metricRunTotals[environment] = total
		}
	} else {
		metricRunTotals, err = failurePatternReportMetricRunTotalsByEnvironment(
			ctx,
			factsStore,
			targetEnvironments,
			scope.StartTime,
			scope.EndTime,
		)
		if err != nil {
			return FailurePatternsData{}, fmt.Errorf("load failure-pattern metric run totals: %w", err)
		}
	}

	currentClusters := toFailurePatternReportClusters(currentResult.FailurePatterns)
	signalHorizonRefs := buildSignalHorizonReferencesForClusters(
		toFailurePatternReportClusters(horizonResult.FailurePatterns),
	)
	extractedRowsByKey := inlineExtractedRowsByMatchKey(currentResult.ExtractedRows)
	trendDays := presentationTrendDays(scope.StartTime, scope.EndTime)
	trendEndAnchor := scope.EndTime.Add(-time.Nanosecond)

	rowsByEnvironment := make(map[string][]FailurePatternsRow, len(targetEnvironments))
	phraseEnvironments := map[string]map[string]struct{}{}
	for _, cluster := range currentClusters {
		environment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
		if environment == "" {
			continue
		}
		scoringRefs := signalHorizonRefs.byMergeKey[failurePatternsMergeKeyForCluster(cluster)]
		row := buildInlineFailurePatternsRow(
			cluster,
			historyResolver,
			trendEndAnchor,
			trendDays,
			scope.EndDate,
			scoringRefs,
			extractedRowsByKey,
		)
		rowsByEnvironment[environment] = append(rowsByEnvironment[environment], row)
		collectWindowedPhraseEnvironments(row, phraseEnvironments)
	}

	generatedAt := query.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	finalEnvironments := targetEnvironments
	if len(requestedEnvironments) == 0 {
		finalEnvironments = availableFailurePatternEnvironments(targetEnvironments, currentResult, currentClusters)
	}

	environments := make([]FailurePatternsEnvironment, 0, len(finalEnvironments))
	for _, environment := range finalEnvironments {
		rows := applyWindowedSeenIn(rowsByEnvironment[environment], phraseEnvironments, environment)
		totalRuns := metricRunTotals[environment]
		if totalRuns <= 0 {
			totalRuns = currentResult.Diagnostics.RunsByEnvironment[environment]
		}
		rows = applyWindowedImpact(rows, totalRuns)
		sortFailurePatternsRows(rows)
		environments = append(environments, FailurePatternsEnvironment{
			Environment: environment,
			Summary:     buildInlineFailurePatternsSummary(currentResult.Diagnostics, environment, rows, totalRuns),
			Rows:        rows,
		})
	}

	return FailurePatternsData{
		Meta: FailurePatternsMeta{
			StartDate:    scope.StartDate,
			EndDate:      scope.EndDate,
			StartAt:      scope.StartAt,
			EndAt:        scope.EndAt,
			AnchorWeek:   scope.AnchorWeek,
			Timezone:     "UTC",
			GeneratedAt:  generatedAt.UTC().Format(time.RFC3339),
			Environments: append([]string(nil), finalEnvironments...),
		},
		Environments: environments,
	}, nil
}

func resolveFailurePatternsTargetEnvironments(requestedEnvironments []string) []string {
	if len(requestedEnvironments) > 0 {
		return append([]string(nil), requestedEnvironments...)
	}
	return readmodelmodel.NormalizeStringSlice(sourceoptions.SupportedEnvironments())
}

func failurePatternsHorizonStart(scope readmodelwindow.Scope) time.Time {
	if scope.EndTime.IsZero() {
		return scope.StartTime
	}
	anchorWeekStart := readmodelwindow.WeekStartForDate(scope.EndTime.Add(-time.Nanosecond))
	if anchorWeekStart.IsZero() {
		return scope.StartTime
	}
	return anchorWeekStart.AddDate(0, 0, -(signalHorizonMinWeeks * 7)).UTC()
}

func buildInlineFailurePatternsRow(
	cluster FailurePatternReportCluster,
	historyResolver failurepatterns.PresenceResolver,
	trendAnchor time.Time,
	trendDays int,
	windowEndDate string,
	scoringReferences []FailurePatternReportReference,
	extractedRowsByKey map[string]failurepatternwindow.ExtractedFailureRow,
) FailurePatternsRow {
	primary := primaryContributingTestForReport(cluster.ContributingTests)
	references := append([]FailurePatternReportReference(nil), cluster.References...)
	sortWindowedReferences(references)

	scoringRefs := append([]FailurePatternReportReference(nil), scoringReferences...)
	if len(scoringRefs) == 0 {
		scoringRefs = append([]FailurePatternReportReference(nil), references...)
	}
	sortWindowedReferences(scoringRefs)

	row := FailurePatternsRow{
		Environment:             readmodelmodel.NormalizeEnvironment(cluster.Environment),
		ClusterID:               strings.TrimSpace(cluster.Phase2ClusterID),
		CanonicalEvidencePhrase: strings.TrimSpace(cluster.CanonicalEvidencePhrase),
		SearchQueryPhrase:       strings.TrimSpace(cluster.SearchQueryPhrase),
		Lane:                    strings.TrimSpace(primary.Lane),
		JobName:                 strings.TrimSpace(primary.JobName),
		TestName:                strings.TrimSpace(primary.TestName),
		TestSuite:               "",
		WindowFailureCount:      cluster.SupportCount,
		JobsAffected:            windowedDistinctRunCount(references),
		FailedRuns:              windowedDistinctRunCount(references),
		WeeklySupportCount:      cluster.SupportCount,
		WeeklyPostGoodCount:     windowedPostGoodCount(scoringRefs),
		ContributingTests:       append([]FailurePatternReportContributingTest(nil), cluster.ContributingTests...),
		FullErrorSamples:        inlineFailurePatternSamples(references, extractedRowsByKey, failurePatternReportFullErrorExamplesLimit),
		References:              references,
		ScoringReferences:       scoringRefs,
		WindowEndDate:           strings.TrimSpace(windowEndDate),
		MergeKey:                failurePatternsMergeKeyForCluster(cluster),
	}

	if historyResolver != nil {
		presence := historyResolver.PresenceFor(failurepatterns.PatternKey{
			Environment: row.Environment,
			Phrase:      row.CanonicalEvidencePhrase,
			SearchQuery: row.SearchQueryPhrase,
		})
		row.PriorWeeksPresent = presence.PriorWeeksPresent
		row.PriorWeekStarts = append([]string(nil), presence.PriorWeekStarts...)
		row.PriorJobsAffected = presence.PriorJobsAffected
		if !presence.PriorLastSeenAt.IsZero() {
			row.PriorLastSeenAt = presence.PriorLastSeenAt.UTC().Format(time.RFC3339)
		}
		row.BadPRScore = presence.BadPRScore
		row.BadPRReasons = append([]string(nil), presence.BadPRReasons...)
		row.BadPREvaluated = true
	}

	if trendDays > 0 && !trendAnchor.IsZero() {
		if _, counts, trendRange, ok := readmodelmodel.DailyDensitySparkline(toWindowedHTMLRunReferences(scoringRefs), trendDays, trendAnchor); ok {
			row.TrendCounts = append([]int(nil), counts...)
			row.TrendRange = trendRange
		}
	}

	return row
}

func buildInlineFailurePatternsSummary(
	diagnostics failurepatternwindow.FailurePatternWindowDiagnostics,
	environment string,
	rows []FailurePatternsRow,
	totalRuns int,
) FailurePatternsSummary {
	matchedFailureCount := 0
	affectedRuns := map[string]struct{}{}
	for _, row := range rows {
		matchedFailureCount += row.WindowFailureCount
		for _, ref := range windowedRowAllReferences(row) {
			runURL := strings.TrimSpace(ref.RunURL)
			if runURL == "" {
				continue
			}
			affectedRuns[runURL] = struct{}{}
		}
	}
	return FailurePatternsSummary{
		TotalRuns:           totalRuns,
		FailedRuns:          diagnostics.FailedRunsByEnvironment[environment],
		RawFailureCount:     diagnostics.RawFailuresByEnvironment[environment],
		MatchedFailureCount: matchedFailureCount,
		JobsAffected:        len(affectedRuns),
	}
}

func availableFailurePatternEnvironments(
	targetEnvironments []string,
	result failurepatternwindow.FailurePatternWindowResult,
	clusters []FailurePatternReportCluster,
) []string {
	set := map[string]struct{}{}
	for _, environment := range targetEnvironments {
		if result.Diagnostics.RunsByEnvironment[environment] > 0 ||
			result.Diagnostics.RawFailuresByEnvironment[environment] > 0 ||
			result.Diagnostics.FailedRunsByEnvironment[environment] > 0 {
			set[environment] = struct{}{}
		}
	}
	for _, cluster := range clusters {
		environment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
		if environment != "" {
			set[environment] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for environment := range set {
		out = append(out, environment)
	}
	sort.Strings(out)
	return out
}

func buildSignalHorizonReferencesForClusters(
	clusters []FailurePatternReportCluster,
) signalHorizonRefSet {
	byMergeKey := map[string][]FailurePatternReportReference{}
	for _, cluster := range clusters {
		key := failurePatternsMergeKeyForCluster(cluster)
		if key == "" {
			continue
		}
		for _, ref := range cluster.References {
			byMergeKey[key] = append(byMergeKey[key], FailurePatternReportReference{
				RowID:          strings.TrimSpace(ref.RowID),
				RunURL:         strings.TrimSpace(ref.RunURL),
				OccurredAt:     strings.TrimSpace(ref.OccurredAt),
				SignatureID:    strings.TrimSpace(ref.SignatureID),
				PRNumber:       ref.PRNumber,
				PostGoodCommit: ref.PostGoodCommit,
			})
		}
	}
	for key := range byMergeKey {
		byMergeKey[key] = deduplicateSignalHorizonRefs(byMergeKey[key])
	}
	return signalHorizonRefSet{byMergeKey: byMergeKey}
}

func inlineExtractedRowsByMatchKey(
	rows []failurepatternwindow.ExtractedFailureRow,
) map[string]failurepatternwindow.ExtractedFailureRow {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]failurepatternwindow.ExtractedFailureRow, len(rows)*2)
	for _, row := range rows {
		for _, key := range inlineExtractedFailureRowMatchKeys(row) {
			out[key] = row
		}
	}
	return out
}

func inlineExtractedFailureRowMatchKeys(row failurepatternwindow.ExtractedFailureRow) []string {
	keys := make([]string, 0, 2)
	rowID := strings.TrimSpace(row.RowID)
	if rowID != "" {
		keys = append(keys, "row|"+rowID)
	}
	if key := failurePatternsReferenceTupleKey(row.RunURL, row.OccurredAt, row.SignatureID); key != "" {
		keys = append(keys, key)
	}
	return keys
}

func inlineFailurePatternSamples(
	references []FailurePatternReportReference,
	extractedRowsByKey map[string]failurepatternwindow.ExtractedFailureRow,
	limit int,
) []string {
	if len(references) == 0 || limit <= 0 {
		return nil
	}
	ordered := append([]FailurePatternReportReference(nil), references...)
	sortWindowedReferences(ordered)
	samples := make([]string, 0, limit)
	for _, ref := range ordered {
		for _, key := range failurePatternsReferenceMatchKeys(ref) {
			row, ok := extractedRowsByKey[key]
			if !ok {
				continue
			}
			samples = failurePatternReportAppendUniqueLimitedSample(
				samples,
				sampleFailureText(storecontracts.RawFailureRecord{
					RawText:        row.RawText,
					NormalizedText: row.NormalizedText,
				}),
				limit,
			)
			break
		}
		if len(samples) >= limit {
			break
		}
	}
	return samples
}

func filterFailurePatternsFactsWindow(
	facts failurePatternsEnvironmentFacts,
	startTime time.Time,
	endTime time.Time,
) failurePatternsEnvironmentFacts {
	startTime = startTime.UTC()
	endTime = endTime.UTC()
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return facts
	}
	filtered := failurePatternsEnvironmentFacts{
		RawFailures: make([]storecontracts.RawFailureRecord, 0, len(facts.RawFailures)),
		RunsByURL:   map[string]storecontracts.RunRecord{},
	}
	for _, row := range facts.RawFailures {
		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(row.OccurredAt))
		if err != nil {
			continue
		}
		occurredAt = occurredAt.UTC()
		if occurredAt.Before(startTime) || !occurredAt.Before(endTime) {
			continue
		}
		filtered.RawFailures = append(filtered.RawFailures, row)
	}
	for runURL, run := range facts.RunsByURL {
		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(run.OccurredAt))
		if err != nil {
			continue
		}
		occurredAt = occurredAt.UTC()
		if occurredAt.Before(startTime) || !occurredAt.Before(endTime) {
			continue
		}
		filtered.RunsByURL[runURL] = run
		if run.Failed {
			filtered.FailedRuns++
		}
	}
	return filtered
}

func buildFailurePatternsRow(
	cluster FailurePatternReportCluster,
	facts failurePatternsEnvironmentFacts,
	historyResolver failurepatterns.PresenceResolver,
	trendAnchor time.Time,
	windowEndDate string,
) (FailurePatternsRow, bool) {
	children := make([]FailurePatternsRow, 0, len(cluster.LinkedChildren))
	for _, child := range cluster.LinkedChildren {
		childRow, ok := buildFailurePatternsRow(child, facts, nil, trendAnchor, windowEndDate)
		if !ok {
			continue
		}
		children = append(children, childRow)
	}

	match := matchFailurePatternsCluster(cluster, facts)
	if match.FailureCount == 0 && len(children) == 0 {
		return FailurePatternsRow{}, false
	}

	primary := primaryContributingTestForReport(cluster.ContributingTests)
	references := append([]FailurePatternReportReference(nil), match.References...)
	failedRuns := match.FailedRuns
	failureCount := match.FailureCount
	fullErrorSamples := windowedFullErrorSamples(match.RawFailures, failurePatternReportFullErrorExamplesLimit)
	if len(references) == 0 && len(children) > 0 {
		references = windowedReferencesFromChildren(children)
		failedRuns = windowedFailedRunsFromReferences(references, facts.RunsByURL)
		failureCount = 0
		for _, child := range children {
			failureCount += child.WindowFailureCount
		}
		fullErrorSamples = windowedFullErrorSamplesFromChildren(children, failurePatternReportFullErrorExamplesLimit)
	}

	anchorWeekReferences := append([]FailurePatternReportReference(nil), cluster.References...)
	row := FailurePatternsRow{
		Environment:             readmodelmodel.NormalizeEnvironment(cluster.Environment),
		ClusterID:               strings.TrimSpace(cluster.Phase2ClusterID),
		CanonicalEvidencePhrase: strings.TrimSpace(cluster.CanonicalEvidencePhrase),
		SearchQueryPhrase:       strings.TrimSpace(cluster.SearchQueryPhrase),
		Lane:                    strings.TrimSpace(primary.Lane),
		JobName:                 strings.TrimSpace(primary.JobName),
		TestName:                strings.TrimSpace(primary.TestName),
		TestSuite:               "",
		WindowFailureCount:      failureCount,
		JobsAffected:            windowedDistinctRunCount(references),
		FailedRuns:              failedRuns,
		WeeklySupportCount:      cluster.SupportCount,
		WeeklyPostGoodCount:     windowedPostGoodCount(references),
		ContributingTests:       append([]FailurePatternReportContributingTest(nil), cluster.ContributingTests...),
		FullErrorSamples:        fullErrorSamples,
		References:              references,
		ScoringReferences:       append([]FailurePatternReportReference(nil), references...),
		LinkedChildren:          children,
		WindowEndDate:           strings.TrimSpace(windowEndDate),
		MergeKey:                failurePatternsMergeKeyForCluster(cluster),
	}

	if historyResolver != nil {
		presence := historyResolver.PresenceFor(failurepatterns.PatternKey{
			Environment: row.Environment,
			Phrase:      row.CanonicalEvidencePhrase,
			SearchQuery: row.SearchQueryPhrase,
		})
		row.PriorWeeksPresent = presence.PriorWeeksPresent
		row.PriorWeekStarts = append([]string(nil), presence.PriorWeekStarts...)
		row.PriorJobsAffected = presence.PriorJobsAffected
		if !presence.PriorLastSeenAt.IsZero() {
			row.PriorLastSeenAt = presence.PriorLastSeenAt.UTC().Format(time.RFC3339)
		}
		row.BadPRScore = presence.BadPRScore
		row.BadPRReasons = append([]string(nil), presence.BadPRReasons...)
		row.BadPREvaluated = true
	}

	if counts, trendRange, ok := buildWindowedTrend(anchorWeekReferences, trendAnchor); ok {
		row.TrendCounts = counts
		row.TrendRange = trendRange
	}

	sortFailurePatternsRows(row.LinkedChildren)
	return row, true
}

func buildWindowedTrend(references []FailurePatternReportReference, trendAnchor time.Time) ([]int, string, bool) {
	if trendAnchor.IsZero() {
		return nil, "", false
	}
	if _, counts, trendRange, ok := readmodelmodel.DailyDensitySparkline(toWindowedHTMLRunReferences(references), 7, trendAnchor); ok {
		return append([]int(nil), counts...), trendRange, true
	}
	return nil, "", false
}

const (
	trendMinDays = 7
	trendMaxDays = 14
)

func presentationTrendDays(start time.Time, end time.Time) int {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return trendMinDays
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < trendMinDays {
		return trendMinDays
	}
	if days > trendMaxDays {
		return trendMaxDays
	}
	return days
}

func failurePatternsTrendAnchor(week string) time.Time {
	weekStart, err := time.Parse("2006-01-02", strings.TrimSpace(week))
	if err != nil {
		return time.Now().UTC()
	}
	return weekStart.UTC().AddDate(0, 0, 7).Add(-time.Nanosecond)
}

func failurePatternsMergeKeyForCluster(cluster FailurePatternReportCluster) string {
	environment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
	if environment == "" {
		return ""
	}
	clusterID := strings.TrimSpace(cluster.Phase2ClusterID)
	phraseKey := readmodelmodel.NormalizePhrase(cluster.CanonicalEvidencePhrase)
	searchKey := readmodelmodel.NormalizePhrase(cluster.SearchQueryPhrase)
	if phraseKey == "" && searchKey == "" {
		if clusterID == "" {
			return ""
		}
		return "cluster|" + environment + "|" + clusterID
	}
	return "fallback|" + environment + "|" + phraseKey + "|" + searchKey
}

func cloneFailurePatternsRow(row FailurePatternsRow) FailurePatternsRow {
	cloned := row
	cloned.SeenIn = append([]string(nil), row.SeenIn...)
	cloned.TrendCounts = append([]int(nil), row.TrendCounts...)
	cloned.PriorWeekStarts = append([]string(nil), row.PriorWeekStarts...)
	cloned.BadPRReasons = append([]string(nil), row.BadPRReasons...)
	cloned.ContributingTests = append([]FailurePatternReportContributingTest(nil), row.ContributingTests...)
	cloned.FullErrorSamples = append([]string(nil), row.FullErrorSamples...)
	cloned.References = append([]FailurePatternReportReference(nil), row.References...)
	cloned.ScoringReferences = append([]FailurePatternReportReference(nil), row.ScoringReferences...)
	if len(row.LinkedChildren) == 0 {
		cloned.LinkedChildren = nil
		return cloned
	}
	cloned.LinkedChildren = make([]FailurePatternsRow, 0, len(row.LinkedChildren))
	for _, child := range row.LinkedChildren {
		cloned.LinkedChildren = append(cloned.LinkedChildren, cloneFailurePatternsRow(child))
	}
	return cloned
}

func mergeFailurePatternsRows(
	existing FailurePatternsRow,
	incoming FailurePatternsRow,
	runsByURL map[string]storecontracts.RunRecord,
) FailurePatternsRow {
	merged := cloneFailurePatternsRow(existing)
	merged.WindowFailureCount += incoming.WindowFailureCount
	merged.References = mergeFailurePatternsReferences(merged.References, incoming.References)
	merged.LinkedChildren = mergeFailurePatternsChildren(merged.LinkedChildren, incoming.LinkedChildren, runsByURL)
	merged.FullErrorSamples = mergeFailurePatternsSamples(merged.FullErrorSamples, incoming.FullErrorSamples, failurePatternReportFullErrorExamplesLimit)
	if strings.TrimSpace(incoming.WindowEndDate) >= strings.TrimSpace(merged.WindowEndDate) {
		merged.Environment = incoming.Environment
		merged.ClusterID = incoming.ClusterID
		merged.CanonicalEvidencePhrase = incoming.CanonicalEvidencePhrase
		merged.SearchQueryPhrase = incoming.SearchQueryPhrase
		merged.Lane = incoming.Lane
		merged.JobName = incoming.JobName
		merged.TestName = incoming.TestName
		merged.TestSuite = incoming.TestSuite
		merged.WeeklySupportCount = incoming.WeeklySupportCount
		merged.WeeklyPostGoodCount = incoming.WeeklyPostGoodCount
		merged.TrendCounts = append([]int(nil), incoming.TrendCounts...)
		merged.TrendRange = incoming.TrendRange
		merged.PriorWeeksPresent = incoming.PriorWeeksPresent
		merged.PriorWeekStarts = append([]string(nil), incoming.PriorWeekStarts...)
		merged.PriorJobsAffected = incoming.PriorJobsAffected
		merged.PriorLastSeenAt = incoming.PriorLastSeenAt
		merged.BadPRScore = incoming.BadPRScore
		merged.BadPRReasons = append([]string(nil), incoming.BadPRReasons...)
		merged.BadPREvaluated = incoming.BadPREvaluated
		merged.ContributingTests = append([]FailurePatternReportContributingTest(nil), incoming.ContributingTests...)
		merged.ScoringReferences = mergeFailurePatternsReferences(merged.ScoringReferences, incoming.ScoringReferences)
		merged.WindowEndDate = incoming.WindowEndDate
	}
	merged.JobsAffected = windowedDistinctRunCount(merged.References)
	merged.FailedRuns = windowedFailedRunsFromReferences(merged.References, runsByURL)
	merged.WeeklyPostGoodCount = windowedPostGoodCount(merged.ScoringReferences)
	if len(merged.References) == 0 && len(merged.LinkedChildren) > 0 {
		merged.References = windowedReferencesFromChildren(merged.LinkedChildren)
		merged.FullErrorSamples = windowedFullErrorSamplesFromChildren(merged.LinkedChildren, failurePatternReportFullErrorExamplesLimit)
		merged.JobsAffected = windowedDistinctRunCount(merged.References)
		merged.FailedRuns = windowedFailedRunsFromReferences(merged.References, runsByURL)
		if len(merged.ScoringReferences) == 0 {
			merged.ScoringReferences = append([]FailurePatternReportReference(nil), merged.References...)
		}
		merged.WeeklyPostGoodCount = windowedPostGoodCount(merged.ScoringReferences)
	}
	return merged
}

func mergeFailurePatternsChildren(
	existing []FailurePatternsRow,
	incoming []FailurePatternsRow,
	runsByURL map[string]storecontracts.RunRecord,
) []FailurePatternsRow {
	if len(existing) == 0 {
		out := make([]FailurePatternsRow, 0, len(incoming))
		for _, row := range incoming {
			out = append(out, cloneFailurePatternsRow(row))
		}
		return out
	}
	merged := make(map[string]FailurePatternsRow, len(existing)+len(incoming))
	order := make([]string, 0, len(existing)+len(incoming))
	for _, row := range existing {
		key := strings.TrimSpace(row.MergeKey)
		if key == "" {
			key = fmt.Sprintf("existing|%d", len(order))
		}
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		merged[key] = cloneFailurePatternsRow(row)
	}
	for _, row := range incoming {
		key := strings.TrimSpace(row.MergeKey)
		if key == "" {
			key = fmt.Sprintf("incoming|%d", len(order))
		}
		existingRow, exists := merged[key]
		if !exists {
			order = append(order, key)
			merged[key] = cloneFailurePatternsRow(row)
			continue
		}
		merged[key] = mergeFailurePatternsRows(existingRow, row, runsByURL)
	}
	out := make([]FailurePatternsRow, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	sortFailurePatternsRows(out)
	return out
}

func mergeFailurePatternsReferences(
	existing []FailurePatternReportReference,
	incoming []FailurePatternReportReference,
) []FailurePatternReportReference {
	if len(existing) == 0 {
		return append([]FailurePatternReportReference(nil), incoming...)
	}
	seen := map[string]struct{}{}
	out := make([]FailurePatternReportReference, 0, len(existing)+len(incoming))
	appendUnique := func(rows []FailurePatternReportReference) {
		for _, row := range rows {
			key := failurePatternsReferenceDedupKey(row)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, row)
		}
	}
	appendUnique(existing)
	appendUnique(incoming)
	sortWindowedReferences(out)
	return out
}

func mergeFailurePatternsSamples(existing []string, incoming []string, limit int) []string {
	out := append([]string(nil), existing...)
	for _, sample := range incoming {
		out = failurePatternReportAppendUniqueLimitedSample(out, sample, limit)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func matchFailurePatternsCluster(cluster FailurePatternReportCluster, facts failurePatternsEnvironmentFacts) failurePatternsMatch {
	referencesByKey := failurePatternsReferenceMatchMap(cluster.References)
	if len(referencesByKey) == 0 {
		return failurePatternsMatch{}
	}

	match := failurePatternsMatch{
		References:  []FailurePatternReportReference{},
		RawFailures: []storecontracts.RawFailureRecord{},
	}
	failedRunURLs := map[string]struct{}{}
	for _, row := range facts.RawFailures {
		if _, ok := failurePatternsStoredReferenceForRawFailure(row, referencesByKey); !ok {
			continue
		}
		match.FailureCount++
		match.RawFailures = append(match.RawFailures, row)
		runURL := strings.TrimSpace(row.RunURL)
		run := facts.RunsByURL[runURL]
		reference := failurePatternsReferenceFromRawFailure(row, run)
		if storedReference, ok := failurePatternsStoredReferenceForRawFailure(row, referencesByKey); ok {
			reference = failurePatternsOverlayStoredReference(reference, storedReference, run)
		}
		match.References = append(match.References, reference)
		if run.Failed && runURL != "" {
			failedRunURLs[runURL] = struct{}{}
		}
	}
	sortWindowedReferences(match.References)
	match.FailedRuns = len(failedRunURLs)
	return match
}

func failurePatternsReferenceMatchMap(rows []FailurePatternReportReference) map[string]FailurePatternReportReference {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]FailurePatternReportReference, len(rows)*2)
	for _, row := range rows {
		for _, key := range failurePatternsReferenceMatchKeys(row) {
			out[key] = row
		}
	}
	return out
}

func failurePatternsReferenceMatchKeys(row FailurePatternReportReference) []string {
	keys := make([]string, 0, 2)
	rowID := strings.TrimSpace(row.RowID)
	if rowID != "" {
		keys = append(keys, "row|"+rowID)
	}
	if key := failurePatternsReferenceTupleKey(row.RunURL, row.OccurredAt, row.SignatureID); key != "" {
		keys = append(keys, key)
	}
	return keys
}

func failurePatternsRawFailureMatchKeys(row storecontracts.RawFailureRecord) []string {
	keys := make([]string, 0, 2)
	rowID := strings.TrimSpace(row.RowID)
	if rowID != "" {
		keys = append(keys, "row|"+rowID)
	}
	if key := failurePatternsReferenceTupleKey(row.RunURL, row.OccurredAt, row.SignatureID); key != "" {
		keys = append(keys, key)
	}
	return keys
}

func failurePatternsReferenceTupleKey(runURL string, occurredAt string, signatureID string) string {
	trimmedRunURL := strings.TrimSpace(runURL)
	trimmedOccurredAt := strings.TrimSpace(occurredAt)
	trimmedSignatureID := strings.TrimSpace(signatureID)
	if trimmedRunURL == "" && trimmedOccurredAt == "" && trimmedSignatureID == "" {
		return ""
	}
	return "ref|" + trimmedRunURL + "|" + trimmedOccurredAt + "|" + trimmedSignatureID
}

func failurePatternsReferenceDedupKey(row FailurePatternReportReference) string {
	keys := failurePatternsReferenceMatchKeys(row)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func failurePatternsStoredReferenceForRawFailure(
	row storecontracts.RawFailureRecord,
	referencesByKey map[string]FailurePatternReportReference,
) (FailurePatternReportReference, bool) {
	for _, key := range failurePatternsRawFailureMatchKeys(row) {
		reference, ok := referencesByKey[key]
		if ok {
			return reference, true
		}
	}
	return FailurePatternReportReference{}, false
}

func failurePatternsReferenceFromRawFailure(
	row storecontracts.RawFailureRecord,
	run storecontracts.RunRecord,
) FailurePatternReportReference {
	return FailurePatternReportReference{
		RowID:          strings.TrimSpace(row.RowID),
		RunURL:         strings.TrimSpace(row.RunURL),
		OccurredAt:     strings.TrimSpace(row.OccurredAt),
		SignatureID:    strings.TrimSpace(row.SignatureID),
		PRNumber:       run.PRNumber,
		PostGoodCommit: run.PostGoodCommit,
	}
}

func failurePatternsOverlayStoredReference(
	raw FailurePatternReportReference,
	stored FailurePatternReportReference,
	run storecontracts.RunRecord,
) FailurePatternReportReference {
	out := raw
	if trimmed := strings.TrimSpace(stored.RowID); trimmed != "" {
		out.RowID = trimmed
	}
	if trimmed := strings.TrimSpace(stored.RunURL); trimmed != "" {
		out.RunURL = trimmed
	}
	if trimmed := strings.TrimSpace(stored.OccurredAt); trimmed != "" {
		out.OccurredAt = trimmed
	}
	if trimmed := strings.TrimSpace(stored.SignatureID); trimmed != "" {
		out.SignatureID = trimmed
	}
	if stored.PRNumber != 0 {
		out.PRNumber = stored.PRNumber
	} else if out.PRNumber == 0 {
		out.PRNumber = run.PRNumber
	}
	if stored.PostGoodCommit || run.PostGoodCommit {
		out.PostGoodCommit = true
	}
	return out
}

func collectWindowedPhraseEnvironments(row FailurePatternsRow, phraseEnvironments map[string]map[string]struct{}) {
	phraseKey := readmodelmodel.NormalizePhrase(row.CanonicalEvidencePhrase)
	if phraseKey != "" && row.WindowFailureCount > 0 {
		set := phraseEnvironments[phraseKey]
		if set == nil {
			set = map[string]struct{}{}
			phraseEnvironments[phraseKey] = set
		}
		set[row.Environment] = struct{}{}
	}
	for _, child := range row.LinkedChildren {
		collectWindowedPhraseEnvironments(child, phraseEnvironments)
	}
}

func applyWindowedSeenIn(
	rows []FailurePatternsRow,
	phraseEnvironments map[string]map[string]struct{},
	currentEnvironment string,
) []FailurePatternsRow {
	if len(rows) == 0 {
		return nil
	}
	out := append([]FailurePatternsRow(nil), rows...)
	for index := range out {
		phraseKey := readmodelmodel.NormalizePhrase(out[index].CanonicalEvidencePhrase)
		if phraseKey != "" {
			out[index].SeenIn = windowedSeenInOtherEnvironments(phraseEnvironments[phraseKey], currentEnvironment)
		}
		out[index].LinkedChildren = applyWindowedSeenIn(out[index].LinkedChildren, phraseEnvironments, currentEnvironment)
	}
	return out
}

func applyWindowedImpact(rows []FailurePatternsRow, totalRuns int) []FailurePatternsRow {
	if len(rows) == 0 {
		return nil
	}
	out := append([]FailurePatternsRow(nil), rows...)
	for index := range out {
		out[index].ImpactPercent = windowedImpactShare(out[index].JobsAffected, totalRuns)
		out[index].LinkedChildren = applyWindowedImpact(out[index].LinkedChildren, totalRuns)
	}
	return out
}

func buildFailurePatternsSummary(
	facts failurePatternsEnvironmentFacts,
	rows []FailurePatternsRow,
	totalRuns int,
) FailurePatternsSummary {
	matchedFailureCount := 0
	affectedRuns := map[string]struct{}{}
	for _, row := range rows {
		matchedFailureCount += row.WindowFailureCount
		for _, ref := range windowedRowAllReferences(row) {
			runURL := strings.TrimSpace(ref.RunURL)
			if runURL == "" {
				continue
			}
			affectedRuns[runURL] = struct{}{}
		}
	}
	return FailurePatternsSummary{
		TotalRuns:           totalRuns,
		FailedRuns:          facts.FailedRuns,
		RawFailureCount:     len(facts.RawFailures),
		MatchedFailureCount: matchedFailureCount,
		JobsAffected:        len(affectedRuns),
	}
}

func windowedRowAllReferences(row FailurePatternsRow) []FailurePatternReportReference {
	combined := append([]FailurePatternReportReference(nil), row.References...)
	for _, child := range row.LinkedChildren {
		combined = append(combined, windowedRowAllReferences(child)...)
	}
	return combined
}

func windowedDistinctRunCount(references []FailurePatternReportReference) int {
	seen := map[string]struct{}{}
	for _, row := range references {
		runURL := strings.TrimSpace(row.RunURL)
		if runURL == "" {
			continue
		}
		seen[runURL] = struct{}{}
	}
	return len(seen)
}

func windowedPostGoodCount(references []FailurePatternReportReference) int {
	if len(references) == 0 {
		return 0
	}
	total := 0
	for _, reference := range references {
		if reference.PostGoodCommit {
			total++
		}
	}
	return total
}

func windowedFailedRunsFromReferences(
	references []FailurePatternReportReference,
	runsByURL map[string]storecontracts.RunRecord,
) int {
	seen := map[string]struct{}{}
	for _, row := range references {
		runURL := strings.TrimSpace(row.RunURL)
		if runURL == "" {
			continue
		}
		run := runsByURL[runURL]
		if !run.Failed {
			continue
		}
		seen[runURL] = struct{}{}
	}
	return len(seen)
}

func windowedReferencesFromChildren(children []FailurePatternsRow) []FailurePatternReportReference {
	combined := make([]FailurePatternReportReference, 0)
	for _, child := range children {
		combined = append(combined, child.References...)
	}
	sortWindowedReferences(combined)
	return combined
}

func windowedFullErrorSamples(rows []storecontracts.RawFailureRecord, limit int) []string {
	if len(rows) == 0 || limit <= 0 {
		return nil
	}
	ordered := append([]storecontracts.RawFailureRecord(nil), rows...)
	sortWindowedRawFailures(ordered)
	samples := make([]string, 0, limit)
	for _, row := range ordered {
		samples = failurePatternReportAppendUniqueLimitedSample(samples, sampleFailureText(row), limit)
		if len(samples) >= limit {
			break
		}
	}
	return samples
}

func sampleFailureText(row storecontracts.RawFailureRecord) string {
	text := strings.TrimSpace(row.RawText)
	if text == "" {
		text = strings.TrimSpace(row.NormalizedText)
	}
	return text
}

func windowedFullErrorSamplesFromChildren(children []FailurePatternsRow, limit int) []string {
	if len(children) == 0 || limit <= 0 {
		return nil
	}
	samples := make([]string, 0, limit)
	for _, child := range children {
		for _, sample := range child.FullErrorSamples {
			samples = failurePatternReportAppendUniqueLimitedSample(samples, sample, limit)
			if len(samples) >= limit {
				return samples
			}
		}
	}
	return samples
}

func windowedSeenInOtherEnvironments(seenByEnvironment map[string]struct{}, currentEnvironment string) []string {
	if len(seenByEnvironment) == 0 {
		return nil
	}
	out := make([]string, 0, len(seenByEnvironment))
	for environment := range seenByEnvironment {
		normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
		if normalizedEnvironment == "" || normalizedEnvironment == readmodelmodel.NormalizeEnvironment(currentEnvironment) {
			continue
		}
		out = append(out, strings.ToUpper(normalizedEnvironment))
	}
	sort.Strings(out)
	return out
}

func sortFailurePatternsRows(rows []FailurePatternsRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].WindowFailureCount != rows[j].WindowFailureCount {
			return rows[i].WindowFailureCount > rows[j].WindowFailureCount
		}
		if rows[i].JobsAffected != rows[j].JobsAffected {
			return rows[i].JobsAffected > rows[j].JobsAffected
		}
		if rows[i].WeeklySupportCount != rows[j].WeeklySupportCount {
			return rows[i].WeeklySupportCount > rows[j].WeeklySupportCount
		}
		left := strings.TrimSpace(rows[i].CanonicalEvidencePhrase)
		right := strings.TrimSpace(rows[j].CanonicalEvidencePhrase)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(rows[i].ClusterID) < strings.TrimSpace(rows[j].ClusterID)
	})
}

func sortWindowedReferences(rows []FailurePatternReportReference) {
	sort.Slice(rows, func(i, j int) bool {
		ti, okI := readmodelmodel.ParseReferenceTimestamp(rows[i].OccurredAt)
		tj, okJ := readmodelmodel.ParseReferenceTimestamp(rows[j].OccurredAt)
		switch {
		case okI && okJ && !ti.Equal(tj):
			return ti.After(tj)
		case okI != okJ:
			return okI
		}
		if strings.TrimSpace(rows[i].RunURL) != strings.TrimSpace(rows[j].RunURL) {
			return strings.TrimSpace(rows[i].RunURL) < strings.TrimSpace(rows[j].RunURL)
		}
		if strings.TrimSpace(rows[i].SignatureID) != strings.TrimSpace(rows[j].SignatureID) {
			return strings.TrimSpace(rows[i].SignatureID) < strings.TrimSpace(rows[j].SignatureID)
		}
		return strings.TrimSpace(rows[i].RowID) < strings.TrimSpace(rows[j].RowID)
	})
}

func sortWindowedRawFailures(rows []storecontracts.RawFailureRecord) {
	sort.Slice(rows, func(i, j int) bool {
		ti, okI := readmodelmodel.ParseReferenceTimestamp(rows[i].OccurredAt)
		tj, okJ := readmodelmodel.ParseReferenceTimestamp(rows[j].OccurredAt)
		switch {
		case okI && okJ && !ti.Equal(tj):
			return ti.After(tj)
		case okI != okJ:
			return okI
		}
		if rows[i].RunURL != rows[j].RunURL {
			return rows[i].RunURL < rows[j].RunURL
		}
		if rows[i].SignatureID != rows[j].SignatureID {
			return rows[i].SignatureID < rows[j].SignatureID
		}
		return rows[i].RowID < rows[j].RowID
	})
}

func toWindowedHTMLRunReferences(rows []FailurePatternReportReference) []readmodelmodel.RunReference {
	out := make([]readmodelmodel.RunReference, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelmodel.RunReference{
			RunURL:      strings.TrimSpace(row.RunURL),
			OccurredAt:  strings.TrimSpace(row.OccurredAt),
			SignatureID: strings.TrimSpace(row.SignatureID),
			PRNumber:    row.PRNumber,
		})
	}
	return out
}

func primaryContributingTestForReport(rows []FailurePatternReportContributingTest) FailurePatternReportContributingTest {
	if len(rows) == 0 {
		return FailurePatternReportContributingTest{}
	}
	best := rows[0]
	for _, row := range rows[1:] {
		if row.SupportCount != best.SupportCount {
			if row.SupportCount > best.SupportCount {
				best = row
			}
			continue
		}
		currentKey := strings.TrimSpace(row.Lane) + "|" + strings.TrimSpace(row.JobName) + "|" + strings.TrimSpace(row.TestName)
		bestKey := strings.TrimSpace(best.Lane) + "|" + strings.TrimSpace(best.JobName) + "|" + strings.TrimSpace(best.TestName)
		if currentKey < bestKey {
			best = row
		}
	}
	return best
}

func windowedImpactShare(jobsAffected int, totalRuns int) float64 {
	if totalRuns <= 0 {
		return 0
	}
	return (float64(jobsAffected) * 100.0) / float64(totalRuns)
}

type signalHorizonRefSet struct {
	byMergeKey map[string][]FailurePatternReportReference
}

func buildSignalHorizonReferences(
	weeks []string,
	weeklyDataByWeek map[string]FailurePatternReportData,
) signalHorizonRefSet {
	byMergeKey := map[string][]FailurePatternReportReference{}
	for _, week := range weeks {
		data, ok := weeklyDataByWeek[week]
		if !ok {
			continue
		}
		for _, cluster := range data.FailurePatternClusters {
			key := failurePatternsMergeKeyForCluster(cluster)
			if key == "" {
				continue
			}
			for _, ref := range cluster.References {
				byMergeKey[key] = append(byMergeKey[key], FailurePatternReportReference{
					RowID:          strings.TrimSpace(ref.RowID),
					RunURL:         strings.TrimSpace(ref.RunURL),
					OccurredAt:     strings.TrimSpace(ref.OccurredAt),
					SignatureID:    strings.TrimSpace(ref.SignatureID),
					PRNumber:       ref.PRNumber,
					PostGoodCommit: ref.PostGoodCommit,
				})
			}
		}
	}
	for key := range byMergeKey {
		byMergeKey[key] = deduplicateSignalHorizonRefs(byMergeKey[key])
	}
	return signalHorizonRefSet{byMergeKey: byMergeKey}
}

func deduplicateSignalHorizonRefs(refs []FailurePatternReportReference) []FailurePatternReportReference {
	seen := map[string]struct{}{}
	out := make([]FailurePatternReportReference, 0, len(refs))
	for _, ref := range refs {
		key := failurePatternsReferenceDedupKey(ref)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	sortWindowedReferences(out)
	return out
}

func enrichRowFromSignalHorizon(
	row FailurePatternsRow,
	horizonRefs signalHorizonRefSet,
	trendAnchor time.Time,
	trendDays int,
) FailurePatternsRow {
	horizonRefsForRow := horizonRefs.byMergeKey[row.MergeKey]
	if len(horizonRefsForRow) == 0 {
		return row
	}
	enriched := row
	enriched.ScoringReferences = append([]FailurePatternReportReference(nil), horizonRefsForRow...)
	enriched.WeeklyPostGoodCount = windowedPostGoodCount(enriched.ScoringReferences)

	if trendDays > 0 && !trendAnchor.IsZero() {
		if _, counts, trendRange, ok := readmodelmodel.DailyDensitySparkline(toWindowedHTMLRunReferences(horizonRefsForRow), trendDays, trendAnchor); ok {
			enriched.TrendCounts = append([]int(nil), counts...)
			enriched.TrendRange = trendRange
		}
	}

	for i, child := range enriched.LinkedChildren {
		childHorizonRefs := horizonRefs.byMergeKey[child.MergeKey]
		if len(childHorizonRefs) > 0 {
			enriched.LinkedChildren[i].ScoringReferences = append([]FailurePatternReportReference(nil), childHorizonRefs...)
			enriched.LinkedChildren[i].WeeklyPostGoodCount = windowedPostGoodCount(enriched.LinkedChildren[i].ScoringReferences)
		}
	}
	return enriched
}
