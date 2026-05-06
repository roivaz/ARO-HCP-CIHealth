package runlog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns"
	failurepatternwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/window"
	readmodelmodel "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/model"
	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
	readmodelwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/window"
	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

type RunLogDayQuery struct {
	Date         string
	Week         string
	Environments []string
	GeneratedAt  time.Time
}

type RunLogDayData struct {
	Meta         RunLogDayMeta          `json:"meta"`
	Environments []RunLogDayEnvironment `json:"environments"`
}

type RunLogDayMeta struct {
	Date         string   `json:"date"`
	AnchorWeek   string   `json:"-"`
	Timezone     string   `json:"timezone"`
	GeneratedAt  string   `json:"generated_at"`
	Environments []string `json:"environments"`
}

type RunLogDayEnvironment struct {
	Environment string             `json:"environment"`
	Summary     RunLogDaySummary   `json:"summary"`
	Runs        []JobHistoryRunRow `json:"runs"`
}

type RunLogDaySummary struct {
	TotalRuns                  int `json:"total_runs"`
	FailedRuns                 int `json:"failed_runs"`
	RunsWithRawFailures        int `json:"runs_with_occurrences"`
	RunsWithSemanticAttachment int `json:"runs_with_failure_pattern_matches"`
	RunsUnmatchedSignatures    int `json:"runs_with_unmatched_occurrences"`
	RunsNoFailureRows          int `json:"runs_without_occurrence_rows"`
	FailedRunsWithoutRawRows   int `json:"failed_runs_without_occurrence_rows"`
}

type JobHistoryRunRow struct {
	Run             storecontracts.RunRecord  `json:"run"`
	Lanes           []string                  `json:"failed_at,omitempty"`
	FailedTestCount int                       `json:"failed_test_count"`
	FailureRows     []JobHistoryFailureRow    `json:"occurrences,omitempty"`
	SemanticRollups JobHistorySemanticRollups `json:"failure_pattern_summary"`
	BadPRScore      int                       `json:"pr_caused_score,omitempty"`
	BadPRReasons    []string                  `json:"pr_caused_reasons,omitempty"`
}

type JobHistoryFailureRow struct {
	RowID              string                       `json:"row_id"`
	RunURL             string                       `json:"run_url"`
	OccurredAt         string                       `json:"occurred_at"`
	Lane               string                       `json:"failed_at,omitempty"`
	SignatureID        string                       `json:"signature_id,omitempty"`
	TestName           string                       `json:"test_name,omitempty"`
	TestSuite          string                       `json:"test_suite,omitempty"`
	FailureText        string                       `json:"failure_text,omitempty"`
	NonArtifactBacked  bool                         `json:"non_artifact_backed,omitempty"`
	SemanticAttachment JobHistorySemanticAttachment `json:"failure_pattern_match"`
	PriorWeeksPresent  int                          `json:"-"`
	BadPRScore         int                          `json:"-"`
	BadPRReasons       []string                     `json:"-"`
}

type JobHistorySemanticAttachment struct {
	Status                  string `json:"status"`
	ClusterID               string `json:"failure_pattern_id,omitempty"`
	CanonicalEvidencePhrase string `json:"failure_pattern,omitempty"`
	SearchQueryPhrase       string `json:"search_query,omitempty"`
}

type JobHistorySemanticRollups struct {
	SignatureCount     int      `json:"signature_count"`
	DistinctClusterIDs []string `json:"failure_pattern_ids,omitempty"`
	ClusteredRows      int      `json:"matched_occurrences"`
	UnmatchedRows      int      `json:"unmatched_occurrences"`
	AttachmentSummary  string   `json:"match_summary"`
}

type jobHistoryReferenceCluster struct {
	ClusterID               string
	CanonicalEvidencePhrase string
	SearchQueryPhrase       string
	Lane                    string
	SupportCount            int
	PriorWeeksPresent       int
	BadPRScore              int
	BadPRReasons            []string
}

type DayBuilderDeps interface {
	readmodelwindow.WeekWindowResolver
	OpenStore() (storecontracts.Store, error)
	HistoryHorizonWeeks() int
	PrepareFailurePatternWindow(context.Context, failurepatternwindow.PrepareOptions) (failurepatternwindow.PreparedWindow, error)
	BuildHistoryResolver(context.Context, time.Time) (failurepatterns.PresenceResolver, error)
}

