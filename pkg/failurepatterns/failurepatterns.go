package failurepatterns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	failurepatterncontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/contracts"
	failurepatternwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/window"
	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

const DefaultHistoryWeeks = 4

type RangeData struct {
	SourceFailurePatterns     []failurepatterncontracts.FailurePatternRecord
	FailurePatterns           []failurepatterncontracts.FailurePatternRecord
	ReviewItems               []failurepatterncontracts.ReviewItemRecord
	RawFailures               []storecontracts.RawFailureRecord
	TestClusterCountsByEnv    map[string]int
	ReviewItemCountsByEnv     map[string]int
	FailurePatternCountsByEnv map[string]int
	OccurrenceTotalsByEnv     map[string]int
	AvailableEnvironments     []string
}

type LoadRangeOptions struct {
	Environments       []string
	StartTime          time.Time
	EndTime            time.Time
	IncludeRawFailures bool
	IncludeReview      bool
}

type PatternKey struct {
	Environment string
	Phrase      string
	SearchQuery string
}

type PatternPresence struct {
	PriorWeeksPresent int
	PriorWeekStarts   []string
	PriorJobsAffected int
	PriorLastSeenAt   time.Time
	BadPRScore        int
	BadPRReasons      []string
}

type PresenceResolver interface {
	PresenceFor(PatternKey) PatternPresence
}

type BuildPresenceOptions struct {
	Store         storecontracts.Store
	EndTime       time.Time
	LookbackWeeks int
	Environments  []string
}

type PresenceWindow struct {
	EndTime       time.Time
	AnchorStart   time.Time
	LookbackStart time.Time
	LookbackWeeks int
}

type BuildPresenceFromFailurePatternsOptions struct {
	EndTime         time.Time
	LookbackWeeks   int
	FailurePatterns []failurepatterncontracts.FailurePatternRecord
}

type presenceResolver struct {
	byKey map[string]PatternPresence
}

type presenceAggregate struct {
	weeks               map[string]struct{}
	jobs                map[string]struct{}
	lastSeen            time.Time
	signalReferences    map[string]BadPRSignalReference
	signalPostGoodCount int
	environment         string
	phraseKey           string
}

func (r *presenceResolver) PresenceFor(key PatternKey) PatternPresence {
	if r == nil || len(r.byKey) == 0 {
		return PatternPresence{}
	}
	presence, ok := r.byKey[presenceKey(key.Environment, key.Phrase, key.SearchQuery)]
	if !ok {
		return PatternPresence{}
	}
	return presence
}

func LoadRange(
	ctx context.Context,
	store storecontracts.Store,
	opts LoadRangeOptions,
) (RangeData, error) {
	if store == nil {
		return RangeData{}, fmt.Errorf("store is required")
	}
	startTime := opts.StartTime.UTC()
	endTime := opts.EndTime.UTC()
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return RangeData{}, fmt.Errorf("valid start and end times are required")
	}

	targetEnvironments := normalizeEnvironments(opts.Environments)
	if len(targetEnvironments) == 0 {
		targetEnvironments = normalizeEnvironments(sourceoptions.SupportedEnvironments())
	}

	prepared, err := failurepatternwindow.Prepare(ctx, store, failurepatternwindow.PrepareOptions{
		Environments: targetEnvironments,
		StartTime:    startTime,
		EndTime:      endTime,
	})
	if err != nil {
		return RangeData{}, err
	}

	result, err := prepared.ResultForWindow(startTime, endTime, opts.IncludeReview)
	if err != nil {
		return RangeData{}, err
	}

	factsByEnvironment := prepared.FactsByEnvironment()
	rawFailures := []storecontracts.RawFailureRecord(nil)
	if opts.IncludeRawFailures {
		rawFailures = flattenRawFailures(factsByEnvironment, targetEnvironments)
	}

	return RangeData{
		SourceFailurePatterns:     append([]failurepatterncontracts.FailurePatternRecord(nil), result.FailurePatterns...),
		FailurePatterns:           append([]failurepatterncontracts.FailurePatternRecord(nil), result.FailurePatterns...),
		ReviewItems:               append([]failurepatterncontracts.ReviewItemRecord(nil), result.ReviewItems...),
		RawFailures:               rawFailures,
		TestClusterCountsByEnv:    countDistinctContributingTestsByEnvironment(result.FailurePatterns),
		ReviewItemCountsByEnv:     countReviewItemsByEnvironment(result.ReviewItems),
		FailurePatternCountsByEnv: countFailurePatternsByEnvironment(result.FailurePatterns),
		OccurrenceTotalsByEnv:     countOccurrencesByEnvironment(result.FailurePatterns),
		AvailableEnvironments:     availableEnvironments(targetEnvironments, factsByEnvironment, result.FailurePatterns),
	}, nil
}

