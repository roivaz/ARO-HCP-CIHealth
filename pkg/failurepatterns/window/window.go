package window

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	failurepatterncontracts "ci-failure-atlas/pkg/failurepatterns/contracts"
	failureextractor "ci-failure-atlas/pkg/failurepatterns/extractor"
	sourcelanes "ci-failure-atlas/pkg/source/lanes"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
)

type ComputeOptions struct {
	Environments  []string
	StartTime     time.Time
	EndTime       time.Time
	IncludeReview bool
	IncludeDebug  bool
}

type PrepareOptions struct {
	Environments []string
	StartTime    time.Time
	EndTime      time.Time
}

type FailurePatternWindowStageTimings struct {
	Load      time.Duration
	Extract   time.Duration
	Aggregate time.Duration
}

type FailurePatternWindowDiagnostics struct {
	StageTimings             FailurePatternWindowStageTimings
	RunsLoaded               int
	RawFailuresLoaded        int
	RowsExtracted            int
	RowsSkippedMissingRun    int
	RowsSkippedInvalid       int
	RowsSkippedNonArtifact   int
	WeakCanonicalRows        int
	RunsByEnvironment        map[string]int
	FailedRunsByEnvironment  map[string]int
	RawFailuresByEnvironment map[string]int
}

type FailurePatternWindowResult struct {
	ExtractedRows   []ExtractedFailureRow
	FailurePatterns []failurepatterncontracts.FailurePatternRecord
	ReviewItems     []failurepatterncontracts.ReviewItemRecord
	Diagnostics     FailurePatternWindowDiagnostics
}

type ExtractedFailureRow struct {
	Environment             string
	RowID                   string
	RunURL                  string
	OccurredAt              string
	SignatureID             string
	PRNumber                int
	PostGoodCommit          bool
	Lane                    string
	JobName                 string
	TestName                string
	TestSuite               string
	RawText                 string
	NormalizedText          string
	CanonicalEvidencePhrase string
	SearchQueryPhrase       string
	ProviderAnchor          string
	GenericPhrase           bool
	FailurePatternKey       string
}

type FactLoadOptions struct {
	Environments []string
	StartTime    time.Time
	EndTime      time.Time
}

type EnvironmentFacts struct {
	RawFailures []storecontracts.RawFailureRecord
	RunsByURL   map[string]storecontracts.RunRecord
	FailedRuns  int
}

type aggregateBucket struct {
	environment  string
	identitySeed string
	weak         bool
	rows         []ExtractedFailureRow
}

const maxEnvironmentLoadConcurrency = 4

type PreparedWindow struct {
	startTime          time.Time
	endTime            time.Time
	environments       []string
	factsByEnvironment map[string]EnvironmentFacts
	extractedRows      []ExtractedFailureRow
	stageTimings       FailurePatternWindowStageTimings
}

func (prepared PreparedWindow) FactsByEnvironment() map[string]EnvironmentFacts {
	return cloneFactsByEnvironment(prepared.factsByEnvironment)
}

func Compute(
	ctx context.Context,
	store storecontracts.Store,
	opts ComputeOptions,
) (FailurePatternWindowResult, error) {
	prepared, err := Prepare(ctx, store, PrepareOptions{
		Environments: opts.Environments,
		StartTime:    opts.StartTime,
		EndTime:      opts.EndTime,
	})
	if err != nil {
		return FailurePatternWindowResult{}, err
	}
	return prepared.ResultForWindow(opts.StartTime, opts.EndTime, opts.IncludeReview)
}

func Prepare(
	ctx context.Context,
	store storecontracts.Store,
	opts PrepareOptions,
) (PreparedWindow, error) {
	if store == nil {
		return PreparedWindow{}, fmt.Errorf("store is required")
	}
	normalizedEnvironments := normalizeEnvironmentSlice(opts.Environments)
	startTime := opts.StartTime.UTC()
	endTime := opts.EndTime.UTC()
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return PreparedWindow{}, fmt.Errorf("valid start and end times are required")
	}

	prepared := PreparedWindow{
		startTime:          startTime,
		endTime:            endTime,
		environments:       normalizedEnvironments,
		factsByEnvironment: map[string]EnvironmentFacts{},
	}

	loadStarted := time.Now()
	factsByEnvironment, err := LoadDateScopedFacts(ctx, store, FactLoadOptions{
		Environments: normalizedEnvironments,
		StartTime:    startTime,
		EndTime:      endTime,
	})
	if err != nil {
		return PreparedWindow{}, err
	}
	prepared.factsByEnvironment = factsByEnvironment
	prepared.stageTimings.Load = time.Since(loadStarted)

	extractStarted := time.Now()
	prepared.extractedRows = buildExtractedFailureRows(factsByEnvironment, nil)
	prepared.stageTimings.Extract = time.Since(extractStarted)

	return prepared, nil
}

func cloneFactsByEnvironment(source map[string]EnvironmentFacts) map[string]EnvironmentFacts {
	if len(source) == 0 {
		return map[string]EnvironmentFacts{}
	}
	out := make(map[string]EnvironmentFacts, len(source))
	for environment, facts := range source {
		out[environment] = cloneEnvironmentFacts(facts)
	}
	return out
}