func BuildDay(ctx context.Context, deps DayBuilderDeps, query RunLogDayQuery) (RunLogDayData, error) {
	if deps == nil {
		return RunLogDayData{}, fmt.Errorf("service is required")
	}
	dateLabel, _, err := readmodelwindow.NormalizeDateLabel(query.Date)
	if err != nil {
		return RunLogDayData{}, fmt.Errorf("invalid date: %w", err)
	}
	window, err := readmodelwindow.Resolve(ctx, deps, readmodelwindow.Request{Date: dateLabel})
	if err != nil {
		return RunLogDayData{}, err
	}

	targetEnvironments := readmodelmodel.NormalizeStringSlice(query.Environments)
	if len(targetEnvironments) == 0 {
		targetEnvironments = readmodelmodel.NormalizeStringSlice(sourceoptions.SupportedEnvironments())
	}

	historyWindow, err := failurepatterns.ResolvePresenceWindow(window.EndTime, deps.HistoryHorizonWeeks())
	if err != nil {
		return RunLogDayData{}, fmt.Errorf("resolve run history horizon: %w", err)
	}
	prepareStart := window.StartTime
	if historyWindow.LookbackStart.Before(prepareStart) {
		prepareStart = historyWindow.LookbackStart
	}

	preparedWindow, err := deps.PrepareFailurePatternWindow(ctx, failurepatternwindow.PrepareOptions{
		Environments: targetEnvironments,
		StartTime:    prepareStart,
		EndTime:      window.EndTime,
	})
	if err != nil {
		return RunLogDayData{}, fmt.Errorf("prepare failure-pattern day inputs for run history: %w", err)
	}

	currentResult, err := preparedWindow.ResultForWindow(window.StartTime, window.EndTime, false)
	if err != nil {
		return RunLogDayData{}, fmt.Errorf("compute failure-pattern day data for run history: %w", err)
	}
	currentClusters := toFailurePatternReportClusters(currentResult.FailurePatterns)
	if len(query.Environments) == 0 {
		targetEnvironments = availableFailurePatternEnvironments(targetEnvironments, currentResult, currentClusters)
	}

	historyResult := currentResult
	if historyWindow.LookbackStart.Before(window.StartTime) {
		historyResult, err = preparedWindow.ResultForWindow(historyWindow.LookbackStart, window.EndTime, false)
		if err != nil {
			return RunLogDayData{}, fmt.Errorf("compute failure-pattern history horizon for run history: %w", err)
		}
	}
	historyResolver, err := failurepatterns.BuildPresenceResolverFromFailurePatterns(
		failurepatterns.BuildPresenceFromFailurePatternsOptions{
			EndTime:         window.EndTime,
			LookbackWeeks:   deps.HistoryHorizonWeeks(),
			FailurePatterns: historyResult.FailurePatterns,
		},
	)
	if err != nil {
		return RunLogDayData{}, fmt.Errorf("build run history presence resolver: %w", err)
	}
	allFactsByEnvironment := preparedWindow.FactsByEnvironment()
	factsByEnvironment := make(map[string]failurePatternsEnvironmentFacts, len(targetEnvironments))
	for _, environment := range targetEnvironments {
		normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
		if normalizedEnvironment == "" {
			continue
		}
		factsByEnvironment[normalizedEnvironment] = filterFailurePatternsFactsWindow(
			allFactsByEnvironment[normalizedEnvironment],
			window.StartTime,
			window.EndTime,
		)
	}

	clusterByReference := buildJobHistoryReferenceIndex(currentClusters, historyResolver)

	generatedAt := query.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	environments := make([]RunLogDayEnvironment, 0, len(targetEnvironments))
	for _, environment := range targetEnvironments {
		normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
		if normalizedEnvironment == "" {
			continue
		}
		runs := buildJobHistoryRunRows(
			normalizedEnvironment,
			factsByEnvironment[normalizedEnvironment],
			clusterByReference,
		)
		environments = append(environments, RunLogDayEnvironment{
			Environment: normalizedEnvironment,
			Summary:     buildRunLogDaySummary(runs),
			Runs:        runs,
		})
	}

	return RunLogDayData{
		Meta: RunLogDayMeta{
			Date:         window.StartDate,
			AnchorWeek:   window.AnchorWeek,
			Timezone:     "UTC",
			GeneratedAt:  generatedAt.UTC().Format(time.RFC3339),
			Environments: append([]string(nil), targetEnvironments...),
		},
		Environments: environments,
	}, nil
}

