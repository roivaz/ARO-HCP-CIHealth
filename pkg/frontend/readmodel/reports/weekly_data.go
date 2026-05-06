package reports

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns"
	failurepatterncontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns/contracts"
	readmodelmodel "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/model"
	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

const (
	weeklyWindowDays               = 7
	weeklyMetricRunCount           = "run_count"
	weeklyMetricFailureCount       = "failure_count"
	weeklyMetricFailedCIInfraRuns  = "failed_ci_infra_run_count"
	weeklyMetricFailedProvisionRun = "failed_provision_run_count"
	weeklyMetricFailedE2ERun       = "failed_e2e_run_count"
	weeklyMetricPostGoodRunCount   = "post_good_run_count"
	weeklyMetricPostGoodFailedE2E  = "post_good_failed_e2e_jobs"
	weeklyMetricPostGoodFailedCI   = "post_good_failed_ci_infra_run_count"
	weeklyMetricPostGoodFailedProv = "post_good_failed_provision_run_count"

	weeklyDefaultPeriod            = "default"
	weeklyTestSuccessTarget        = 95.0
	weeklyTestSuccessMinRuns       = 10
	weeklyTestsBelowTargetTopLimit = 5
	weeklyFullErrorExamples        = 3
	defaultHistoryWeeks            = 4
)

var weeklyReportEnvironments = sourceoptions.SupportedEnvironments()

type WeeklyReportBuildOptions struct {
	StartDate           time.Time
	TargetRate          float64
	Week                string
	HistoryHorizonWeeks int
	HistoryResolver     failurepatterns.PresenceResolver
}

type WeeklyCounts struct {
	RunCount                int
	FailureCount            int
	FailedCIInfraRunCount   int
	FailedProvisionRunCount int
	FailedE2ERunCount       int
	PostGoodRunCount        int
	PostGoodFailedE2EJobs   int
	PostGoodFailedCIInfra   int
	PostGoodFailedProvision int
}

type counts = WeeklyCounts

type WeeklyRunOutcomes struct {
	TotalRuns           int
	SuccessfulRuns      int
	CIInfraFailedRuns   int
	ProvisionFailedRuns int
	E2EFailedRuns       int
}

type runOutcomes = WeeklyRunOutcomes

type WeeklyDayReport struct {
	Date                string
	Counts              WeeklyCounts
	PostGoodRunOutcomes WeeklyRunOutcomes
}

type dayReport = WeeklyDayReport

type WeeklyEnvReport struct {
	Environment string
	Days        []WeeklyDayReport
	Totals      WeeklyCounts
}

type envReport = WeeklyEnvReport

type WeeklySemanticEnvSummary struct {
	FailurePatternClusters int
	TestClusters           int
	ReviewItems            int
	TopPhrase              string
	TopSupport             int
	TopPostGood            int
}

type semanticEnvSummary = WeeklySemanticEnvSummary

type WeeklyTopSignature struct {
	Environment       string
	Phrase            string
	ClusterID         string
	SearchQuery       string
	SupportCount      int
	SupportShare      float64
	PostGoodCount     int
	BadPRScore        int
	BadPRReasons      []string
	BadPREvaluated    bool
	SeenInOtherEnvs   []string
	QualityScore      int
	QualityNoteLabels []string
	ContributingTests []readmodelmodel.ContributingTest
	References        []readmodelmodel.RunReference
	FullErrorSamples  []string
	LinkedChildren    []WeeklyTopSignature
}

type topSignature = WeeklyTopSignature

type WeeklySemanticSnapshot struct {
	ByEnvironment                    map[string]WeeklySemanticEnvSummary
	ClusterSignaturesByEnv           map[string][]WeeklyTopSignature
	PhraseSupportByEnv               map[string]map[string]int
	PhrasePostGoodByEnv              map[string]map[string]int
	PhraseReferencesByEnv            map[string]map[string][]readmodelmodel.RunReference
	PhraseContributingTestsByEnv     map[string]map[string][]readmodelmodel.ContributingTest
	PhraseClusterIDByEnv             map[string]map[string]string
	PhraseSearchQueryByEnv           map[string]map[string]string
	PhraseRepresentativeSupportByEnv map[string]map[string]int
	PhraseReferenceKeysByEnv         map[string]map[string]map[string]struct{}
	PhraseFullErrorsByEnv            map[string]map[string][]string
}

type semanticSnapshot = WeeklySemanticSnapshot

type WeeklyBelowTargetTest struct {
	TestName  string
	TestSuite string
	Date      string
	PassRate  float64
	Runs      int
}

type belowTargetTest = WeeklyBelowTargetTest

type WeeklyReportData struct {
	StartDate             time.Time
	EndDate               time.Time
	CurrentReports        []WeeklyEnvReport
	PreviousReports       []WeeklyEnvReport
	TargetRate            float64
	CurrentSemantic       WeeklySemanticSnapshot
	PreviousSemantic      WeeklySemanticSnapshot
	TestsBelowTargetByEnv map[string][]WeeklyBelowTargetTest
	TopSignaturesByEnv    map[string][]WeeklyTopSignature
	HistoryResolver       failurepatterns.PresenceResolver
}

