package postgres

import (
	"fmt"
	"sort"
	"strings"
	"time"

	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEnvironmentSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
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

func normalizeDate(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("date is empty")
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return "", err
	}
	return parsed.UTC().Format("2006-01-02"), nil
}

func normalizeTimestampRange(startTime time.Time, endTime time.Time) (time.Time, time.Time, error) {
	start := startTime.UTC()
	end := endTime.UTC()
	if start.IsZero() || end.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("start and end times are required")
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time must be before end time")
	}
	return start, end, nil
}

func dateFromTimestamp(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return ts.UTC().Format("2006-01-02"), true
	}
	if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return ts.UTC().Format("2006-01-02"), true
	}
	return "", false
}

func normalizeRunRecord(row storecontracts.RunRecord) storecontracts.RunRecord {
	prNumber := row.PRNumber
	if prNumber < 0 {
		prNumber = 0
	}
	prState := strings.ToLower(strings.TrimSpace(row.PRState))
	switch prState {
	case "open", "closed":
	default:
		prState = ""
	}
	return storecontracts.RunRecord{
		Environment:    normalizeEnvironment(row.Environment),
		RunURL:         strings.TrimSpace(row.RunURL),
		JobName:        strings.TrimSpace(row.JobName),
		PRNumber:       prNumber,
		PRState:        prState,
		PRSHA:          strings.TrimSpace(row.PRSHA),
		FinalMergedSHA: strings.TrimSpace(row.FinalMergedSHA),
		MergedPR:       row.MergedPR,
		PostGoodCommit: row.PostGoodCommit,
		Failed:         row.Failed,
		OccurredAt:     strings.TrimSpace(row.OccurredAt),
	}
}

func normalizePullRequestRecord(row storecontracts.PullRequestRecord) storecontracts.PullRequestRecord {
	prNumber := row.PRNumber
	if prNumber < 0 {
		prNumber = 0
	}
	state := strings.ToLower(strings.TrimSpace(row.State))
	switch state {
	case "open", "closed":
	default:
		state = ""
	}
	merged := row.Merged
	if merged {
		state = "closed"
	}
	return storecontracts.PullRequestRecord{
		PRNumber:       prNumber,
		State:          state,
		Merged:         merged,
		HeadSHA:        strings.TrimSpace(row.HeadSHA),
		MergeCommitSHA: strings.TrimSpace(row.MergeCommitSHA),
		MergedAt:       strings.TrimSpace(row.MergedAt),
		ClosedAt:       strings.TrimSpace(row.ClosedAt),
		UpdatedAt:      strings.TrimSpace(row.UpdatedAt),
		LastCheckedAt:  strings.TrimSpace(row.LastCheckedAt),
	}
}

func normalizeArtifactFailureRecord(row storecontracts.ArtifactFailureRecord) storecontracts.ArtifactFailureRecord {
	return storecontracts.ArtifactFailureRecord{
		Environment:   normalizeEnvironment(row.Environment),
		ArtifactRowID: strings.TrimSpace(row.ArtifactRowID),
		RunURL:        strings.TrimSpace(row.RunURL),
		TestName:      strings.TrimSpace(row.TestName),
		TestSuite:     strings.TrimSpace(row.TestSuite),
		SignatureID:   strings.TrimSpace(row.SignatureID),
		FailureText:   strings.TrimSpace(row.FailureText),
	}
}

func normalizeRawFailureRecord(row storecontracts.RawFailureRecord) storecontracts.RawFailureRecord {
	return storecontracts.RawFailureRecord{
		Environment:       normalizeEnvironment(row.Environment),
		RowID:             strings.TrimSpace(row.RowID),
		RunURL:            strings.TrimSpace(row.RunURL),
		NonArtifactBacked: row.NonArtifactBacked,
		TestName:          strings.TrimSpace(row.TestName),
		TestSuite:         strings.TrimSpace(row.TestSuite),
		SignatureID:       strings.TrimSpace(row.SignatureID),
		OccurredAt:        strings.TrimSpace(row.OccurredAt),
		RawText:           strings.TrimSpace(row.RawText),
		NormalizedText:    strings.TrimSpace(row.NormalizedText),
	}
}