func cloneEnvironmentFacts(source EnvironmentFacts) EnvironmentFacts {
	out := EnvironmentFacts{
		RawFailures: append([]storecontracts.RawFailureRecord(nil), source.RawFailures...),
		RunsByURL:   make(map[string]storecontracts.RunRecord, len(source.RunsByURL)),
		FailedRuns:  source.FailedRuns,
	}
	for runURL, run := range source.RunsByURL {
		out.RunsByURL[runURL] = run
	}
	return out
}

func (prepared PreparedWindow) ResultForWindow(
	startTime time.Time,
	endTime time.Time,
	includeReview bool,
) (FailurePatternWindowResult, error) {
	requestStart := startTime.UTC()
	requestEnd := endTime.UTC()
	if requestStart.IsZero() || requestEnd.IsZero() || !requestStart.Before(requestEnd) {
		return FailurePatternWindowResult{}, fmt.Errorf("valid start and end times are required")
	}
	if prepared.startTime.IsZero() || prepared.endTime.IsZero() || !prepared.startTime.Before(prepared.endTime) {
		return FailurePatternWindowResult{}, fmt.Errorf("prepared window is invalid")
	}
	if requestStart.Before(prepared.startTime) || requestEnd.After(prepared.endTime) {
		return FailurePatternWindowResult{}, fmt.Errorf(
			"requested window %s..%s must be within prepared window %s..%s",
			requestStart.Format(time.RFC3339),
			requestEnd.Format(time.RFC3339),
			prepared.startTime.Format(time.RFC3339),
			prepared.endTime.Format(time.RFC3339),
		)
	}

	result := FailurePatternWindowResult{
		Diagnostics: newFailurePatternWindowDiagnostics(prepared.environments),
	}
	filteredFacts := sliceFactsByWindow(prepared.factsByEnvironment, prepared.environments, requestStart, requestEnd)
	collectFactDiagnostics(filteredFacts, prepared.environments, &result.Diagnostics)
	collectSkippedRowDiagnostics(filteredFacts, &result.Diagnostics)
	result.ExtractedRows = sliceExtractedRowsByWindow(prepared.extractedRows, requestStart, requestEnd)
	result.Diagnostics.RowsExtracted = len(result.ExtractedRows)
	result.Diagnostics.StageTimings.Load = prepared.stageTimings.Load
	result.Diagnostics.StageTimings.Extract = prepared.stageTimings.Extract

	aggregateStarted := time.Now()
	result.FailurePatterns, result.ReviewItems = aggregateExtractedRows(
		result.ExtractedRows,
		includeReview,
		&result.Diagnostics,
	)
	result.Diagnostics.StageTimings.Aggregate = time.Since(aggregateStarted)

	return result, nil
}

func LoadDateScopedFacts(
	ctx context.Context,
	store storecontracts.Store,
	opts FactLoadOptions,
) (map[string]EnvironmentFacts, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if opts.StartTime.IsZero() || opts.EndTime.IsZero() || !opts.StartTime.Before(opts.EndTime) {
		return nil, fmt.Errorf("valid start and end times are required")
	}

	environments := normalizeEnvironmentSlice(opts.Environments)
	factsByEnvironment := make(map[string]EnvironmentFacts, len(environments))
	if len(environments) == 0 {
		return factsByEnvironment, nil
	}

	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerLimit := maxEnvironmentLoadConcurrency
	if len(environments) < workerLimit {
		workerLimit = len(environments)
	}
	semaphore := make(chan struct{}, workerLimit)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for _, environment := range environments {
		environment := environment
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-loadCtx.Done():
				return
			}
			defer func() {
				<-semaphore
			}()

			facts, err := loadEnvironmentFacts(loadCtx, store, environment, opts.StartTime, opts.EndTime)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			if firstErr != nil {
				return
			}
			factsByEnvironment[environment] = facts
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	return factsByEnvironment, nil
}

func loadEnvironmentFacts(
	ctx context.Context,
	store storecontracts.Store,
	environment string,
	startTime time.Time,
	endTime time.Time,
) (EnvironmentFacts, error) {
	facts := EnvironmentFacts{
		RawFailures: []storecontracts.RawFailureRecord{},
		RunsByURL:   map[string]storecontracts.RunRecord{},
	}
	runs, err := store.ListRunsByDateRange(ctx, environment, startTime, endTime)
	if err != nil {
		return EnvironmentFacts{}, fmt.Errorf(
			"list runs for %s in %s..%s: %w",
			environment,
			startTime.Format(time.RFC3339),
			endTime.Format(time.RFC3339),
			err,
		)
	}
	for _, run := range runs {
		normalizedRun := normalizeRunRecord(run)
		if normalizedRun.Environment == "" {
			normalizedRun.Environment = environment
		}
		if normalizedRun.RunURL == "" {
			continue
		}
		facts.RunsByURL[normalizedRun.RunURL] = normalizedRun
	}

	rawFailures, err := store.ListRawFailuresByDateRange(ctx, environment, startTime, endTime)
	if err != nil {
		return EnvironmentFacts{}, fmt.Errorf(
			"list raw failures for %s in %s..%s: %w",
			environment,
			startTime.Format(time.RFC3339),
			endTime.Format(time.RFC3339),
			err,
		)
	}
	for _, row := range rawFailures {
		facts.RawFailures = append(facts.RawFailures, normalizeRawFailureRecord(row, environment, ""))
	}
	if err := fillMissingRuns(ctx, store, environment, &facts); err != nil {
		return EnvironmentFacts{}, err
	}
	for _, run := range facts.RunsByURL {
		if run.Failed {
			facts.FailedRuns++
		}
	}
	sortWindowedRawFailures(facts.RawFailures)
	return facts, nil
}