func BuildWeeklyReportData(
	ctx context.Context,
	store storecontracts.Store,
	previousSemanticStore storecontracts.Store,
	opts WeeklyReportBuildOptions,
) (WeeklyReportData, error) {
	if store == nil {
		return WeeklyReportData{}, fmt.Errorf("store is required")
	}
	if opts.StartDate.IsZero() {
		return WeeklyReportData{}, fmt.Errorf("start date is required")
	}

	currentDates := dateWindow(opts.StartDate, weeklyWindowDays)
	currentReports, err := buildEnvReports(ctx, store, currentDates)
	if err != nil {
		return WeeklyReportData{}, err
	}

	previousStart := opts.StartDate.AddDate(0, 0, -weeklyWindowDays)
	previousDates := dateWindow(previousStart, weeklyWindowDays)
	previousReports, err := buildEnvReports(ctx, store, previousDates)
	if err != nil {
		return WeeklyReportData{}, err
	}

	currentStart := opts.StartDate.UTC()
	currentEnd := currentStart.AddDate(0, 0, weeklyWindowDays)
	currentWeekData, err := failurepatterns.LoadRange(ctx, store, failurepatterns.LoadRangeOptions{
		Environments:       append([]string(nil), weeklyReportEnvironments...),
		StartTime:          currentStart,
		EndTime:            currentEnd,
		IncludeRawFailures: true,
	})
	if err != nil {
		return WeeklyReportData{}, fmt.Errorf("load current failure-pattern inputs: %w", err)
	}
	currentSemantic, err := loadSemanticSnapshot(currentWeekData)
	if err != nil {
		return WeeklyReportData{}, fmt.Errorf("load current failure-pattern range: %w", err)
	}
	loadSignatureFullErrorSamplesByEnvironment(
		currentDates,
		currentWeekData.RawFailures,
		&currentSemantic,
		weeklyFullErrorExamples,
	)
	testsBelowTargetByEnv, err := loadBelowTargetTestsByEnvironment(
		ctx,
		store,
		currentDates,
		weeklyDefaultPeriod,
		weeklyTestSuccessTarget,
		weeklyTestSuccessMinRuns,
		weeklyTestsBelowTargetTopLimit,
	)
	if err != nil {
		return WeeklyReportData{}, fmt.Errorf("load weekly tests below target: %w", err)
	}
	var previousSemantic semanticSnapshot
	if previousSemanticStore != nil {
		previousWeekData, loadErr := failurepatterns.LoadRange(ctx, previousSemanticStore, failurepatterns.LoadRangeOptions{
			Environments: append([]string(nil), weeklyReportEnvironments...),
			StartTime:    previousStart,
			EndTime:      currentStart,
		})
		if loadErr != nil {
			return WeeklyReportData{}, fmt.Errorf("load previous failure-pattern inputs: %w", loadErr)
		}
		previousSemantic, err = loadSemanticSnapshot(previousWeekData)
		if err != nil {
			return WeeklyReportData{}, fmt.Errorf("load previous failure-pattern range: %w", err)
		}
	}

	historyResolver := opts.HistoryResolver
	if historyResolver == nil {
		lookbackWeeks := opts.HistoryHorizonWeeks
		if lookbackWeeks <= 0 {
			lookbackWeeks = defaultHistoryWeeks
		}
		historyResolver, err = failurepatterns.BuildPresenceResolver(ctx, failurepatterns.BuildPresenceOptions{
			Store:         store,
			EndTime:       currentEnd,
			LookbackWeeks: lookbackWeeks,
			Environments:  append([]string(nil), weeklyReportEnvironments...),
		})
		if err != nil {
			return WeeklyReportData{}, fmt.Errorf("build signature history resolver: %w", err)
		}
	}
	topSignaturesByEnv := rankTopSignaturesByEnvironment(currentSemantic, historyResolver, 0, 0)

	startDate := currentStart
	return WeeklyReportData{
		StartDate:             startDate,
		EndDate:               startDate.AddDate(0, 0, weeklyWindowDays-1),
		CurrentReports:        currentReports,
		PreviousReports:       previousReports,
		TargetRate:            opts.TargetRate,
		CurrentSemantic:       currentSemantic,
		PreviousSemantic:      previousSemantic,
		TestsBelowTargetByEnv: testsBelowTargetByEnv,
		TopSignaturesByEnv:    topSignaturesByEnv,
		HistoryResolver:       historyResolver,
	}, nil
}

func dateWindow(startDate time.Time, days int) []string {
	if days <= 0 {
		return nil
	}
	out := make([]string, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, startDate.AddDate(0, 0, i).Format("2006-01-02"))
	}
	return out
}