func buildJobHistoryRunRows(
	environment string,
	facts failurePatternsEnvironmentFacts,
	clusterByReference map[string]jobHistoryReferenceCluster,
) []JobHistoryRunRow {
	rawFailuresByRun := map[string][]storecontracts.RawFailureRecord{}
	for _, row := range facts.RawFailures {
		runURL := strings.TrimSpace(row.RunURL)
		if runURL == "" {
			continue
		}
		rawFailuresByRun[runURL] = append(rawFailuresByRun[runURL], row)
	}

	runRows := make([]JobHistoryRunRow, 0, len(facts.RunsByURL))
	for _, run := range facts.RunsByURL {
		runURL := strings.TrimSpace(run.RunURL)
		if runURL == "" {
			continue
		}
		failures := buildJobHistoryFailureRows(
			environment,
			rawFailuresByRun[runURL],
			clusterByReference,
		)
		badPRScore, badPRReasons := jobHistoryWeeklyBadPR(failures)
		runRows = append(runRows, JobHistoryRunRow{
			Run:             run,
			Lanes:           jobHistoryRunLanes(failures),
			FailedTestCount: jobHistoryFailedTestCount(failures),
			FailureRows:     failures,
			SemanticRollups: buildJobHistorySemanticRollups(run, failures),
			BadPRScore:      badPRScore,
			BadPRReasons:    badPRReasons,
		})
	}

	sortJobHistoryRunRows(runRows)
	return runRows
}

func buildJobHistoryFailureRows(
	environment string,
	rawFailures []storecontracts.RawFailureRecord,
	clusterByReference map[string]jobHistoryReferenceCluster,
) []JobHistoryFailureRow {
	rows := make([]JobHistoryFailureRow, 0, len(rawFailures))
	for _, row := range rawFailures {
		signatureID := strings.TrimSpace(row.SignatureID)
		cluster, matched := findJobHistoryClusterForRawFailure(environment, row, clusterByReference)

		attachment := JobHistorySemanticAttachment{
			Status: "unmatched",
		}
		if matched {
			attachment = JobHistorySemanticAttachment{
				Status:                  "clustered",
				ClusterID:               strings.TrimSpace(cluster.ClusterID),
				CanonicalEvidencePhrase: strings.TrimSpace(cluster.CanonicalEvidencePhrase),
				SearchQueryPhrase:       strings.TrimSpace(cluster.SearchQueryPhrase),
			}
		}

		rows = append(rows, JobHistoryFailureRow{
			RowID:              strings.TrimSpace(row.RowID),
			RunURL:             strings.TrimSpace(row.RunURL),
			OccurredAt:         strings.TrimSpace(row.OccurredAt),
			Lane:               strings.TrimSpace(cluster.Lane),
			SignatureID:        signatureID,
			TestName:           strings.TrimSpace(row.TestName),
			TestSuite:          strings.TrimSpace(row.TestSuite),
			FailureText:        jobHistoryFailureText(row),
			NonArtifactBacked:  row.NonArtifactBacked,
			SemanticAttachment: attachment,
			PriorWeeksPresent:  cluster.PriorWeeksPresent,
			BadPRScore:         cluster.BadPRScore,
			BadPRReasons:       append([]string(nil), cluster.BadPRReasons...),
		})
	}

	sortJobHistoryFailureRows(rows)
	return rows
}

func buildJobHistorySemanticRollups(run storecontracts.RunRecord, failures []JobHistoryFailureRow) JobHistorySemanticRollups {
	signatureIDs := map[string]struct{}{}
	clusterIDs := map[string]struct{}{}
	clusteredRows := 0
	unmatchedRows := 0

	for _, row := range failures {
		if trimmedSignatureID := strings.TrimSpace(row.SignatureID); trimmedSignatureID != "" {
			signatureIDs[trimmedSignatureID] = struct{}{}
		}
		switch strings.TrimSpace(row.SemanticAttachment.Status) {
		case "clustered":
			clusteredRows++
			if trimmedClusterID := strings.TrimSpace(row.SemanticAttachment.ClusterID); trimmedClusterID != "" {
				clusterIDs[trimmedClusterID] = struct{}{}
			}
		default:
			unmatchedRows++
		}
	}

	return JobHistorySemanticRollups{
		SignatureCount:     len(signatureIDs),
		DistinctClusterIDs: readmodelmodel.SortedStringSet(clusterIDs),
		ClusteredRows:      clusteredRows,
		UnmatchedRows:      unmatchedRows,
		AttachmentSummary:  buildJobHistoryAttachmentSummary(run, len(signatureIDs), len(clusterIDs), clusteredRows, unmatchedRows),
	}
}

func buildJobHistoryAttachmentSummary(
	run storecontracts.RunRecord,
	signatureCount int,
	distinctClusterCount int,
	clusteredRows int,
	unmatchedRows int,
) string {
	if signatureCount == 0 {
		if run.Failed {
			return "failed_without_raw_rows"
		}
		return "no_failures"
	}
	if clusteredRows == 0 {
		return "unmatched_only"
	}
	if unmatchedRows > 0 {
		return "mixed"
	}
	if signatureCount == 1 && distinctClusterCount == 1 {
		return "single_clustered"
	}
	return "multiple_clustered"
}