func ResolveTargetEnvironments(configured []string, data RangeData) []string {
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
	anchorEnd := opts.EndTime.UTC()
	if anchorEnd.IsZero() {
		return &presenceResolver{byKey: map[string]PatternPresence{}}, nil
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	presenceWindow, err := ResolvePresenceWindow(anchorEnd, opts.LookbackWeeks)
	if err != nil {
		return nil, err
	}

	targetEnvironments := normalizeEnvironments(opts.Environments)
	if len(targetEnvironments) == 0 {
		targetEnvironments = normalizeEnvironments(sourceoptions.SupportedEnvironments())
	}

	result, err := failurepatternwindow.Compute(ctx, opts.Store, failurepatternwindow.ComputeOptions{
		Environments:  targetEnvironments,
		StartTime:     presenceWindow.LookbackStart,
		EndTime:       presenceWindow.EndTime,
		IncludeReview: false,
	})
	if err != nil {
		return nil, fmt.Errorf("compute failure-pattern history horizon: %w", err)
	}
	return BuildPresenceResolverFromFailurePatterns(BuildPresenceFromFailurePatternsOptions{
		EndTime:         presenceWindow.EndTime,
		LookbackWeeks:   presenceWindow.LookbackWeeks,
		FailurePatterns: result.FailurePatterns,
	})
}

func ResolvePresenceWindow(endTime time.Time, lookbackWeeks int) (PresenceWindow, error) {
	anchorEnd := endTime.UTC()
	if anchorEnd.IsZero() {
		return PresenceWindow{}, fmt.Errorf("end time is required")
	}
	// Preserve the current calendar-week history behavior while letting callers
	// anchor the resolver using the resolved window end time instead of passing
	// a separate week label through readmodel.
	anchorStart := weekStartForDate(anchorEnd.Add(-time.Nanosecond))
	if anchorStart.IsZero() {
		return PresenceWindow{}, fmt.Errorf("anchor week start is required")
	}
	if lookbackWeeks <= 0 {
		lookbackWeeks = DefaultHistoryWeeks
	}
	lookbackStart := anchorStart.AddDate(0, 0, -(lookbackWeeks * 7))
	if !lookbackStart.Before(anchorStart) {
		return PresenceWindow{}, fmt.Errorf("lookback start must be before anchor start")
	}
	return PresenceWindow{
		EndTime:       anchorEnd,
		AnchorStart:   anchorStart,
		LookbackStart: lookbackStart,
		LookbackWeeks: lookbackWeeks,
	}, nil
}

func BuildPresenceResolverFromFailurePatterns(
	opts BuildPresenceFromFailurePatternsOptions,
) (PresenceResolver, error) {
	anchorEnd := opts.EndTime.UTC()
	if anchorEnd.IsZero() {
		return &presenceResolver{byKey: map[string]PatternPresence{}}, nil
	}
	presenceWindow, err := ResolvePresenceWindow(anchorEnd, opts.LookbackWeeks)
	if err != nil {
		return nil, err
	}
	return buildPresenceResolverFromFailurePatterns(opts.FailurePatterns, presenceWindow), nil
}

func buildPresenceResolverFromFailurePatterns(
	rows []failurepatterncontracts.FailurePatternRecord,
	presenceWindow PresenceWindow,
) PresenceResolver {
	aggregates := map[string]*presenceAggregate{}
	phraseEnvironments := map[string]map[string]struct{}{}
	for _, row := range rows {
		filteredReferences := presenceReferencesWithinWindow(
			row.References,
			presenceWindow.LookbackStart,
			presenceWindow.EndTime,
		)
		if len(filteredReferences) == 0 {
			continue
		}
		environment := normalizeEnvironment(row.Environment)
		phraseKey := normalizePhrase(row.CanonicalEvidencePhrase)
		if environment != "" && phraseKey != "" {
			set := phraseEnvironments[phraseKey]
			if set == nil {
				set = map[string]struct{}{}
				phraseEnvironments[phraseKey] = set
			}
			set[environment] = struct{}{}
		}
		key := presenceKey(row.Environment, row.CanonicalEvidencePhrase, row.SearchQueryPhrase)
		if key == "" {
			continue
		}
		item := aggregates[key]
		if item == nil {
			item = &presenceAggregate{
				weeks:            map[string]struct{}{},
				jobs:             map[string]struct{}{},
				signalReferences: map[string]BadPRSignalReference{},
				environment:      environment,
				phraseKey:        phraseKey,
			}
			aggregates[key] = item
		}
		for _, reference := range filteredReferences {
			if refKey := normalizedRunReferenceKey(
				reference.RunURL,
				reference.SignatureID,
				reference.OccurredAt,
				reference.PRNumber,
			); refKey != "" {
				if _, exists := item.signalReferences[refKey]; !exists {
					item.signalReferences[refKey] = BadPRSignalReference{
						RunURL:      strings.TrimSpace(reference.RunURL),
						OccurredAt:  strings.TrimSpace(reference.OccurredAt),
						SignatureID: strings.TrimSpace(reference.SignatureID),
						PRNumber:    reference.PRNumber,
					}
					if reference.PostGoodCommit {
						item.signalPostGoodCount++
					}
				}
			} else if reference.PostGoodCommit {
				item.signalPostGoodCount++
			}

			referenceTime, ok := ParseReferenceTimestamp(reference.OccurredAt)
			if ok && referenceTime.Before(presenceWindow.AnchorStart) {
				weekStart := weekStartForDate(referenceTime)
				if !weekStart.IsZero() && weekStart.Before(presenceWindow.AnchorStart) && !weekStart.Before(presenceWindow.LookbackStart) {
					item.weeks[weekStart.Format("2006-01-02")] = struct{}{}
				}
				if item.lastSeen.IsZero() || referenceTime.After(item.lastSeen) {
					item.lastSeen = referenceTime
				}
			}
			jobKey := normalizedRunReferenceKey(
				reference.RunURL,
				reference.SignatureID,
				reference.OccurredAt,
				reference.PRNumber,
			)
			if jobKey != "" {
				item.jobs[jobKey] = struct{}{}
			}
		}
	}

	byKey := make(map[string]PatternPresence, len(aggregates))
	for key, item := range aggregates {
		weeks := make([]string, 0, len(item.weeks))
		for week := range item.weeks {
			weeks = append(weeks, week)
		}
		sort.Strings(weeks)
		signalReferences := make([]BadPRSignalReference, 0, len(item.signalReferences))
		for _, reference := range item.signalReferences {
			signalReferences = append(signalReferences, reference)
		}
		badPRScore, badPRReasons := BadPRScoreAndReasons(BadPRSignalEvidence{
			Environment:             item.environment,
			AfterLastPushCount:      item.signalPostGoodCount,
			SeenInOtherEnvironments: presenceSeenInOtherEnvironments(phraseEnvironments[item.phraseKey], item.environment),
			References:              signalReferences,
			PriorWeeksPresent:       len(weeks),
		})
		byKey[key] = PatternPresence{
			PriorWeeksPresent: len(weeks),
			PriorWeekStarts:   weeks,
			PriorJobsAffected: len(item.jobs),
			PriorLastSeenAt:   item.lastSeen,
			BadPRScore:        badPRScore,
			BadPRReasons:      append([]string(nil), badPRReasons...),
		}
	}
	return &presenceResolver{byKey: byKey}
}

func presenceReferencesWithinWindow(
	references []failurepatterncontracts.ReferenceRecord,
	startTime time.Time,
	endTime time.Time,
) []failurepatterncontracts.ReferenceRecord {
	if len(references) == 0 || startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return nil
	}
	filtered := make([]failurepatterncontracts.ReferenceRecord, 0, len(references))
	for _, reference := range references {
		referenceTime, ok := ParseReferenceTimestamp(reference.OccurredAt)
		if !ok || referenceTime.Before(startTime) || !referenceTime.Before(endTime) {
			continue
		}
		filtered = append(filtered, reference)
	}
	return filtered
}