func buildEnvReports(ctx context.Context, store storecontracts.Store, dates []string) ([]envReport, error) {
	metricsByEnvironmentDate, err := loadMetricsDailyByEnvironmentDate(ctx, store, weeklyReportEnvironments, dates)
	if err != nil {
		return nil, err
	}
	reports := make([]envReport, 0, len(weeklyReportEnvironments))
	for _, env := range weeklyReportEnvironments {
		report := envReport{
			Environment: env,
			Days:        make([]dayReport, 0, len(dates)),
		}
		for _, date := range dates {
			rows := metricsByEnvironmentDate[weeklyEnvironmentDateKey(env, date)]
			dayCounts := collectCounts(rows)
			day := dayReport{
				Date:   date,
				Counts: dayCounts,
			}
			if env == "dev" {
				day.PostGoodRunOutcomes = collectPostGoodRunOutcomes(dayCounts)
			}
			report.Days = append(report.Days, day)
			report.Totals = addCounts(report.Totals, dayCounts)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func loadMetricsDailyByEnvironmentDate(
	ctx context.Context,
	store storecontracts.Store,
	environments []string,
	dates []string,
) (map[string][]storecontracts.MetricDailyRecord, error) {
	rows, err := loadMetricsDailyForDates(ctx, store, environments, dates)
	if err != nil {
		return nil, fmt.Errorf("list metrics daily for dates: %w", err)
	}
	out := make(map[string][]storecontracts.MetricDailyRecord)
	for _, row := range rows {
		environment := normalizeReportEnvironment(row.Environment)
		date := strings.TrimSpace(row.Date)
		if environment == "" || date == "" {
			continue
		}
		key := weeklyEnvironmentDateKey(environment, date)
		out[key] = append(out[key], row)
	}
	for key := range out {
		metricRows := out[key]
		sort.Slice(metricRows, func(i, j int) bool {
			return strings.TrimSpace(metricRows[i].Metric) < strings.TrimSpace(metricRows[j].Metric)
		})
		out[key] = metricRows
	}
	return out, nil
}

func loadSemanticSnapshot(weekData failurepatterns.RangeData) (semanticSnapshot, error) {
	out := semanticSnapshot{
		ByEnvironment:                    map[string]semanticEnvSummary{},
		ClusterSignaturesByEnv:           map[string][]topSignature{},
		PhraseSupportByEnv:               map[string]map[string]int{},
		PhrasePostGoodByEnv:              map[string]map[string]int{},
		PhraseReferencesByEnv:            map[string]map[string][]readmodelmodel.RunReference{},
		PhraseContributingTestsByEnv:     map[string]map[string][]readmodelmodel.ContributingTest{},
		PhraseClusterIDByEnv:             map[string]map[string]string{},
		PhraseSearchQueryByEnv:           map[string]map[string]string{},
		PhraseRepresentativeSupportByEnv: map[string]map[string]int{},
		PhraseReferenceKeysByEnv:         map[string]map[string]map[string]struct{}{},
		PhraseFullErrorsByEnv:            map[string]map[string][]string{},
	}

	for _, row := range weekData.FailurePatterns {
		environment := normalizeReportEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		summary := out.ByEnvironment[environment]
		summary.FailurePatternClusters++

		phrase := strings.TrimSpace(row.CanonicalEvidencePhrase)
		if phrase == "" {
			phrase = "(unknown evidence)"
		}
		support := row.SupportCount
		if support < 0 {
			support = 0
		}
		postGood := row.PostGoodCommitCount
		if postGood < 0 {
			postGood = 0
		}

		if support > summary.TopSupport || (support == summary.TopSupport && (summary.TopPhrase == "" || phrase < summary.TopPhrase)) {
			summary.TopPhrase = phrase
			summary.TopSupport = support
			summary.TopPostGood = postGood
		}
		out.ByEnvironment[environment] = summary

		if _, ok := out.PhraseSupportByEnv[environment]; !ok {
			out.PhraseSupportByEnv[environment] = map[string]int{}
		}
		out.PhraseSupportByEnv[environment][phrase] += support

		if _, ok := out.PhrasePostGoodByEnv[environment]; !ok {
			out.PhrasePostGoodByEnv[environment] = map[string]int{}
		}
		out.PhrasePostGoodByEnv[environment][phrase] += postGood

		if _, ok := out.PhraseReferencesByEnv[environment]; !ok {
			out.PhraseReferencesByEnv[environment] = map[string][]readmodelmodel.RunReference{}
		}
		out.PhraseReferencesByEnv[environment][phrase] = append(
			out.PhraseReferencesByEnv[environment][phrase],
			toFailurePatternRunReferences(row.References)...,
		)
		if sourceRunURL := strings.TrimSpace(row.SearchQuerySourceRunURL); sourceRunURL != "" {
			out.PhraseReferencesByEnv[environment][phrase] = append(
				out.PhraseReferencesByEnv[environment][phrase],
				readmodelmodel.RunReference{
					RunURL:      sourceRunURL,
					SignatureID: strings.TrimSpace(row.SearchQuerySourceSignatureID),
				},
			)
		}

		if _, ok := out.PhraseContributingTestsByEnv[environment]; !ok {
			out.PhraseContributingTestsByEnv[environment] = map[string][]readmodelmodel.ContributingTest{}
		}
		out.PhraseContributingTestsByEnv[environment][phrase] = mergeFailurePatternContributingTests(
			out.PhraseContributingTestsByEnv[environment][phrase],
			toFailurePatternContributingTests(row.ContributingTests),
		)

		if _, ok := out.PhraseRepresentativeSupportByEnv[environment]; !ok {
			out.PhraseRepresentativeSupportByEnv[environment] = map[string]int{}
		}
		if _, ok := out.PhraseClusterIDByEnv[environment]; !ok {
			out.PhraseClusterIDByEnv[environment] = map[string]string{}
		}
		if _, ok := out.PhraseSearchQueryByEnv[environment]; !ok {
			out.PhraseSearchQueryByEnv[environment] = map[string]string{}
		}
		repSupport := out.PhraseRepresentativeSupportByEnv[environment][phrase]
		if support > repSupport || strings.TrimSpace(out.PhraseClusterIDByEnv[environment][phrase]) == "" {
			out.PhraseRepresentativeSupportByEnv[environment][phrase] = support
			out.PhraseClusterIDByEnv[environment][phrase] = strings.TrimSpace(row.Phase2ClusterID)
			out.PhraseSearchQueryByEnv[environment][phrase] = strings.TrimSpace(row.SearchQueryPhrase)
		}

		mergePhraseReferenceKeys(out.PhraseReferenceKeysByEnv, environment, phrase, row.References)

		qualityCodes := readmodelmodel.QualityIssueCodes(strings.TrimSpace(phrase))
		qualityLabels := make([]string, 0, len(qualityCodes))
		for _, code := range qualityCodes {
			qualityLabels = append(qualityLabels, readmodelmodel.QualityIssueLabel(code))
		}
		rowReferences := toFailurePatternRunReferences(row.References)
		if sourceRunURL := strings.TrimSpace(row.SearchQuerySourceRunURL); sourceRunURL != "" {
			rowReferences = append(rowReferences, readmodelmodel.RunReference{
				RunURL:      sourceRunURL,
				SignatureID: strings.TrimSpace(row.SearchQuerySourceSignatureID),
			})
		}
		out.ClusterSignaturesByEnv[environment] = append(out.ClusterSignaturesByEnv[environment], topSignature{
			Environment:       environment,
			Phrase:            strings.TrimSpace(phrase),
			ClusterID:         strings.TrimSpace(row.Phase2ClusterID),
			SearchQuery:       strings.TrimSpace(row.SearchQueryPhrase),
			SupportCount:      support,
			PostGoodCount:     postGood,
			QualityScore:      readmodelmodel.QualityScore(qualityCodes),
			QualityNoteLabels: qualityLabels,
			ContributingTests: readmodelmodel.OrderedContributingTests(toFailurePatternContributingTests(row.ContributingTests)),
			References:        rowReferences,
		})
	}

	for _, row := range weekData.SourceFailurePatterns {
		environment := normalizeReportEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		phrase := strings.TrimSpace(row.CanonicalEvidencePhrase)
		if phrase == "" {
			phrase = "(unknown evidence)"
		}
		mergePhraseReferenceKeys(out.PhraseReferenceKeysByEnv, environment, phrase, row.References)
	}

	for environment, testClusterCount := range weekData.TestClusterCountsByEnv {
		summary := out.ByEnvironment[environment]
		summary.TestClusters = testClusterCount
		out.ByEnvironment[environment] = summary
	}

	for _, row := range weekData.ReviewItems {
		environment := normalizeReportEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		summary := out.ByEnvironment[environment]
		summary.ReviewItems++
		out.ByEnvironment[environment] = summary
	}

	return out, nil
}

func collectCounts(rows []storecontracts.MetricDailyRecord) counts {
	out := counts{}
	for _, row := range rows {
		value := int(row.Value)
		switch strings.TrimSpace(row.Metric) {
		case weeklyMetricRunCount:
			out.RunCount = value
		case weeklyMetricFailureCount:
			out.FailureCount = value
		case weeklyMetricFailedCIInfraRuns:
			out.FailedCIInfraRunCount = value
		case weeklyMetricFailedProvisionRun:
			out.FailedProvisionRunCount = value
		case weeklyMetricFailedE2ERun:
			out.FailedE2ERunCount = value
		case weeklyMetricPostGoodRunCount:
			out.PostGoodRunCount = value
		case weeklyMetricPostGoodFailedE2E:
			out.PostGoodFailedE2EJobs = value
		case weeklyMetricPostGoodFailedCI:
			out.PostGoodFailedCIInfra = value
		case weeklyMetricPostGoodFailedProv:
			out.PostGoodFailedProvision = value
		}
	}
	return out
}

func addCounts(a counts, b counts) counts {
	return counts{
		RunCount:                a.RunCount + b.RunCount,
		FailureCount:            a.FailureCount + b.FailureCount,
		FailedCIInfraRunCount:   a.FailedCIInfraRunCount + b.FailedCIInfraRunCount,
		FailedProvisionRunCount: a.FailedProvisionRunCount + b.FailedProvisionRunCount,
		FailedE2ERunCount:       a.FailedE2ERunCount + b.FailedE2ERunCount,
		PostGoodRunCount:        a.PostGoodRunCount + b.PostGoodRunCount,
		PostGoodFailedE2EJobs:   a.PostGoodFailedE2EJobs + b.PostGoodFailedE2EJobs,
		PostGoodFailedCIInfra:   a.PostGoodFailedCIInfra + b.PostGoodFailedCIInfra,
		PostGoodFailedProvision: a.PostGoodFailedProvision + b.PostGoodFailedProvision,
	}
}

func loadBelowTargetTestsByEnvironment(
	ctx context.Context,
	store storecontracts.Store,
	dates []string,
	period string,
	targetPassRate float64,
	minRuns int,
	limit int,
) (map[string][]belowTargetTest, error) {
	out := make(map[string][]belowTargetTest, len(weeklyReportEnvironments))
	trimmedPeriod := strings.TrimSpace(period)
	windowEndDate := ""
	for i := len(dates) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(dates[i])
		if candidate == "" {
			continue
		}
		windowEndDate = candidate
		break
	}
	if windowEndDate == "" {
		return out, nil
	}

	for _, environment := range weeklyReportEnvironments {
		availableDates, err := store.ListTestMetadataDatesByEnvironment(ctx, environment, trimmedPeriod)
		if err != nil {
			return nil, fmt.Errorf("list test metadata dates for env=%q period=%q: %w", environment, trimmedPeriod, err)
		}
		selectedDate := preferredMetadataDateForWindow(windowEndDate, availableDates)
		if selectedDate == "" {
			out[environment] = nil
			continue
		}
		rows, err := store.ListBelowTargetTestMetadataByDate(
			ctx,
			environment,
			selectedDate,
			trimmedPeriod,
			targetPassRate,
			minRuns,
			limit,
		)
		if err != nil {
			return nil, fmt.Errorf("list below-target test metadata for env=%q date=%q: %w", environment, selectedDate, err)
		}
		out[environment] = belowTargetTestsFromMetadataRows(rows)
	}
	return out, nil
}

func belowTargetTestsFromMetadataRows(rows []storecontracts.TestMetadataDailyRecord) []belowTargetTest {
	if len(rows) == 0 {
		return nil
	}
	out := make([]belowTargetTest, 0, len(rows))
	for _, row := range rows {
		testName := strings.TrimSpace(row.TestName)
		if testName == "" {
			continue
		}
		out = append(out, belowTargetTest{
			TestName:  testName,
			TestSuite: strings.TrimSpace(row.TestSuite),
			Date:      strings.TrimSpace(row.Date),
			PassRate:  row.CurrentPassPercentage,
			Runs:      row.CurrentRuns,
		})
	}
	return out
}

func preferredMetadataDateForWindow(windowEndDate string, availableDates []string) string {
	candidateDatesAfter := metadataDatesAfter(availableDates, windowEndDate)
	if len(candidateDatesAfter) > 0 {
		return candidateDatesAfter[0]
	}
	candidateDatesBefore := metadataDatesBefore(availableDates, windowEndDate)
	if len(candidateDatesBefore) > 0 {
		return candidateDatesBefore[0]
	}
	return ""
}

func metadataDatesAfter(metricDates []string, threshold string) []string {
	trimmedThreshold := strings.TrimSpace(threshold)
	unique := map[string]struct{}{}
	for _, date := range metricDates {
		trimmed := strings.TrimSpace(date)
		if trimmed == "" {
			continue
		}
		if trimmedThreshold != "" && trimmed <= trimmedThreshold {
			continue
		}
		unique[trimmed] = struct{}{}
	}
	return readmodelmodel.SortedStringSet(unique)
}

func metadataDatesBefore(metricDates []string, threshold string) []string {
	trimmedThreshold := strings.TrimSpace(threshold)
	unique := map[string]struct{}{}
	for _, date := range metricDates {
		trimmed := strings.TrimSpace(date)
		if trimmed == "" {
			continue
		}
		if trimmedThreshold != "" && trimmed >= trimmedThreshold {
			continue
		}
		unique[trimmed] = struct{}{}
	}
	out := readmodelmodel.SortedStringSet(unique)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func rankTopSignaturesByEnvironment(
	snapshot semanticSnapshot,
	historyResolver failurepatterns.PresenceResolver,
	limit int,
	minShare float64,
) map[string][]topSignature {
	if len(snapshot.ClusterSignaturesByEnv) > 0 {
		return rankTopSignaturesByEnvironmentFromClusters(snapshot, historyResolver, limit, minShare)
	}
	return rankTopSignaturesByEnvironmentFromPhrases(snapshot, historyResolver, limit, minShare)
}

func rankTopSignaturesByEnvironmentFromClusters(
	snapshot semanticSnapshot,
	historyResolver failurepatterns.PresenceResolver,
	limit int,
	minShare float64,
) map[string][]topSignature {
	out := make(map[string][]topSignature, len(weeklyReportEnvironments))
	for _, environment := range weeklyReportEnvironments {
		totalSupport := 0
		clusterRows := snapshot.ClusterSignaturesByEnv[environment]
		for _, item := range clusterRows {
			if item.SupportCount > 0 {
				totalSupport += item.SupportCount
			}
		}

		rows := make([]topSignature, 0, len(clusterRows))
		for _, source := range clusterRows {
			phrase := strings.TrimSpace(source.Phrase)
			if phrase == "" {
				phrase = "(unknown evidence)"
			}
			support := source.SupportCount
			if support <= 0 {
				continue
			}
			otherEnvironments := make([]string, 0, len(weeklyReportEnvironments)-1)
			for _, candidateEnvironment := range weeklyReportEnvironments {
				if candidateEnvironment == environment {
					continue
				}
				if snapshot.PhraseSupportByEnv[candidateEnvironment][phrase] <= 0 {
					continue
				}
				otherEnvironments = append(otherEnvironments, strings.ToUpper(candidateEnvironment))
			}
			share := 0.0
			if totalSupport > 0 {
				share = float64(support) * 100.0 / float64(totalSupport)
			}
			if minShare > 0 && share < minShare {
				continue
			}
			references := append([]readmodelmodel.RunReference(nil), source.References...)
			presence := topSignaturePatternPresence(historyResolver, environment, source.Phrase, source.SearchQuery)
			linkedChildren := make([]topSignature, 0, len(source.LinkedChildren))
			for _, child := range source.LinkedChildren {
				childEnvironment := normalizeReportEnvironment(child.Environment)
				if childEnvironment == "" {
					childEnvironment = environment
				}
				childPhrase := strings.TrimSpace(child.Phrase)
				if childPhrase == "" {
					childPhrase = "(unknown evidence)"
				}
				childSupport := child.SupportCount
				childShare := 0.0
				if totalSupport > 0 && childSupport > 0 {
					childShare = float64(childSupport) * 100.0 / float64(totalSupport)
				}
				childPresence := topSignaturePatternPresence(historyResolver, childEnvironment, child.Phrase, child.SearchQuery)
				linkedChildren = append(linkedChildren, topSignature{
					Environment:       childEnvironment,
					Phrase:            childPhrase,
					ClusterID:         strings.TrimSpace(child.ClusterID),
					SearchQuery:       strings.TrimSpace(child.SearchQuery),
					SupportCount:      childSupport,
					SupportShare:      childShare,
					PostGoodCount:     child.PostGoodCount,
					BadPRScore:        childPresence.BadPRScore,
					BadPRReasons:      append([]string(nil), childPresence.BadPRReasons...),
					BadPREvaluated:    historyResolver != nil,
					QualityScore:      child.QualityScore,
					QualityNoteLabels: append([]string(nil), child.QualityNoteLabels...),
					ContributingTests: append([]readmodelmodel.ContributingTest(nil), child.ContributingTests...),
					References:        append([]readmodelmodel.RunReference(nil), child.References...),
					FullErrorSamples:  append([]string(nil), snapshot.PhraseFullErrorsByEnv[childEnvironment][childPhrase]...),
				})
			}
			rows = append(rows, topSignature{
				Environment:       environment,
				Phrase:            phrase,
				ClusterID:         strings.TrimSpace(source.ClusterID),
				SearchQuery:       strings.TrimSpace(source.SearchQuery),
				SupportCount:      support,
				SupportShare:      share,
				PostGoodCount:     source.PostGoodCount,
				BadPRScore:        presence.BadPRScore,
				BadPRReasons:      append([]string(nil), presence.BadPRReasons...),
				BadPREvaluated:    historyResolver != nil,
				SeenInOtherEnvs:   otherEnvironments,
				QualityScore:      source.QualityScore,
				QualityNoteLabels: append([]string(nil), source.QualityNoteLabels...),
				ContributingTests: append([]readmodelmodel.ContributingTest(nil), source.ContributingTests...),
				References:        references,
				FullErrorSamples:  append([]string(nil), snapshot.PhraseFullErrorsByEnv[environment][phrase]...),
				LinkedChildren:    linkedChildren,
			})
		}
		sortTopSignatures(rows)
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
		out[environment] = rows
	}
	return out
}

func rankTopSignaturesByEnvironmentFromPhrases(
	snapshot semanticSnapshot,
	historyResolver failurepatterns.PresenceResolver,
	limit int,
	minShare float64,
) map[string][]topSignature {
	out := make(map[string][]topSignature, len(weeklyReportEnvironments))
	for _, environment := range weeklyReportEnvironments {
		supportByPhrase := snapshot.PhraseSupportByEnv[environment]
		postGoodByPhrase := snapshot.PhrasePostGoodByEnv[environment]
		totalSupport := 0
		for _, support := range supportByPhrase {
			if support > 0 {
				totalSupport += support
			}
		}

		rows := make([]topSignature, 0, len(supportByPhrase))
		for phrase, support := range supportByPhrase {
			if support <= 0 {
				continue
			}
			otherEnvironments := make([]string, 0, len(weeklyReportEnvironments)-1)
			for _, candidateEnvironment := range weeklyReportEnvironments {
				if candidateEnvironment == environment {
					continue
				}
				if snapshot.PhraseSupportByEnv[candidateEnvironment][phrase] <= 0 {
					continue
				}
				otherEnvironments = append(otherEnvironments, strings.ToUpper(candidateEnvironment))
			}
			share := 0.0
			if totalSupport > 0 {
				share = float64(support) * 100.0 / float64(totalSupport)
			}
			if minShare > 0 && share < minShare {
				continue
			}
			qualityCodes := readmodelmodel.QualityIssueCodes(strings.TrimSpace(phrase))
			qualityLabels := make([]string, 0, len(qualityCodes))
			for _, code := range qualityCodes {
				qualityLabels = append(qualityLabels, readmodelmodel.QualityIssueLabel(code))
			}
			references := append([]readmodelmodel.RunReference(nil), snapshot.PhraseReferencesByEnv[environment][phrase]...)
			presence := topSignaturePatternPresence(
				historyResolver,
				environment,
				phrase,
				snapshot.PhraseSearchQueryByEnv[environment][phrase],
			)
			rows = append(rows, topSignature{
				Environment:       environment,
				Phrase:            strings.TrimSpace(phrase),
				ClusterID:         strings.TrimSpace(snapshot.PhraseClusterIDByEnv[environment][phrase]),
				SearchQuery:       strings.TrimSpace(snapshot.PhraseSearchQueryByEnv[environment][phrase]),
				SupportCount:      support,
				SupportShare:      share,
				PostGoodCount:     postGoodByPhrase[phrase],
				BadPRScore:        presence.BadPRScore,
				BadPRReasons:      append([]string(nil), presence.BadPRReasons...),
				BadPREvaluated:    historyResolver != nil,
				SeenInOtherEnvs:   otherEnvironments,
				QualityScore:      readmodelmodel.QualityScore(qualityCodes),
				QualityNoteLabels: qualityLabels,
				ContributingTests: append([]readmodelmodel.ContributingTest(nil), snapshot.PhraseContributingTestsByEnv[environment][phrase]...),
				References:        references,
				FullErrorSamples:  append([]string(nil), snapshot.PhraseFullErrorsByEnv[environment][phrase]...),
			})
		}
		sortTopSignatures(rows)
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
		out[environment] = rows
	}
	return out
}

func topSignaturePatternPresence(
	historyResolver failurepatterns.PresenceResolver,
	environment string,
	phrase string,
	searchQuery string,
) failurepatterns.PatternPresence {
	if historyResolver == nil {
		return failurepatterns.PatternPresence{}
	}
	return historyResolver.PresenceFor(failurepatterns.PatternKey{
		Environment: environment,
		Phrase:      phrase,
		SearchQuery: searchQuery,
	})
}

func sortTopSignatures(rows []topSignature) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].BadPRScore != rows[j].BadPRScore {
			return rows[i].BadPRScore < rows[j].BadPRScore
		}
		if rows[i].SupportCount != rows[j].SupportCount {
			return rows[i].SupportCount > rows[j].SupportCount
		}
		if rows[i].PostGoodCount != rows[j].PostGoodCount {
			return rows[i].PostGoodCount > rows[j].PostGoodCount
		}
		return rows[i].Phrase < rows[j].Phrase
	})
}