func normalizeMetricDailyRecord(row storecontracts.MetricDailyRecord) storecontracts.MetricDailyRecord {
	return storecontracts.MetricDailyRecord{
		Environment: normalizeEnvironment(row.Environment),
		Date:        strings.TrimSpace(row.Date),
		Metric:      strings.TrimSpace(row.Metric),
		Value:       row.Value,
	}
}

func normalizeTestMetadataPeriod(value string) string {
	period := strings.TrimSpace(value)
	if period == "" {
		return "default"
	}
	return period
}

func normalizeTestMetadataDailyRecord(row storecontracts.TestMetadataDailyRecord) storecontracts.TestMetadataDailyRecord {
	return storecontracts.TestMetadataDailyRecord{
		Environment:            normalizeEnvironment(row.Environment),
		Date:                   strings.TrimSpace(row.Date),
		Release:                strings.TrimSpace(row.Release),
		Period:                 normalizeTestMetadataPeriod(row.Period),
		TestName:               strings.TrimSpace(row.TestName),
		TestSuite:              strings.TrimSpace(row.TestSuite),
		CurrentPassPercentage:  row.CurrentPassPercentage,
		CurrentRuns:            row.CurrentRuns,
		PreviousPassPercentage: row.PreviousPassPercentage,
		PreviousRuns:           row.PreviousRuns,
		NetImprovement:         row.NetImprovement,
		IngestedAt:             strings.TrimSpace(row.IngestedAt),
	}
}

func normalizeCheckpointRecord(row storecontracts.CheckpointRecord) storecontracts.CheckpointRecord {
	return storecontracts.CheckpointRecord{
		Name:      strings.TrimSpace(row.Name),
		Value:     strings.TrimSpace(row.Value),
		UpdatedAt: strings.TrimSpace(row.UpdatedAt),
	}
}

func normalizeDeadLetterRecord(row storecontracts.DeadLetterRecord) storecontracts.DeadLetterRecord {
	return storecontracts.DeadLetterRecord{
		Controller: strings.TrimSpace(row.Controller),
		Key:        strings.TrimSpace(row.Key),
		Error:      strings.TrimSpace(row.Error),
		FailedAt:   strings.TrimSpace(row.FailedAt),
	}
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeDateSlice(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	set := map[string]struct{}{}
	for _, value := range values {
		normalized, err := normalizeDate(value)
		if err != nil {
			return nil, err
		}
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	if len(set) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func runRecordKey(row storecontracts.RunRecord) string {
	if row.Environment == "" || row.RunURL == "" {
		return ""
	}
	return row.Environment + "|" + row.RunURL
}

func artifactFailureKey(row storecontracts.ArtifactFailureRecord) string {
	if row.Environment == "" || row.ArtifactRowID == "" {
		return ""
	}
	return row.Environment + "|" + row.ArtifactRowID
}

func rawFailureKey(row storecontracts.RawFailureRecord) string {
	if row.Environment == "" || row.RowID == "" {
		return ""
	}
	return row.Environment + "|" + row.RowID
}

func metricDailyKey(row storecontracts.MetricDailyRecord) string {
	if row.Environment == "" || row.Date == "" || row.Metric == "" {
		return ""
	}
	return row.Environment + "|" + row.Date + "|" + row.Metric
}

func testMetadataDailyKey(row storecontracts.TestMetadataDailyRecord) string {
	if row.Environment == "" || row.Date == "" || row.Period == "" || row.TestName == "" {
		return ""
	}
	return row.Environment + "|" + row.Date + "|" + row.Period + "|" + row.TestSuite + "|" + row.TestName
}
