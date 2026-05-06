package runlog

import (
	"sort"
	"strings"
	"time"

	failurepatterncontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/contracts"
	failurepatternwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/window"
	readmodelmodel "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/model"
	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
	readmodelwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/window"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

const signalHorizonMinWeeks = 3

type failurePatternsEnvironmentFacts = failurepatternwindow.EnvironmentFacts

type signalHorizonRefSet struct {
	byMergeKey map[string][]readmodelpatterns.FailurePatternReportReference
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

func toFailurePatternReportClusters(rows []failurepatterncontracts.FailurePatternRecord) []readmodelpatterns.FailurePatternReportCluster {
	out := make([]readmodelpatterns.FailurePatternReportCluster, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelpatterns.FailurePatternReportCluster{
			Environment:             readmodelmodel.NormalizeEnvironment(row.Environment),
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

func toFailurePatternReportContributingTests(rows []failurepatterncontracts.ContributingTestRecord) []readmodelpatterns.FailurePatternReportContributingTest {
	out := make([]readmodelpatterns.FailurePatternReportContributingTest, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelpatterns.FailurePatternReportContributingTest{
			Lane:         strings.TrimSpace(row.Lane),
			JobName:      strings.TrimSpace(row.JobName),
			TestName:     strings.TrimSpace(row.TestName),
			SupportCount: row.SupportCount,
		})
	}
	return out
}

func toFailurePatternReportReferences(rows []failurepatterncontracts.ReferenceRecord) []readmodelpatterns.FailurePatternReportReference {
	out := make([]readmodelpatterns.FailurePatternReportReference, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelpatterns.FailurePatternReportReference{
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

func availableFailurePatternEnvironments(
	targetEnvironments []string,
	result failurepatternwindow.FailurePatternWindowResult,
	clusters []readmodelpatterns.FailurePatternReportCluster,
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
	clusters []readmodelpatterns.FailurePatternReportCluster,
) signalHorizonRefSet {
	byMergeKey := map[string][]readmodelpatterns.FailurePatternReportReference{}
	for _, cluster := range clusters {
		key := failurePatternsMergeKeyForCluster(cluster)
		if key == "" {
			continue
		}
		for _, ref := range cluster.References {
			byMergeKey[key] = append(byMergeKey[key], readmodelpatterns.FailurePatternReportReference{
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

func deduplicateSignalHorizonRefs(
	refs []readmodelpatterns.FailurePatternReportReference,
) []readmodelpatterns.FailurePatternReportReference {
	seen := map[string]struct{}{}
	out := make([]readmodelpatterns.FailurePatternReportReference, 0, len(refs))
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

func failurePatternsMergeKeyForCluster(cluster readmodelpatterns.FailurePatternReportCluster) string {
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

func failurePatternsReferenceDedupKey(row readmodelpatterns.FailurePatternReportReference) string {
	rowID := strings.TrimSpace(row.RowID)
	if rowID != "" {
		return "row|" + rowID
	}
	if key := failurePatternsReferenceTupleKey(row.RunURL, row.OccurredAt, row.SignatureID); key != "" {
		return key
	}
	return ""
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

func toWindowedHTMLRunReferences(rows []readmodelpatterns.FailurePatternReportReference) []readmodelmodel.RunReference {
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

func windowedPostGoodCount(references []readmodelpatterns.FailurePatternReportReference) int {
	total := 0
	for _, reference := range references {
		if reference.PostGoodCommit {
			total++
		}
	}
	return total
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

func primaryContributingTestForReport(
	rows []readmodelpatterns.FailurePatternReportContributingTest,
) readmodelpatterns.FailurePatternReportContributingTest {
	if len(rows) == 0 {
		return readmodelpatterns.FailurePatternReportContributingTest{}
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

func sortWindowedReferences(rows []readmodelpatterns.FailurePatternReportReference) {
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