func newFailurePatternWindowDiagnostics(environments []string) FailurePatternWindowDiagnostics {
	diagnostics := FailurePatternWindowDiagnostics{
		RunsByEnvironment:        map[string]int{},
		FailedRunsByEnvironment:  map[string]int{},
		RawFailuresByEnvironment: map[string]int{},
	}
	for _, environment := range environments {
		diagnostics.RunsByEnvironment[environment] = 0
		diagnostics.FailedRunsByEnvironment[environment] = 0
		diagnostics.RawFailuresByEnvironment[environment] = 0
	}
	return diagnostics
}

func sliceFactsByWindow(
	factsByEnvironment map[string]EnvironmentFacts,
	environments []string,
	startTime time.Time,
	endTime time.Time,
) map[string]EnvironmentFacts {
	filteredByEnvironment := make(map[string]EnvironmentFacts, len(environments))
	for _, environment := range environments {
		sourceFacts := factsByEnvironment[environment]
		filteredFacts := EnvironmentFacts{
			RawFailures: []storecontracts.RawFailureRecord{},
			RunsByURL:   map[string]storecontracts.RunRecord{},
		}
		for runURL, run := range sourceFacts.RunsByURL {
			if !timestampWithinWindow(run.OccurredAt, startTime, endTime) {
				continue
			}
			filteredFacts.RunsByURL[runURL] = run
		}
		for _, row := range sourceFacts.RawFailures {
			runURL := strings.TrimSpace(row.RunURL)
			run, runFound := sourceFacts.RunsByURL[runURL]
			occurredAt := strings.TrimSpace(row.OccurredAt)
			if occurredAt == "" && runFound {
				occurredAt = strings.TrimSpace(run.OccurredAt)
			}
			if !timestampWithinWindow(occurredAt, startTime, endTime) {
				continue
			}
			filteredFacts.RawFailures = append(filteredFacts.RawFailures, row)
			if runURL != "" && runFound {
				filteredFacts.RunsByURL[runURL] = run
			}
		}
		for _, run := range filteredFacts.RunsByURL {
			if run.Failed {
				filteredFacts.FailedRuns++
			}
		}
		sortWindowedRawFailures(filteredFacts.RawFailures)
		filteredByEnvironment[environment] = filteredFacts
	}
	return filteredByEnvironment
}

