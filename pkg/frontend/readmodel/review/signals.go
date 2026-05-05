package review

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ci-failure-atlas/pkg/failurepatterns"
	failurepatterncontracts "ci-failure-atlas/pkg/failurepatterns/contracts"
	failurepatternwindow "ci-failure-atlas/pkg/failurepatterns/window"
	readmodelmodel "ci-failure-atlas/pkg/frontend/readmodel/model"
	readmodelwindow "ci-failure-atlas/pkg/frontend/readmodel/window"
	sourceoptions "ci-failure-atlas/pkg/source/options"
)

type ReviewSignalReference struct {
	RowID          string `json:"row_id,omitempty"`
	RunURL         string `json:"run_url"`
	OccurredAt     string `json:"occurred_at"`
	SignatureID    string `json:"signature_id"`
	PRNumber       int    `json:"pr_number"`
	PostGoodCommit bool   `json:"after_last_push_of_merged_pr"`
}

type ReviewSignalMatchedFailurePattern struct {
	Environment      string `json:"environment"`
	FailurePatternID string `json:"failure_pattern_id"`
	FailurePattern   string `json:"failure_pattern"`
	SearchQuery      string `json:"search_query,omitempty"`
}

type ReviewSignalRow struct {
	Environment                          string                              `json:"environment"`
	ReviewItemID                         string                              `json:"review_item_id"`
	Phase                                string                              `json:"phase"`
	Reason                               string                              `json:"reason"`
	Severity                             string                              `json:"severity,omitempty"`
	ProposedFailurePattern               string                              `json:"proposed_failure_pattern,omitempty"`
	ProposedSearchQuery                  string                              `json:"proposed_search_query,omitempty"`
	ProposedSearchQuerySourceRunURL      string                              `json:"proposed_search_query_source_run_url,omitempty"`
	ProposedSearchQuerySourceSignatureID string                              `json:"proposed_search_query_source_signature_id,omitempty"`
	SourcePhase1ClusterIDs               []string                            `json:"source_phase1_cluster_ids,omitempty"`
	MemberSignatureIDs                   []string                            `json:"member_signature_ids,omitempty"`
	References                           []ReviewSignalReference             `json:"references,omitempty"`
	MatchedFailurePatterns               []ReviewSignalMatchedFailurePattern `json:"matched_failure_patterns,omitempty"`
}

type WindowQuery struct {
	StartDate   string
	EndDate     string
	GeneratedAt time.Time
}

type WindowData struct {
	Meta              WindowMeta        `json:"meta"`
	TotalSignals      int               `json:"total_signals"`
	SignalsByReason   map[string]int    `json:"signals_by_reason,omitempty"`
	SignalsBySeverity map[string]int    `json:"signals_by_severity,omitempty"`
	Rows              []ReviewSignalRow `json:"rows"`
}

type WindowMeta struct {
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	Timezone     string   `json:"timezone"`
	GeneratedAt  string   `json:"generated_at"`
	Environments []string `json:"environments"`
}

type BuilderDeps interface {
	readmodelwindow.WeekWindowResolver
	HistoryHorizonWeeks() int
	PrepareFailurePatternWindow(context.Context, failurepatternwindow.PrepareOptions) (failurepatternwindow.PreparedWindow, error)
}