func ParseReferenceTimestamp(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return ts.UTC(), true
	}
	return time.Time{}, false
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

func flattenRawFailures(
	factsByEnvironment map[string]failurepatternwindow.EnvironmentFacts,
	environments []string,
) []storecontracts.RawFailureRecord {
	if len(factsByEnvironment) == 0 {
		return nil
	}
	out := make([]storecontracts.RawFailureRecord, 0)
	for _, environment := range environments {
		facts := factsByEnvironment[environment]
		out = append(out, facts.RawFailures...)
	}
	return out
}

func countFailurePatternsByEnvironment(rows []failurepatterncontracts.FailurePatternRecord) map[string]int {
	out := map[string]int{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		out[environment]++
	}
	return out
}

func countReviewItemsByEnvironment(rows []failurepatterncontracts.ReviewItemRecord) map[string]int {
	out := map[string]int{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		out[environment]++
	}
	return out
}

func countOccurrencesByEnvironment(rows []failurepatterncontracts.FailurePatternRecord) map[string]int {
	out := map[string]int{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		out[environment] += row.SupportCount
	}
	return out
}

func countDistinctContributingTestsByEnvironment(rows []failurepatterncontracts.FailurePatternRecord) map[string]int {
	byEnvironment := map[string]map[string]struct{}{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		if byEnvironment[environment] == nil {
			byEnvironment[environment] = map[string]struct{}{}
		}
		for _, test := range row.ContributingTests {
			key := strings.TrimSpace(test.Lane) + "|" + strings.TrimSpace(test.JobName) + "|" + strings.TrimSpace(test.TestName)
			if key == "||" {
				continue
			}
			byEnvironment[environment][key] = struct{}{}
		}
	}
	out := map[string]int{}
	for environment, tests := range byEnvironment {
		out[environment] = len(tests)
	}
	return out
}