func topSignaturesFromFailurePatternClusters(rows []failurepatterncontracts.FailurePatternRecord) []topSignature {
	out := make([]topSignature, 0, len(rows))
	for _, row := range rows {
		environment := normalizeReportEnvironment(row.Environment)
		if environment == "" {
			continue
		}
		phrase := strings.TrimSpace(row.CanonicalEvidencePhrase)
		if phrase == "" {
			phrase = "(unknown evidence)"
		}
		support := row.SupportCount
		if support < 0 {
			support = 0
		}
		postGood := row.PostGoodCommitCount
		if postGood < 0 {
			postGood = 0
		}
		qualityCodes := readmodelmodel.QualityIssueCodes(phrase)
		qualityLabels := make([]string, 0, len(qualityCodes))
		for _, code := range qualityCodes {
			qualityLabels = append(qualityLabels, readmodelmodel.QualityIssueLabel(code))
		}
		references := toFailurePatternRunReferences(row.References)
		if sourceRunURL := strings.TrimSpace(row.SearchQuerySourceRunURL); sourceRunURL != "" {
			references = append(references, readmodelmodel.RunReference{
				RunURL:      sourceRunURL,
				SignatureID: strings.TrimSpace(row.SearchQuerySourceSignatureID),
			})
		}
		out = append(out, topSignature{
			Environment:       environment,
			Phrase:            phrase,
			ClusterID:         strings.TrimSpace(row.Phase2ClusterID),
			SearchQuery:       strings.TrimSpace(row.SearchQueryPhrase),
			SupportCount:      support,
			PostGoodCount:     postGood,
			QualityScore:      readmodelmodel.QualityScore(qualityCodes),
			QualityNoteLabels: qualityLabels,
			ContributingTests: readmodelmodel.OrderedContributingTests(toFailurePatternContributingTests(row.ContributingTests)),
			References:        references,
		})
	}
	return out
}