func BuildWindow(
	ctx context.Context,
	deps BuilderDeps,
	query WindowQuery,
) (WindowData, error) {
	if deps == nil {
		return WindowData{}, fmt.Errorf("builder dependencies are required")
	}
	scope, err := readmodelwindow.Resolve(ctx, deps, readmodelwindow.Request{
		StartDate:   query.StartDate,
		EndDate:     query.EndDate,
		DefaultMode: readmodelwindow.DefaultRolling,
		RollingDays: 7,
	})
	if err != nil {
		return WindowData{}, err
	}

	targetEnvironments := reviewTargetEnvironments()
	windowDuration := scope.EndTime.Sub(scope.StartTime)
	if windowDuration <= 0 {
		return WindowData{}, fmt.Errorf("valid review window duration is required")
	}

	historyWeeks := deps.HistoryHorizonWeeks()
	if historyWeeks <= 0 {
		historyWeeks = failurepatterns.DefaultHistoryWeeks
	}
	compareStart := scope.StartTime.Add(-windowDuration).UTC()
	historyStart := scope.StartTime.AddDate(0, 0, -(historyWeeks * 7)).UTC()
	prepareStart := scope.StartTime
	if compareStart.Before(prepareStart) {
		prepareStart = compareStart
	}
	if historyStart.Before(prepareStart) {
		prepareStart = historyStart
	}

	preparedWindow, err := deps.PrepareFailurePatternWindow(ctx, failurepatternwindow.PrepareOptions{
		Environments: targetEnvironments,
		StartTime:    prepareStart,
		EndTime:      scope.EndTime,
	})
	if err != nil {
		return WindowData{}, fmt.Errorf("prepare review signals window %s..%s: %w", scope.StartDate, scope.EndDate, err)
	}

	currentResult, err := preparedWindow.ResultForWindow(scope.StartTime, scope.EndTime, true)
	if err != nil {
		return WindowData{}, fmt.Errorf("compute review signals window %s..%s: %w", scope.StartDate, scope.EndDate, err)
	}
	previousResult, err := preparedWindow.ResultForWindow(compareStart, scope.StartTime, false)
	if err != nil {
		return WindowData{}, fmt.Errorf("compute previous review comparison window %s..%s: %w", compareStart.Format(time.RFC3339), scope.StartTime.Format(time.RFC3339), err)
	}
	historyResult, err := preparedWindow.ResultForWindow(historyStart, scope.StartTime, false)
	if err != nil {
		return WindowData{}, fmt.Errorf("compute prior review history window %s..%s: %w", historyStart.Format(time.RFC3339), scope.StartTime.Format(time.RFC3339), err)
	}

	sourceClusters := append([]failurepatterncontracts.FailurePatternRecord(nil), currentResult.FailurePatterns...)
	rows := make([]ReviewSignalRow, 0, len(currentResult.ReviewItems))
	signalsByReason := map[string]int{}

	windowSignalRows := crossWindowPatternSignals(
		currentResult.FailurePatterns,
		previousResult.FailurePatterns,
		historyResult.FailurePatterns,
	)
	for i := range windowSignalRows {
		reason := windowSignalRows[i].Reason
		if reason != "" {
			signalsByReason[reason]++
		}
		rows = append(rows, windowSignalRows[i])
	}

	for _, item := range currentResult.ReviewItems {
		reason := strings.TrimSpace(item.Reason)
		if reason != "" {
			signalsByReason[reason]++
		}
		rows = append(rows, ReviewSignalRow{
			Environment:                          readmodelmodel.NormalizeEnvironment(item.Environment),
			ReviewItemID:                         strings.TrimSpace(item.ReviewItemID),
			Phase:                                strings.TrimSpace(item.Phase),
			Reason:                               reason,
			Severity:                             strings.TrimSpace(item.Severity),
			ProposedFailurePattern:               strings.TrimSpace(item.ProposedCanonicalEvidencePhrase),
			ProposedSearchQuery:                  strings.TrimSpace(item.ProposedSearchQueryPhrase),
			ProposedSearchQuerySourceRunURL:      strings.TrimSpace(item.ProposedSearchQuerySourceRunURL),
			ProposedSearchQuerySourceSignatureID: strings.TrimSpace(item.ProposedSearchQuerySourceSignatureID),
			SourcePhase1ClusterIDs:               reviewSignalCopyStrings(item.SourcePhase1ClusterIDs),
			MemberSignatureIDs:                   reviewSignalCopyStrings(item.MemberSignatureIDs),
			References:                           reviewSignalReferences(item.References),
			MatchedFailurePatterns:               reviewSignalMatchedFailurePatterns(item, sourceClusters),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		si := reviewSignalSeverityRank(rows[i].Severity)
		sj := reviewSignalSeverityRank(rows[j].Severity)
		if si != sj {
			return si < sj
		}
		if rows[i].Environment != rows[j].Environment {
			return rows[i].Environment < rows[j].Environment
		}
		if rows[i].Phase != rows[j].Phase {
			return rows[i].Phase < rows[j].Phase
		}
		if rows[i].Reason != rows[j].Reason {
			return rows[i].Reason < rows[j].Reason
		}
		return rows[i].ReviewItemID < rows[j].ReviewItemID
	})

	signalsBySeverity := map[string]int{}
	for _, row := range rows {
		sev := strings.TrimSpace(row.Severity)
		if sev == "" {
			sev = "unset"
		}
		signalsBySeverity[sev]++
	}

	generatedAt := query.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	return WindowData{
		Meta: WindowMeta{
			StartDate:    scope.StartDate,
			EndDate:      scope.EndDate,
			Timezone:     "UTC",
			GeneratedAt:  generatedAt.UTC().Format(time.RFC3339),
			Environments: append([]string(nil), targetEnvironments...),
		},
		TotalSignals:      len(rows),
		SignalsByReason:   signalsByReason,
		SignalsBySeverity: signalsBySeverity,
		Rows:              rows,
	}, nil
}

func reviewSignalReferences(rows []failurepatterncontracts.ReferenceRecord) []ReviewSignalReference {
	if len(rows) == 0 {
		return nil
	}
	out := make([]ReviewSignalReference, 0, len(rows))
	for _, row := range rows {
		out = append(out, ReviewSignalReference{
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

func reviewTargetEnvironments() []string {
	return readmodelmodel.NormalizeStringSlice(sourceoptions.SupportedEnvironments())
}

func reviewSignalMatchedFailurePatterns(
	item failurepatterncontracts.ReviewItemRecord,
	clusters []failurepatterncontracts.FailurePatternRecord,
) []ReviewSignalMatchedFailurePattern {
	if len(clusters) == 0 {
		return nil
	}
	environment := readmodelmodel.NormalizeEnvironment(item.Environment)
	if environment == "" {
		return nil
	}
	sourcePhase1IDs := map[string]struct{}{}
	for _, phase1ID := range item.SourcePhase1ClusterIDs {
		trimmed := strings.TrimSpace(phase1ID)
		if trimmed == "" {
			continue
		}
		sourcePhase1IDs[trimmed] = struct{}{}
	}
	referenceKeys := map[string]struct{}{}
	for _, key := range reviewSignalReferenceKeys(item.Environment, item.References) {
		referenceKeys[key] = struct{}{}
	}

	out := make([]ReviewSignalMatchedFailurePattern, 0, 2)
	seen := map[string]struct{}{}
	for _, cluster := range clusters {
		clusterEnvironment := readmodelmodel.NormalizeEnvironment(cluster.Environment)
		if clusterEnvironment == "" || clusterEnvironment != environment {
			continue
		}
		if !reviewSignalMatchesCluster(sourcePhase1IDs, referenceKeys, cluster) {
			continue
		}
		matched := ReviewSignalMatchedFailurePattern{
			Environment:      clusterEnvironment,
			FailurePatternID: strings.TrimSpace(cluster.Phase2ClusterID),
			FailurePattern:   strings.TrimSpace(cluster.CanonicalEvidencePhrase),
			SearchQuery:      strings.TrimSpace(cluster.SearchQueryPhrase),
		}
		key := matched.Environment + "|" + matched.FailurePatternID
		if key == "|" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, matched)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Environment != out[j].Environment {
			return out[i].Environment < out[j].Environment
		}
		if out[i].FailurePattern != out[j].FailurePattern {
			return out[i].FailurePattern < out[j].FailurePattern
		}
		return out[i].FailurePatternID < out[j].FailurePatternID
	})
	return out
}

func reviewSignalCurrentPatternMatchedFailurePatterns(
	pattern failurepatterncontracts.FailurePatternRecord,
) []ReviewSignalMatchedFailurePattern {
	environment := readmodelmodel.NormalizeEnvironment(pattern.Environment)
	failurePatternID := strings.TrimSpace(pattern.Phase2ClusterID)
	failurePattern := strings.TrimSpace(pattern.CanonicalEvidencePhrase)
	searchQuery := strings.TrimSpace(pattern.SearchQueryPhrase)
	if environment == "" && failurePatternID == "" && failurePattern == "" && searchQuery == "" {
		return nil
	}
	return []ReviewSignalMatchedFailurePattern{{
		Environment:      environment,
		FailurePatternID: failurePatternID,
		FailurePattern:   failurePattern,
		SearchQuery:      searchQuery,
	}}
}

func reviewSignalMatchesCluster(
	sourcePhase1IDs map[string]struct{},
	referenceKeys map[string]struct{},
	cluster failurepatterncontracts.FailurePatternRecord,
) bool {
	for _, phase1ID := range cluster.MemberPhase1ClusterIDs {
		if _, exists := sourcePhase1IDs[strings.TrimSpace(phase1ID)]; exists {
			return true
		}
	}
	for _, key := range reviewSignalReferenceKeys(cluster.Environment, cluster.References) {
		if _, exists := referenceKeys[key]; exists {
			return true
		}
	}
	return false
}

func reviewSignalReferenceKeys(environment string, references []failurepatterncontracts.ReferenceRecord) []string {
	if len(references) == 0 {
		return nil
	}
	normalizedEnvironment := readmodelmodel.NormalizeEnvironment(environment)
	if normalizedEnvironment == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(references)*2)
	appendKey := func(key string) {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, reference := range references {
		appendKey(failurepatterns.EnvironmentRunRowKey(normalizedEnvironment, reference.RunURL, reference.RowID))
		appendKey(failurepatterns.EnvironmentRunSignatureKey(normalizedEnvironment, reference.RunURL, reference.SignatureID))
	}
	return out
}

func reviewSignalCopyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func reviewSignalSeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

// crossWindowPatternSignals generates review signals for failure patterns
// that are genuinely new within the configured history horizon or that have
// recurred after being absent from the immediately previous equal-length
// comparison window.
func crossWindowPatternSignals(
	currentPatterns []failurepatterncontracts.FailurePatternRecord,
	previousPatterns []failurepatterncontracts.FailurePatternRecord,
	historyPatterns []failurepatterncontracts.FailurePatternRecord,
) []ReviewSignalRow {
	if len(currentPatterns) == 0 {
		return nil
	}
	previousKeys := reviewSignalPatternKeySet(previousPatterns)
	historyKeys := reviewSignalPatternKeySet(historyPatterns)
	rows := make([]ReviewSignalRow, 0)
	for _, fp := range currentPatterns {
		patternKey := reviewSignalPatternKey(fp)
		if patternKey == "" {
			continue
		}
		if _, exists := previousKeys[patternKey]; exists {
			continue
		}

		env := readmodelmodel.NormalizeEnvironment(fp.Environment)
		canonical := strings.TrimSpace(fp.CanonicalEvidencePhrase)
		searchQuery := strings.TrimSpace(fp.SearchQueryPhrase)

		reason := "recurrence"
		severity := "low"
		if _, exists := historyKeys[patternKey]; !exists {
			reason = "new_pattern"
			severity = "medium"
			if fp.SupportCount >= 5 {
				severity = "high"
			}
		}

		refs := make([]ReviewSignalReference, 0, len(fp.References))
		for _, ref := range fp.References {
			refs = append(refs, ReviewSignalReference{
				RowID:          strings.TrimSpace(ref.RowID),
				RunURL:         strings.TrimSpace(ref.RunURL),
				OccurredAt:     strings.TrimSpace(ref.OccurredAt),
				SignatureID:    strings.TrimSpace(ref.SignatureID),
				PRNumber:       ref.PRNumber,
				PostGoodCommit: ref.PostGoodCommit,
			})
		}
		rows = append(rows, ReviewSignalRow{
			Environment:                          env,
			ReviewItemID:                         "crosswindow-" + strings.TrimSpace(fp.Phase2ClusterID),
			Phase:                                "crosswindow",
			Reason:                               reason,
			Severity:                             severity,
			ProposedFailurePattern:               canonical,
			ProposedSearchQuery:                  searchQuery,
			ProposedSearchQuerySourceRunURL:      strings.TrimSpace(fp.SearchQuerySourceRunURL),
			ProposedSearchQuerySourceSignatureID: strings.TrimSpace(fp.SearchQuerySourceSignatureID),
			SourcePhase1ClusterIDs:               reviewSignalCopyStrings(fp.MemberPhase1ClusterIDs),
			MemberSignatureIDs:                   reviewSignalCopyStrings(fp.MemberSignatureIDs),
			References:                           refs,
			MatchedFailurePatterns:               reviewSignalCurrentPatternMatchedFailurePatterns(fp),
		})
	}
	return rows
}

func reviewSignalPatternKeySet(
	patterns []failurepatterncontracts.FailurePatternRecord,
) map[string]struct{} {
	out := map[string]struct{}{}
	for _, pattern := range patterns {
		key := reviewSignalPatternKey(pattern)
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	return out
}

func reviewSignalPatternKey(pattern failurepatterncontracts.FailurePatternRecord) string {
	environment := readmodelmodel.NormalizeEnvironment(pattern.Environment)
	canonical := strings.TrimSpace(pattern.CanonicalEvidencePhrase)
	searchQuery := strings.TrimSpace(pattern.SearchQueryPhrase)
	if environment == "" || canonical == "" {
		return ""
	}
	return environment + "|" + canonical + "|" + searchQuery
}