func sliceExtractedRowsByWindow(
	rows []ExtractedFailureRow,
	startTime time.Time,
	endTime time.Time,
) []ExtractedFailureRow {
	if len(rows) == 0 {
		return nil
	}
	filtered := make([]ExtractedFailureRow, 0, len(rows))
	for _, row := range rows {
		if !timestampWithinWindow(row.OccurredAt, startTime, endTime) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func collectFactDiagnostics(
	factsByEnvironment map[string]EnvironmentFacts,
	environments []string,
	diagnostics *FailurePatternWindowDiagnostics,
) {
	if diagnostics == nil {
		return
	}
	for _, environment := range environments {
		facts := factsByEnvironment[environment]
		diagnostics.RunsLoaded += len(facts.RunsByURL)
		diagnostics.RawFailuresLoaded += len(facts.RawFailures)
		diagnostics.RunsByEnvironment[environment] = len(facts.RunsByURL)
		diagnostics.FailedRunsByEnvironment[environment] = facts.FailedRuns
		diagnostics.RawFailuresByEnvironment[environment] = len(facts.RawFailures)
	}
}

func collectSkippedRowDiagnostics(
	factsByEnvironment map[string]EnvironmentFacts,
	diagnostics *FailurePatternWindowDiagnostics,
) {
	if diagnostics == nil {
		return
	}
	for _, facts := range factsByEnvironment {
		for _, rawFailure := range facts.RawFailures {
			if rawFailure.NonArtifactBacked {
				diagnostics.RowsSkippedNonArtifact++
				continue
			}

			runURL := strings.TrimSpace(rawFailure.RunURL)
			run, found := facts.RunsByURL[runURL]
			if !found {
				diagnostics.RowsSkippedMissingRun++
				continue
			}

			issues := validateExtractedFailureRow(rawFailure, run, found)
			if len(issues) > 0 {
				diagnostics.RowsSkippedInvalid++
			}
		}
	}
}

func buildExtractedFailureRows(
	factsByEnvironment map[string]EnvironmentFacts,
	diagnostics *FailurePatternWindowDiagnostics,
) []ExtractedFailureRow {
	environments := make([]string, 0, len(factsByEnvironment))
	for environment := range factsByEnvironment {
		environments = append(environments, environment)
	}
	sort.Strings(environments)

	rows := make([]ExtractedFailureRow, 0)
	for _, environment := range environments {
		facts := factsByEnvironment[environment]
		for _, rawFailure := range facts.RawFailures {
			if rawFailure.NonArtifactBacked {
				if diagnostics != nil {
					diagnostics.RowsSkippedNonArtifact++
				}
				continue
			}

			runURL := strings.TrimSpace(rawFailure.RunURL)
			run, found := facts.RunsByURL[runURL]
			if !found {
				if diagnostics != nil {
					diagnostics.RowsSkippedMissingRun++
				}
				continue
			}

			issues := validateExtractedFailureRow(rawFailure, run, found)
			if len(issues) > 0 {
				if diagnostics != nil {
					diagnostics.RowsSkippedInvalid++
				}
				continue
			}

			occurredAt := strings.TrimSpace(rawFailure.OccurredAt)
			if occurredAt == "" {
				occurredAt = strings.TrimSpace(run.OccurredAt)
			}

			evidence := failureextractor.ExtractWithOptions(rawFailure.RawText, failureextractor.ExtractOptions{
				TestName: rawFailure.TestName,
			})
			rows = append(rows, ExtractedFailureRow{
				Environment:             environment,
				RowID:                   strings.TrimSpace(rawFailure.RowID),
				RunURL:                  runURL,
				OccurredAt:              occurredAt,
				SignatureID:             strings.TrimSpace(rawFailure.SignatureID),
				PRNumber:                run.PRNumber,
				PostGoodCommit:          run.PostGoodCommit,
				Lane:                    string(sourcelanes.ClassifyLane(environment, rawFailure.TestSuite, rawFailure.TestName)),
				JobName:                 strings.TrimSpace(run.JobName),
				TestName:                strings.TrimSpace(rawFailure.TestName),
				TestSuite:               strings.TrimSpace(rawFailure.TestSuite),
				RawText:                 strings.TrimSpace(rawFailure.RawText),
				NormalizedText:          strings.TrimSpace(rawFailure.NormalizedText),
				CanonicalEvidencePhrase: strings.TrimSpace(evidence.CanonicalEvidencePhrase),
				SearchQueryPhrase:       strings.TrimSpace(evidence.SearchQueryPhrase),
				ProviderAnchor:          strings.TrimSpace(evidence.ProviderAnchor),
				GenericPhrase:           evidence.GenericPhrase,
				FailurePatternKey:       strings.TrimSpace(failureextractor.FailurePatternKey(evidence)),
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Environment != rows[j].Environment {
			return rows[i].Environment < rows[j].Environment
		}
		if rows[i].Lane != rows[j].Lane {
			return rows[i].Lane < rows[j].Lane
		}
		if rows[i].JobName != rows[j].JobName {
			return rows[i].JobName < rows[j].JobName
		}
		if rows[i].TestName != rows[j].TestName {
			return rows[i].TestName < rows[j].TestName
		}
		if rows[i].OccurredAt != rows[j].OccurredAt {
			return rows[i].OccurredAt < rows[j].OccurredAt
		}
		if rows[i].RunURL != rows[j].RunURL {
			return rows[i].RunURL < rows[j].RunURL
		}
		if rows[i].SignatureID != rows[j].SignatureID {
			return rows[i].SignatureID < rows[j].SignatureID
		}
		return rows[i].RowID < rows[j].RowID
	})

	if diagnostics != nil {
		diagnostics.RowsExtracted = len(rows)
	}
	return rows
}

func aggregateExtractedRows(
	rows []ExtractedFailureRow,
	includeReview bool,
	diagnostics *FailurePatternWindowDiagnostics,
) ([]failurepatterncontracts.FailurePatternRecord, []failurepatterncontracts.ReviewItemRecord) {
	if len(rows) == 0 {
		return nil, nil
	}

	buckets := map[string]*aggregateBucket{}
	for _, row := range rows {
		environment := normalizeEnvironment(row.Environment)
		if environment == "" {
			environment = "unknown"
		}

		weak := isWeakCanonical(row)
		identitySeed := strings.TrimSpace(row.FailurePatternKey)
		if diagnostics != nil && weak {
			diagnostics.WeakCanonicalRows++
		}
		if identitySeed == "" {
			identitySeed = singletonIdentitySeed(row)
		}

		bucketKey := environment + "|" + identitySeed
		bucket := buckets[bucketKey]
		if bucket == nil {
			bucket = &aggregateBucket{
				environment:  environment,
				identitySeed: identitySeed,
				weak:         weak,
				rows:         []ExtractedFailureRow{},
			}
			buckets[bucketKey] = bucket
		}
		bucket.weak = bucket.weak || weak
		bucket.rows = append(bucket.rows, row)
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	failurePatterns := make([]failurepatterncontracts.FailurePatternRecord, 0, len(keys))
	reviewItems := make([]failurepatterncontracts.ReviewItemRecord, 0)
	for _, key := range keys {
		bucket := buckets[key]
		pattern := compileFailurePattern(bucket)
		failurePatterns = append(failurePatterns, pattern)
		if !includeReview {
			continue
		}
		if bucket.weak {
			reviewItems = append(reviewItems, buildWeakCanonicalReviewItem(bucket, pattern))
			continue
		}
		if reviewItem, ok := buildAmbiguousProviderReviewItem(bucket, pattern); ok {
			reviewItems = append(reviewItems, reviewItem)
		}
	}

	sortFailurePatterns(failurePatterns)
	sortReviewItems(reviewItems)
	return failurePatterns, reviewItems
}

func compileFailurePattern(bucket *aggregateBucket) failurepatterncontracts.FailurePatternRecord {
	memberRows := append([]ExtractedFailureRow(nil), bucket.rows...)
	sortExtractedRowsForAggregation(memberRows)

	canonicalCounts := map[string]int{}
	searchCounts := map[string]int{}
	signatureSet := map[string]struct{}{}
	referencesByKey := map[string]failurepatterncontracts.ReferenceRecord{}
	contributingTests := map[string]failurepatterncontracts.ContributingTestRecord{}
	postGoodCommitCount := 0

	for _, row := range memberRows {
		if row.PostGoodCommit {
			postGoodCommitCount++
		}
		if signatureID := strings.TrimSpace(row.SignatureID); signatureID != "" {
			signatureSet[signatureID] = struct{}{}
		}
		reference := normalizeReference(failurepatterncontracts.ReferenceRecord{
			RowID:          strings.TrimSpace(row.RowID),
			RunURL:         strings.TrimSpace(row.RunURL),
			OccurredAt:     strings.TrimSpace(row.OccurredAt),
			SignatureID:    strings.TrimSpace(row.SignatureID),
			PRNumber:       row.PRNumber,
			PostGoodCommit: row.PostGoodCommit,
		})
		if key := referenceKey(reference); key != "" {
			referencesByKey[key] = reference
		}
		if candidate := strings.TrimSpace(row.CanonicalEvidencePhrase); candidate != "" {
			canonicalCounts[candidate]++
		}
		if candidate := strings.TrimSpace(row.SearchQueryPhrase); candidate != "" {
			searchCounts[candidate]++
		}
		testKey := strings.TrimSpace(row.Lane) + "|" + strings.TrimSpace(row.JobName) + "|" + strings.TrimSpace(row.TestName)
		contributing := contributingTests[testKey]
		contributing.Lane = strings.TrimSpace(row.Lane)
		contributing.JobName = strings.TrimSpace(row.JobName)
		contributing.TestName = strings.TrimSpace(row.TestName)
		contributing.SupportCount++
		contributingTests[testKey] = contributing
	}

	references := make([]failurepatterncontracts.ReferenceRecord, 0, len(referencesByKey))
	for _, reference := range referencesByKey {
		references = append(references, reference)
	}
	sortReferences(references)

	contributingList := make([]failurepatterncontracts.ContributingTestRecord, 0, len(contributingTests))
	for _, row := range contributingTests {
		contributingList = append(contributingList, row)
	}
	sort.Slice(contributingList, func(i, j int) bool {
		if contributingList[i].Lane != contributingList[j].Lane {
			return contributingList[i].Lane < contributingList[j].Lane
		}
		if contributingList[i].JobName != contributingList[j].JobName {
			return contributingList[i].JobName < contributingList[j].JobName
		}
		return contributingList[i].TestName < contributingList[j].TestName
	})

	canonical := pickMostCommonPhrase(canonicalCounts)
	representative := representativeExtractedFailureRow(memberRows, canonical)
	if canonical == "" {
		canonical = displayFallbackPhrase(representative)
	}

	searchPhrase := pickMostCommonPhrase(searchCounts)
	searchSourceRunURL, searchSourceSignatureID := searchSourceForPhrase(memberRows, searchPhrase)
	if searchPhrase == "" {
		searchPhrase = strings.TrimSpace(representative.SearchQueryPhrase)
		searchSourceRunURL = strings.TrimSpace(representative.RunURL)
		searchSourceSignatureID = strings.TrimSpace(representative.SignatureID)
	}
	if searchPhrase == "" {
		searchPhrase = strings.TrimSpace(canonical)
		searchSourceRunURL = strings.TrimSpace(representative.RunURL)
		searchSourceSignatureID = strings.TrimSpace(representative.SignatureID)
	}
	if len(references) > 0 && (searchSourceRunURL == "" || searchSourceSignatureID == "") {
		searchSourceRunURL = strings.TrimSpace(references[0].RunURL)
		searchSourceSignatureID = strings.TrimSpace(references[0].SignatureID)
	}

	return failurepatterncontracts.FailurePatternRecord{
		SchemaVersion:                failurepatterncontracts.CurrentSchemaVersion,
		Environment:                  bucket.environment,
		Phase2ClusterID:              fingerprint(bucket.environment + "|phase2|" + bucket.identitySeed),
		CanonicalEvidencePhrase:      strings.TrimSpace(canonical),
		SearchQueryPhrase:            strings.TrimSpace(searchPhrase),
		SearchQuerySourceRunURL:      strings.TrimSpace(searchSourceRunURL),
		SearchQuerySourceSignatureID: strings.TrimSpace(searchSourceSignatureID),
		SupportCount:                 len(memberRows),
		SeenPostGoodCommit:           postGoodCommitCount > 0,
		PostGoodCommitCount:          postGoodCommitCount,
		ContributingTestsCount:       len(contributingList),
		ContributingTests:            contributingList,
		MemberSignatureIDs:           sortedKeys(signatureSet),
		References:                   references,
	}
}

func buildWeakCanonicalReviewItem(
	bucket *aggregateBucket,
	pattern failurepatterncontracts.FailurePatternRecord,
) failurepatterncontracts.ReviewItemRecord {
	firstRow := ExtractedFailureRow{}
	if len(bucket.rows) > 0 {
		firstRow = bucket.rows[0]
	}
	return failurepatterncontracts.ReviewItemRecord{
		SchemaVersion:                        failurepatterncontracts.CurrentSchemaVersion,
		Environment:                          bucket.environment,
		ReviewItemID:                         fingerprint(bucket.environment + "|phase2|weak_canonical_needs_review|" + bucket.identitySeed),
		Phase:                                "phase2",
		Reason:                               "weak_canonical_needs_review",
		Severity:                             reviewSeverity(pattern.SupportCount),
		ProposedCanonicalEvidencePhrase:      strings.TrimSpace(pattern.CanonicalEvidencePhrase),
		ProposedSearchQueryPhrase:            strings.TrimSpace(pattern.SearchQueryPhrase),
		ProposedSearchQuerySourceRunURL:      strings.TrimSpace(firstRow.RunURL),
		ProposedSearchQuerySourceSignatureID: strings.TrimSpace(firstRow.SignatureID),
		MemberSignatureIDs:                   append([]string(nil), pattern.MemberSignatureIDs...),
		References:                           append([]failurepatterncontracts.ReferenceRecord(nil), pattern.References...),
	}
}

func buildAmbiguousProviderReviewItem(
	bucket *aggregateBucket,
	pattern failurepatterncontracts.FailurePatternRecord,
) (failurepatterncontracts.ReviewItemRecord, bool) {
	providerSet := map[string]struct{}{}
	for _, row := range bucket.rows {
		provider := strings.TrimSpace(row.ProviderAnchor)
		if provider == "" {
			continue
		}
		providerSet[provider] = struct{}{}
	}
	if len(providerSet) <= 1 {
		return failurepatterncontracts.ReviewItemRecord{}, false
	}
	return failurepatterncontracts.ReviewItemRecord{
		SchemaVersion:                        failurepatterncontracts.CurrentSchemaVersion,
		Environment:                          bucket.environment,
		ReviewItemID:                         fingerprint(bucket.environment + "|phase2|ambiguous_provider_anchor|" + bucket.identitySeed),
		Phase:                                "phase2",
		Reason:                               "ambiguous_provider_anchor",
		Severity:                             reviewSeverity(pattern.SupportCount),
		ProposedCanonicalEvidencePhrase:      strings.TrimSpace(pattern.CanonicalEvidencePhrase),
		ProposedSearchQueryPhrase:            strings.TrimSpace(pattern.SearchQueryPhrase),
		ProposedSearchQuerySourceRunURL:      strings.TrimSpace(pattern.SearchQuerySourceRunURL),
		ProposedSearchQuerySourceSignatureID: strings.TrimSpace(pattern.SearchQuerySourceSignatureID),
		MemberSignatureIDs:                   append([]string(nil), pattern.MemberSignatureIDs...),
		References:                           append([]failurepatterncontracts.ReferenceRecord(nil), pattern.References...),
	}, true
}

func isWeakCanonical(row ExtractedFailureRow) bool {
	if strings.TrimSpace(row.FailurePatternKey) == "" {
		return true
	}
	canonical := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(row.CanonicalEvidencePhrase)), " "))
	if isKnownTerminalCanonical(canonical) {
		return false
	}
	switch {
	case canonical == "":
		return true
	case canonical == "failure":
		return true
	case canonical == "failure occurred":
		return true
	case canonical == "cluster provisioning failed":
		return true
	case canonical == "context deadline exceeded":
		return true
	case canonical == "msg:":
		return true
	case canonical == "err:":
		return true
	case canonical == "caused by:":
		return true
	case strings.HasPrefix(canonical, "unexpected error"):
		return true
	default:
		return false
	}
}

func isKnownTerminalCanonical(canonical string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(canonical)), " ")) {
	case "interrupted by user", "command error: signal: killed":
		return true
	default:
		return false
	}
}