func buildRunLogDaySummary(runs []JobHistoryRunRow) RunLogDaySummary {
	summary := RunLogDaySummary{
		TotalRuns: len(runs),
	}
	for _, row := range runs {
		if row.Run.Failed {
			summary.FailedRuns++
		}
		if len(row.FailureRows) == 0 {
			summary.RunsNoFailureRows++
			if row.Run.Failed {
				summary.FailedRunsWithoutRawRows++
			}
			continue
		}
		summary.RunsWithRawFailures++
		if row.SemanticRollups.ClusteredRows > 0 {
			summary.RunsWithSemanticAttachment++
		}
		if row.SemanticRollups.UnmatchedRows > 0 {
			summary.RunsUnmatchedSignatures++
		}
	}
	return summary
}

func buildJobHistoryReferenceIndex(
	clusters []readmodelpatterns.FailurePatternReportCluster,
	historyResolver failurepatterns.PresenceResolver,
) map[string]jobHistoryReferenceCluster {
	index := map[string]jobHistoryReferenceCluster{}
	for _, cluster := range clusters {
		environment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
		if environment == "" {
			continue
		}
		presence := failurepatterns.PatternPresence{}
		if historyResolver != nil {
			presence = historyResolver.PresenceFor(failurepatterns.PatternKey{
				Environment: environment,
				Phrase:      strings.TrimSpace(cluster.CanonicalEvidencePhrase),
				SearchQuery: strings.TrimSpace(cluster.SearchQueryPhrase),
			})
		}
		candidate := jobHistoryReferenceCluster{
			ClusterID:               strings.TrimSpace(cluster.Phase2ClusterID),
			CanonicalEvidencePhrase: strings.TrimSpace(cluster.CanonicalEvidencePhrase),
			SearchQueryPhrase:       strings.TrimSpace(cluster.SearchQueryPhrase),
			Lane:                    strings.TrimSpace(primaryContributingTestForReport(cluster.ContributingTests).Lane),
			SupportCount:            cluster.SupportCount,
			PriorWeeksPresent:       presence.PriorWeeksPresent,
			BadPRScore:              presence.BadPRScore,
			BadPRReasons:            append([]string(nil), presence.BadPRReasons...),
		}
		for _, key := range jobHistoryReferenceKeys(environment, cluster.References) {
			if key == "" {
				continue
			}
			current, exists := index[key]
			if !exists || jobHistoryPrefersClusterCandidate(current, candidate) {
				index[key] = candidate
			}
		}
	}
	return index
}

func jobHistoryPhraseEnvironments(clusters []readmodelpatterns.FailurePatternReportCluster) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, cluster := range clusters {
		environment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
		phraseKey := readmodelmodel.NormalizePhrase(cluster.CanonicalEvidencePhrase)
		if environment == "" || phraseKey == "" {
			continue
		}
		set := out[phraseKey]
		if set == nil {
			set = map[string]struct{}{}
			out[phraseKey] = set
		}
		set[environment] = struct{}{}
	}
	return out
}

func jobHistorySignalReferences(
	cluster readmodelpatterns.FailurePatternReportCluster,
	supportRefs signalHorizonRefSet,
) []readmodelpatterns.FailurePatternReportReference {
	references := supportRefs.byMergeKey[failurePatternsMergeKeyForCluster(cluster)]
	if len(references) == 0 {
		return append([]readmodelpatterns.FailurePatternReportReference(nil), cluster.References...)
	}
	return append([]readmodelpatterns.FailurePatternReportReference(nil), references...)
}

func jobHistoryPrefersClusterCandidate(current jobHistoryReferenceCluster, candidate jobHistoryReferenceCluster) bool {
	if candidate.SupportCount != current.SupportCount {
		return candidate.SupportCount > current.SupportCount
	}
	currentPhrase := strings.TrimSpace(current.CanonicalEvidencePhrase)
	candidatePhrase := strings.TrimSpace(candidate.CanonicalEvidencePhrase)
	if candidatePhrase != currentPhrase {
		return candidatePhrase < currentPhrase
	}
	return strings.TrimSpace(candidate.ClusterID) < strings.TrimSpace(current.ClusterID)
}