func toFailurePatternRunReferences(rows []failurepatterncontracts.ReferenceRecord) []readmodelmodel.RunReference {
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

func toFailurePatternContributingTests(rows []failurepatterncontracts.ContributingTestRecord) []readmodelmodel.ContributingTest {
	out := make([]readmodelmodel.ContributingTest, 0, len(rows))
	for _, row := range rows {
		out = append(out, readmodelmodel.ContributingTest{
			FailedAt:    strings.TrimSpace(row.Lane),
			JobName:     strings.TrimSpace(row.JobName),
			TestName:    strings.TrimSpace(row.TestName),
			Occurrences: row.SupportCount,
		})
	}
	return out
}

func mergeFailurePatternContributingTests(existing []readmodelmodel.ContributingTest, incoming []readmodelmodel.ContributingTest) []readmodelmodel.ContributingTest {
	if len(incoming) == 0 {
		return existing
	}
	type mergeKey struct {
		lane string
		job  string
		test string
	}
	merged := make(map[mergeKey]readmodelmodel.ContributingTest, len(existing)+len(incoming))
	for _, item := range existing {
		merged[mergeKey{
			lane: strings.TrimSpace(item.FailedAt),
			job:  strings.TrimSpace(item.JobName),
			test: strings.TrimSpace(item.TestName),
		}] = item
	}
	for _, item := range incoming {
		key := mergeKey{
			lane: strings.TrimSpace(item.FailedAt),
			job:  strings.TrimSpace(item.JobName),
			test: strings.TrimSpace(item.TestName),
		}
		existingItem, ok := merged[key]
		if !ok {
			merged[key] = item
			continue
		}
		existingItem.Occurrences += item.Occurrences
		merged[key] = existingItem
	}
	out := make([]readmodelmodel.ContributingTest, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	return readmodelmodel.OrderedContributingTests(out)
}

func loadSignatureFullErrorSamplesByEnvironment(
	dates []string,
	rawRows []storecontracts.RawFailureRecord,
	snapshot *semanticSnapshot,
	limit int,
) {
	if snapshot == nil || limit <= 0 || len(dates) == 0 {
		return
	}
	if snapshot.PhraseFullErrorsByEnv == nil {
		snapshot.PhraseFullErrorsByEnv = map[string]map[string][]string{}
	}
	rawByEnvironmentDate := indexRawFailuresByEnvironmentDate(rawRows)
	for environment, referenceKeysByPhrase := range snapshot.PhraseReferenceKeysByEnv {
		if len(referenceKeysByPhrase) == 0 {
			continue
		}
		matchKeyToPhrases := map[string]map[string]struct{}{}
		for phrase, keySet := range referenceKeysByPhrase {
			for key := range keySet {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					continue
				}
				if _, ok := matchKeyToPhrases[trimmedKey]; !ok {
					matchKeyToPhrases[trimmedKey] = map[string]struct{}{}
				}
				matchKeyToPhrases[trimmedKey][phrase] = struct{}{}
			}
		}
		if len(matchKeyToPhrases) == 0 {
			continue
		}
		if _, ok := snapshot.PhraseFullErrorsByEnv[environment]; !ok {
			snapshot.PhraseFullErrorsByEnv[environment] = map[string][]string{}
		}
		for dateIndex := len(dates) - 1; dateIndex >= 0; dateIndex-- {
			date := strings.TrimSpace(dates[dateIndex])
			if date == "" {
				continue
			}
			for _, row := range rawByEnvironmentDate[weeklyEnvironmentDateKey(environment, date)] {
				phraseSet := map[string]struct{}{}
				for _, key := range failurePatternsRawFailureMatchKeys(row) {
					for phrase := range matchKeyToPhrases[key] {
						phraseSet[phrase] = struct{}{}
					}
				}
				if len(phraseSet) == 0 {
					continue
				}
				sample := strings.TrimSpace(row.RawText)
				if sample == "" {
					sample = strings.TrimSpace(row.NormalizedText)
				}
				if sample == "" {
					continue
				}
				for phrase := range phraseSet {
					existing := snapshot.PhraseFullErrorsByEnv[environment][phrase]
					snapshot.PhraseFullErrorsByEnv[environment][phrase] = appendUniqueLimitedSample(existing, sample, limit)
				}
			}
		}
	}
}

func mergePhraseReferenceKeys(
	byEnvironment map[string]map[string]map[string]struct{},
	environment string,
	phrase string,
	references []failurepatterncontracts.ReferenceRecord,
) {
	if byEnvironment == nil {
		return
	}
	normalizedEnvironment := normalizeReportEnvironment(environment)
	trimmedPhrase := strings.TrimSpace(phrase)
	if normalizedEnvironment == "" || trimmedPhrase == "" {
		return
	}
	if _, ok := byEnvironment[normalizedEnvironment]; !ok {
		byEnvironment[normalizedEnvironment] = map[string]map[string]struct{}{}
	}
	if _, ok := byEnvironment[normalizedEnvironment][trimmedPhrase]; !ok {
		byEnvironment[normalizedEnvironment][trimmedPhrase] = map[string]struct{}{}
	}
	keySet := byEnvironment[normalizedEnvironment][trimmedPhrase]
	for _, key := range failurePatternReportReferenceKeys(toFailurePatternReportReferences(references)) {
		keySet[key] = struct{}{}
	}
}

func failurePatternReportReferenceKeys(rows []readmodelpatterns.FailurePatternReportReference) []string {
	keys := make([]string, 0, len(rows)*2)
	seen := map[string]struct{}{}
	for _, row := range rows {
		for _, key := range failurePatternsReferenceMatchKeys(row) {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
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

func failurePatternsReferenceMatchKeys(row readmodelpatterns.FailurePatternReportReference) []string {
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

func indexRawFailuresByEnvironmentDate(rows []storecontracts.RawFailureRecord) map[string][]storecontracts.RawFailureRecord {
	out := map[string][]storecontracts.RawFailureRecord{}
	for _, row := range rows {
		environment := normalizeReportEnvironment(row.Environment)
		date, ok := dateFromTimestamp(row.OccurredAt)
		if !ok {
			continue
		}
		key := weeklyEnvironmentDateKey(environment, date)
		if key == "" {
			continue
		}
		out[key] = append(out[key], row)
	}
	for key := range out {
		rawRows := out[key]
		sort.Slice(rawRows, func(i, j int) bool {
			if rawRows[i].OccurredAt != rawRows[j].OccurredAt {
				return rawRows[i].OccurredAt < rawRows[j].OccurredAt
			}
			if rawRows[i].RunURL != rawRows[j].RunURL {
				return rawRows[i].RunURL < rawRows[j].RunURL
			}
			if rawRows[i].RowID != rawRows[j].RowID {
				return rawRows[i].RowID < rawRows[j].RowID
			}
			return rawRows[i].SignatureID < rawRows[j].SignatureID
		})
		out[key] = rawRows
	}
	return out
}

func appendUniqueLimitedSample(existing []string, candidate string, limit int) []string {
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

func collectPostGoodRunOutcomes(day counts) runOutcomes {
	out := runOutcomes{}

	ciInfraFailedRuns := day.PostGoodFailedCIInfra
	provisionFailedRuns := day.PostGoodFailedProvision
	e2eFailedRuns := day.PostGoodFailedE2EJobs
	totalFailedRuns := ciInfraFailedRuns + provisionFailedRuns + e2eFailedRuns

	totalRuns := day.PostGoodRunCount
	if totalRuns < totalFailedRuns {
		totalRuns = totalFailedRuns
	}
	successfulRuns := totalRuns - totalFailedRuns
	if successfulRuns < 0 {
		successfulRuns = 0
	}

	out.TotalRuns = totalRuns
	out.SuccessfulRuns = successfulRuns
	out.CIInfraFailedRuns = ciInfraFailedRuns
	out.ProvisionFailedRuns = provisionFailedRuns
	out.E2EFailedRuns = e2eFailedRuns
	return out
}

func normalizeReportEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func weeklyEnvironmentDateKey(environment string, date string) string {
	normalizedEnvironment := normalizeReportEnvironment(environment)
	trimmedDate := strings.TrimSpace(date)
	if normalizedEnvironment == "" || trimmedDate == "" {
		return ""
	}
	return normalizedEnvironment + "|" + trimmedDate
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