func singletonIdentitySeed(row ExtractedFailureRow) string {
	rowID := strings.TrimSpace(row.RowID)
	if rowID != "" {
		return "row|" + rowID
	}
	return "ref|" + strings.TrimSpace(row.RunURL) + "|" + strings.TrimSpace(row.OccurredAt) + "|" + strings.TrimSpace(row.SignatureID)
}

func representativeExtractedFailureRow(rows []ExtractedFailureRow, preferredCanonical string) ExtractedFailureRow {
	if len(rows) == 0 {
		return ExtractedFailureRow{}
	}
	sorted := append([]ExtractedFailureRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool {
		leftCanonicalPreferred := strings.TrimSpace(sorted[i].CanonicalEvidencePhrase) == strings.TrimSpace(preferredCanonical)
		rightCanonicalPreferred := strings.TrimSpace(sorted[j].CanonicalEvidencePhrase) == strings.TrimSpace(preferredCanonical)
		if leftCanonicalPreferred != rightCanonicalPreferred {
			return leftCanonicalPreferred
		}
		if sorted[i].GenericPhrase != sorted[j].GenericPhrase {
			return !sorted[i].GenericPhrase
		}
		if strings.TrimSpace(sorted[i].SearchQueryPhrase) != strings.TrimSpace(sorted[j].SearchQueryPhrase) {
			return strings.TrimSpace(sorted[i].SearchQueryPhrase) > strings.TrimSpace(sorted[j].SearchQueryPhrase)
		}
		if sorted[i].OccurredAt != sorted[j].OccurredAt {
			return sorted[i].OccurredAt < sorted[j].OccurredAt
		}
		if sorted[i].RunURL != sorted[j].RunURL {
			return sorted[i].RunURL < sorted[j].RunURL
		}
		return sorted[i].RowID < sorted[j].RowID
	})
	return sorted[0]
}