func findJobHistoryClusterForRawFailure(
	environment string,
	row storecontracts.RawFailureRecord,
	clusterByReference map[string]jobHistoryReferenceCluster,
) (jobHistoryReferenceCluster, bool) {
	for _, key := range jobHistoryRawFailureKeys(environment, row) {
		cluster, ok := clusterByReference[key]
		if ok {
			return cluster, true
		}
	}
	return jobHistoryReferenceCluster{}, false
}

func jobHistoryReferenceKeys(environment string, references []readmodelpatterns.FailurePatternReportReference) []string {
	if len(references) == 0 {
		return nil
	}
	normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
	if normalizedEnvironment == "" {
		return nil
	}
	out := make([]string, 0, len(references)*2)
	seen := map[string]struct{}{}
	appendKey := func(key string) {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, reference := range references {
		appendKey(jobHistoryReferenceRowKey(normalizedEnvironment, reference.RowID))
		appendKey(jobHistoryReferenceTupleKey(normalizedEnvironment, reference.RunURL, reference.OccurredAt, reference.SignatureID))
	}
	return out
}

func jobHistoryRawFailureKeys(environment string, row storecontracts.RawFailureRecord) []string {
	normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
	if normalizedEnvironment == "" {
		return nil
	}
	out := make([]string, 0, 2)
	if key := jobHistoryReferenceRowKey(normalizedEnvironment, row.RowID); key != "" {
		out = append(out, key)
	}
	if key := jobHistoryReferenceTupleKey(normalizedEnvironment, row.RunURL, row.OccurredAt, row.SignatureID); key != "" {
		out = append(out, key)
	}
	return out
}

func jobHistoryReferenceRowKey(environment string, rowID string) string {
	return failurepatterns.ReferenceRowMatchKey(environment, rowID)
}

func jobHistoryReferenceTupleKey(environment string, runURL string, occurredAt string, signatureID string) string {
	return failurepatterns.ReferenceTupleMatchKey(environment, runURL, occurredAt, signatureID)
}

func sortJobHistoryRunRows(rows []JobHistoryRunRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := strings.TrimSpace(rows[i].Run.OccurredAt)
		right := strings.TrimSpace(rows[j].Run.OccurredAt)
		if left != right {
			return left > right
		}
		leftJob := strings.TrimSpace(rows[i].Run.JobName)
		rightJob := strings.TrimSpace(rows[j].Run.JobName)
		if leftJob != rightJob {
			return leftJob < rightJob
		}
		return strings.TrimSpace(rows[i].Run.RunURL) < strings.TrimSpace(rows[j].Run.RunURL)
	})
}

func sortJobHistoryFailureRows(rows []JobHistoryFailureRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := strings.TrimSpace(rows[i].OccurredAt)
		right := strings.TrimSpace(rows[j].OccurredAt)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(rows[i].RowID) < strings.TrimSpace(rows[j].RowID)
	})
}

func jobHistoryRunLanes(rows []JobHistoryFailureRow) []string {
	set := map[string]struct{}{}
	for _, row := range rows {
		if lane := strings.TrimSpace(row.Lane); lane != "" {
			set[lane] = struct{}{}
		}
	}
	return readmodelmodel.SortedStringSet(set)
}

func jobHistoryFailedTestCount(rows []JobHistoryFailureRow) int {
	if len(rows) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, row := range rows {
		testName := strings.TrimSpace(row.TestName)
		testSuite := strings.TrimSpace(row.TestSuite)
		switch {
		case testName != "" || testSuite != "":
			set[testSuite+"|"+testName] = struct{}{}
		case strings.TrimSpace(row.RowID) != "":
			set["row|"+strings.TrimSpace(row.RowID)] = struct{}{}
		default:
			set["failure|"+strings.TrimSpace(row.OccurredAt)+"|"+strings.TrimSpace(row.RunURL)] = struct{}{}
		}
	}
	return len(set)
}

func jobHistoryWeeklyBadPR(failures []JobHistoryFailureRow) (int, []string) {
	bestScore := 0
	var bestReasons []string
	seen := map[string]struct{}{}
	for _, row := range failures {
		if strings.TrimSpace(row.SemanticAttachment.Status) != "clustered" {
			continue
		}
		key := strings.TrimSpace(row.SemanticAttachment.ClusterID)
		if key == "" {
			key = strings.TrimSpace(row.SignatureID)
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		if row.BadPRScore <= bestScore {
			continue
		}
		bestScore = row.BadPRScore
		bestReasons = append([]string(nil), row.BadPRReasons...)
	}
	return bestScore, bestReasons
}

func jobHistoryFailureText(row storecontracts.RawFailureRecord) string {
	text := strings.TrimSpace(row.RawText)
	if text != "" {
		return text
	}
	return strings.TrimSpace(row.NormalizedText)
}