func availableEnvironments(
	requested []string,
	factsByEnvironment map[string]failurepatternwindow.EnvironmentFacts,
	rows []failurepatterncontracts.FailurePatternRecord,
) []string {
	set := map[string]struct{}{}
	for _, environment := range requested {
		facts := factsByEnvironment[environment]
		if len(facts.RunsByURL) > 0 || len(facts.RawFailures) > 0 {
			set[environment] = struct{}{}
		}
	}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
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

func weekStartForDate(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	date := time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return date.AddDate(0, 0, -int((date.Weekday()+6)%7)).UTC()
}

func presenceKey(environment string, phrase string, searchQuery string) string {
	normalizedEnvironment := normalizeEnvironment(environment)
	signatureText := normalizedSignatureText(phrase, searchQuery)
	if normalizedEnvironment == "" || signatureText == "" {
		return ""
	}
	return normalizedEnvironment + "|" + signatureText
}

func normalizedSignatureText(phrase string, searchQuery string) string {
	canonical := normalizePhrase(phrase)
	if canonical != "" {
		return canonical
	}
	return normalizePhrase(searchQuery)
}

func normalizePhrase(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func normalizedRunReferenceKey(runURL string, signatureID string, occurredAt string, prNumber int) string {
	if trimmed := strings.TrimSpace(runURL); trimmed != "" {
		return trimmed
	}
	parts := []string{
		strings.TrimSpace(signatureID),
		strings.TrimSpace(occurredAt),
		fmt.Sprintf("%d", prNumber),
	}
	key := strings.TrimSpace(strings.Join(parts, "|"))
	if key == "||0" {
		return ""
	}
	return key
}

func presenceSeenInOtherEnvironments(seenByEnvironment map[string]struct{}, currentEnvironment string) []string {
	if len(seenByEnvironment) == 0 {
		return nil
	}
	currentEnvironment = normalizeEnvironment(currentEnvironment)
	out := make([]string, 0, len(seenByEnvironment))
	for environment := range seenByEnvironment {
		normalizedEnvironment := normalizeEnvironment(environment)
		if normalizedEnvironment == "" || normalizedEnvironment == currentEnvironment {
			continue
		}
		out = append(out, normalizedEnvironment)
	}
	sort.Strings(out)
	return out
}