func searchSourceForPhrase(rows []ExtractedFailureRow, phrase string) (string, string) {
	trimmedPhrase := strings.TrimSpace(phrase)
	if trimmedPhrase == "" {
		return "", ""
	}
	sorted := append([]ExtractedFailureRow(nil), rows...)
	sortExtractedRowsForAggregation(sorted)
	for _, row := range sorted {
		if strings.TrimSpace(row.SearchQueryPhrase) != trimmedPhrase {
			continue
		}
		return strings.TrimSpace(row.RunURL), strings.TrimSpace(row.SignatureID)
	}
	return "", ""
}

func displayFallbackPhrase(row ExtractedFailureRow) string {
	if candidate := strings.TrimSpace(row.CanonicalEvidencePhrase); candidate != "" {
		return candidate
	}
	return compactPhrase(sampleFailureText(row.RawText, row.NormalizedText), 220)
}

func compactPhrase(value string, limit int) string {
	collapsed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len(collapsed) <= limit {
		return collapsed
	}
	return strings.TrimSpace(collapsed[:limit])
}

func sampleFailureText(rawText string, normalizedText string) string {
	if trimmed := strings.TrimSpace(rawText); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(normalizedText)
}

func pickMostCommonPhrase(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	type candidate struct {
		phrase string
		count  int
	}
	candidates := make([]candidate, 0, len(counts))
	for phrase, count := range counts {
		candidates = append(candidates, candidate{
			phrase: strings.TrimSpace(phrase),
			count:  count,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].count != candidates[j].count {
			return candidates[i].count > candidates[j].count
		}
		return candidates[i].phrase < candidates[j].phrase
	})
	return strings.TrimSpace(candidates[0].phrase)
}

func validateExtractedFailureRow(
	row storecontracts.RawFailureRecord,
	run storecontracts.RunRecord,
	runFound bool,
) []string {
	issues := make([]string, 0, 6)
	if !runFound {
		return append(issues, "missing_run_metadata")
	}
	if strings.TrimSpace(run.JobName) == "" {
		issues = append(issues, "missing_job_name")
	}
	occurredAt := strings.TrimSpace(row.OccurredAt)
	if occurredAt == "" {
		occurredAt = strings.TrimSpace(run.OccurredAt)
	}
	if occurredAt == "" {
		issues = append(issues, "missing_occurred_at")
	}
	if strings.TrimSpace(row.RowID) == "" {
		issues = append(issues, "missing_row_id")
	}
	if strings.TrimSpace(row.SignatureID) == "" {
		issues = append(issues, "missing_signature_id")
	}
	if strings.TrimSpace(row.RawText) == "" {
		issues = append(issues, "missing_raw_text")
	}
	if strings.TrimSpace(row.NormalizedText) == "" {
		issues = append(issues, "missing_normalized_text")
	}
	return issues
}

func fillMissingRuns(
	ctx context.Context,
	store storecontracts.Store,
	environment string,
	facts *EnvironmentFacts,
) error {
	if facts == nil {
		return nil
	}
	for _, row := range facts.RawFailures {
		runURL := strings.TrimSpace(row.RunURL)
		if runURL == "" {
			continue
		}
		if _, exists := facts.RunsByURL[runURL]; exists {
			continue
		}
		run, found, err := store.GetRun(ctx, environment, runURL)
		if err != nil {
			return fmt.Errorf("get run %s for %s: %w", runURL, environment, err)
		}
		if !found {
			continue
		}
		normalizedRun := normalizeRunRecord(run)
		if normalizedRun.Environment == "" {
			normalizedRun.Environment = environment
		}
		facts.RunsByURL[runURL] = normalizedRun
	}
	return nil
}

func normalizeRunRecord(run storecontracts.RunRecord) storecontracts.RunRecord {
	return storecontracts.RunRecord{
		Environment:    normalizeEnvironment(run.Environment),
		RunURL:         strings.TrimSpace(run.RunURL),
		JobName:        strings.TrimSpace(run.JobName),
		PRNumber:       run.PRNumber,
		PRState:        strings.TrimSpace(run.PRState),
		PRSHA:          strings.TrimSpace(run.PRSHA),
		FinalMergedSHA: strings.TrimSpace(run.FinalMergedSHA),
		MergedPR:       run.MergedPR,
		PostGoodCommit: run.PostGoodCommit,
		Failed:         run.Failed,
		OccurredAt:     strings.TrimSpace(run.OccurredAt),
	}
}

func normalizeRawFailureRecord(
	row storecontracts.RawFailureRecord,
	fallbackEnvironment string,
	fallbackRunURL string,
) storecontracts.RawFailureRecord {
	environment := normalizeEnvironment(row.Environment)
	if environment == "" {
		environment = normalizeEnvironment(fallbackEnvironment)
	}
	runURL := strings.TrimSpace(row.RunURL)
	if runURL == "" {
		runURL = strings.TrimSpace(fallbackRunURL)
	}
	return storecontracts.RawFailureRecord{
		Environment:       environment,
		RowID:             strings.TrimSpace(row.RowID),
		RunURL:            runURL,
		NonArtifactBacked: row.NonArtifactBacked,
		TestName:          strings.TrimSpace(row.TestName),
		TestSuite:         strings.TrimSpace(row.TestSuite),
		SignatureID:       strings.TrimSpace(row.SignatureID),
		OccurredAt:        strings.TrimSpace(row.OccurredAt),
		RawText:           strings.TrimSpace(row.RawText),
		NormalizedText:    strings.TrimSpace(row.NormalizedText),
	}
}

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEnvironmentSlice(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeEnvironment(value)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return sortedKeys(set)
}

func sortExtractedRowsForAggregation(rows []ExtractedFailureRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OccurredAt != rows[j].OccurredAt {
			return rows[i].OccurredAt < rows[j].OccurredAt
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

func normalizeReference(row failurepatterncontracts.ReferenceRecord) failurepatterncontracts.ReferenceRecord {
	return failurepatterncontracts.ReferenceRecord{
		RowID:          strings.TrimSpace(row.RowID),
		RunURL:         strings.TrimSpace(row.RunURL),
		OccurredAt:     strings.TrimSpace(row.OccurredAt),
		SignatureID:    strings.TrimSpace(row.SignatureID),
		PRNumber:       row.PRNumber,
		PostGoodCommit: row.PostGoodCommit,
	}
}

func referenceKey(row failurepatterncontracts.ReferenceRecord) string {
	if strings.TrimSpace(row.RowID) != "" {
		return "row|" + strings.TrimSpace(row.RowID)
	}
	if strings.TrimSpace(row.RunURL) == "" && strings.TrimSpace(row.OccurredAt) == "" && strings.TrimSpace(row.SignatureID) == "" {
		return ""
	}
	return "ref|" + strings.TrimSpace(row.RunURL) + "|" + strings.TrimSpace(row.OccurredAt) + "|" + strings.TrimSpace(row.SignatureID)
}

func sortReferences(rows []failurepatterncontracts.ReferenceRecord) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OccurredAt != rows[j].OccurredAt {
			return rows[i].OccurredAt < rows[j].OccurredAt
		}
		if rows[i].RunURL != rows[j].RunURL {
			return rows[i].RunURL < rows[j].RunURL
		}
		if rows[i].RowID != rows[j].RowID {
			return rows[i].RowID < rows[j].RowID
		}
		return rows[i].SignatureID < rows[j].SignatureID
	})
}

func sortFailurePatterns(rows []failurepatterncontracts.FailurePatternRecord) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SupportCount != rows[j].SupportCount {
			return rows[i].SupportCount > rows[j].SupportCount
		}
		if rows[i].ContributingTestsCount != rows[j].ContributingTestsCount {
			return rows[i].ContributingTestsCount > rows[j].ContributingTestsCount
		}
		if rows[i].Environment != rows[j].Environment {
			return rows[i].Environment < rows[j].Environment
		}
		return rows[i].Phase2ClusterID < rows[j].Phase2ClusterID
	})
}

func sortReviewItems(rows []failurepatterncontracts.ReviewItemRecord) {
	sort.Slice(rows, func(i, j int) bool {
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
}

func sortWindowedRawFailures(rows []storecontracts.RawFailureRecord) {
	sort.Slice(rows, func(i, j int) bool {
		ti, okI := parseTimestamp(rows[i].OccurredAt)
		tj, okJ := parseTimestamp(rows[j].OccurredAt)
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

func parseTimestamp(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func timestampWithinWindow(raw string, startTime time.Time, endTime time.Time) bool {
	parsed, ok := parseTimestamp(raw)
	if !ok {
		return false
	}
	return !parsed.Before(startTime) && parsed.Before(endTime)
}

func sortedKeys[T any](set map[string]T) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func reviewSeverity(supportCount int) string {
	switch {
	case supportCount >= 5:
		return "high"
	case supportCount >= 2:
		return "medium"
	default:
		return "low"
	}
}
