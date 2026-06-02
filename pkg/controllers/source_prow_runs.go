package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowjobs"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

const (
	sourceProwRunsReconcileInterval = 10 * time.Minute
	sourceProwRunsReplayWindow      = 10 * time.Hour
	sourceProwRunsSyncKey           = "all"
	prowRunsDropSampleLimit         = 5
)

type sourceProwRunsController struct {
	logger            logr.Logger
	reconcileInterval time.Duration
	queue             workqueue.TypedRateLimitingInterface[string]

	store      contracts.Store
	prowClient prowjobs.Client
	deps       Dependencies
}

type prowRunsFetchStats struct {
	FetchedJobs int
}

type prowRunDropSample struct {
	Reason         string `json:"reason"`
	Detail         string `json:"detail,omitempty"`
	JobName        string `json:"job_name,omitempty"`
	State          string `json:"state,omitempty"`
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
	BuildID        string `json:"build_id,omitempty"`
	RawRunURL      string `json:"raw_run_url,omitempty"`
}

var _ Controller = (*sourceProwRunsController)(nil)

func NewSourceProwRuns(logger logr.Logger, deps Dependencies) (Controller, error) {
	return newSourceProwRunsController(logger, deps, nil)
}

func newSourceProwRunsController(logger logr.Logger, deps Dependencies, client prowjobs.Client) (*sourceProwRunsController, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("source.prow.runs: store dependency is required")
	}
	if deps.Source == nil {
		return nil, fmt.Errorf("source.prow.runs: source options dependency is required")
	}
	if len(deps.Source.Environments) == 0 {
		return nil, fmt.Errorf("source.prow.runs: no source environments configured")
	}
	if strings.TrimSpace(deps.Source.ProwBaseURL) == "" {
		return nil, fmt.Errorf("source.prow.runs: prow base URL is required")
	}

	for _, env := range deps.Source.Environments {
		if jobNames, ok := sourceoptions.ProwJobNamesForEnvironment(env); !ok || len(jobNames) == 0 {
			return nil, fmt.Errorf("source.prow.runs: missing prow job mapping for environment %q", normalizeEnvironment(env))
		}
	}

	if client == nil {
		client = prowjobs.NewHTTPClient(deps.Source.ProwBaseURL)
	}

	return &sourceProwRunsController{
		logger: logger.WithValues("controller", SourceProwRunsControllerName),
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: SourceProwRunsControllerName,
			},
		),
		reconcileInterval: sourceProwRunsReconcileInterval,
		store:             deps.Store,
		prowClient:        client,
		deps:              deps,
	}, nil
}

func (c *sourceProwRunsController) Run(ctx context.Context, threadiness int) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	if threadiness <= 0 {
		threadiness = 1
	}

	c.logger.Info("Starting controller.", "threads", threadiness)
	for i := 0; i < threadiness; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}
	go wait.JitterUntilWithContext(ctx, c.queueMetadata, c.reconcileInterval, 0.1, true)
	c.logger.Info("Started workers.")
	<-ctx.Done()
	c.logger.Info("Shutting down controller.")
}

func (c *sourceProwRunsController) RunOnce(ctx context.Context, key string) error {
	c.logger.Info("Reconciling one key.", "key", key)
	return c.processKey(ctx, key)
}

func (c *sourceProwRunsController) SyncOnce(ctx context.Context) error {
	if err := c.processKey(ctx, sourceProwRunsSyncKey); err != nil {
		return fmt.Errorf("failed processing key %q: %w", sourceProwRunsSyncKey, err)
	}
	c.logger.Info("Completed one full sync.", "keys", 1)
	return nil
}

func (c *sourceProwRunsController) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *sourceProwRunsController) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.processKey(ctx, key); err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("failed processing key %q", key), "Error syncing; requeuing for later retry", "controller", SourceProwRunsControllerName, "key", key)
	c.queue.AddRateLimited(key)
	return true
}

func (c *sourceProwRunsController) queueMetadata(ctx context.Context) {
	keys, err := c.listKeys(ctx)
	if err != nil {
		utilruntime.HandleErrorWithContext(ctx, err, "Failed listing keys for periodic enqueue", "controller", SourceProwRunsControllerName)
		return
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		c.queue.Add(key)
	}
}

func (c *sourceProwRunsController) listKeys(_ context.Context) ([]string, error) {
	return []string{sourceProwRunsSyncKey}, nil
}

func (c *sourceProwRunsController) configuredEnvironments() []string {
	keys := make([]string, 0, len(c.deps.Source.Environments))
	for _, env := range c.deps.Source.Environments {
		normalized := normalizeEnvironment(env)
		if normalized == "" {
			continue
		}
		keys = append(keys, normalized)
	}
	return keys
}

func (c *sourceProwRunsController) processKey(ctx context.Context, key string) error {
	normalizedKey := normalizeEnvironment(key)
	if normalizedKey == "" {
		return fmt.Errorf("empty key")
	}
	if normalizedKey == sourceProwRunsSyncKey {
		return c.syncEnvironments(ctx, c.configuredEnvironments())
	}
	return c.syncEnvironments(ctx, []string{normalizedKey})
}

func (c *sourceProwRunsController) syncEnvironments(ctx context.Context, environments []string) error {
	if len(environments) == 0 {
		return fmt.Errorf("no environments to sync")
	}

	jobs, fetchStats, err := fetchProwJobsSnapshot(ctx, c.prowClient)
	if err != nil {
		return fmt.Errorf("list prow jobs snapshot: %w", err)
	}
	c.logger.Info(
		"Fetched Prow job snapshot for run discovery.",
		"environments", len(environments),
		"fetched_total", fetchStats.FetchedJobs,
	)

	for _, environment := range environments {
		if err := c.syncEnvironmentFromSnapshot(ctx, environment, jobs, fetchStats); err != nil {
			return err
		}
	}
	return nil
}

func (c *sourceProwRunsController) syncEnvironmentFromSnapshot(ctx context.Context, environment string, snapshotJobs []prowjobs.Job, fetchStats prowRunsFetchStats) error {
	jobNames, ok := sourceoptions.ProwJobNamesForEnvironment(environment)
	if !ok || len(jobNames) == 0 {
		return fmt.Errorf("missing prow job mapping for environment %q", normalizeEnvironment(environment))
	}
	jobNameList := sets.List(jobNames)

	checkpointTime, err := c.getCheckpointTime(ctx, environment)
	if err != nil {
		return err
	}
	since := c.resolveSince(checkpointTime)

	jobs := filterCompletedJobsByNamesAndSince(snapshotJobs, jobNames, since)

	now := time.Now().UTC()
	dropReasons := map[string]int{}
	dropSamples := make([]prowRunDropSample, 0, minInt(prowRunsDropSampleLimit, len(jobs)))
	mappedJobs := make([]prowjobs.Job, 0, len(jobs))
	runRecords := make([]contracts.RunRecord, 0, len(jobs))
	for _, job := range jobs {
		record, ok, reason, detail := mapProwJobToRunRecordDetailed(c.deps.Source.ProwBaseURL, environment, job)
		if !ok {
			if reason == "" {
				reason = "unknown"
			}
			dropReasons[reason]++
			if len(dropSamples) < prowRunsDropSampleLimit {
				dropSamples = append(dropSamples, buildProwRunDropSample(job, reason, detail))
			}
			continue
		}
		mappedJobs = append(mappedJobs, job)
		existing, found, err := c.store.GetRun(ctx, environment, record.RunURL)
		if err != nil {
			return fmt.Errorf("get existing run record for environment=%q run_url=%q: %w", environment, record.RunURL, err)
		}
		record = mergeRunRecordFromProw(existing, found, record)
		runRecords = append(runRecords, record)
	}
	runRecords = dedupeRunRecords(runRecords)

	if len(runRecords) > 0 {
		if err := c.store.UpsertRuns(ctx, runRecords); err != nil {
			return fmt.Errorf("upsert %d prow run records for environment %q: %w", len(runRecords), environment, err)
		}
	}

	nextCheckpoint := computeNextProwRunsCheckpoint(checkpointTime, mappedJobs)
	if !nextCheckpoint.IsZero() {
		checkpoint := contracts.CheckpointRecord{
			Name:      prowRunsCheckpointNameForEnvironment(environment),
			Value:     nextCheckpoint.Format(time.RFC3339Nano),
			UpdatedAt: now.Format(time.RFC3339Nano),
		}
		if err := c.store.UpsertCheckpoints(ctx, []contracts.CheckpointRecord{checkpoint}); err != nil {
			return fmt.Errorf("update prow runs checkpoint for environment %q: %w", environment, err)
		}
	}
	if len(dropSamples) > 0 {
		c.logger.Info(
			"Dropped matched completed Prow jobs during run discovery.",
			"environment", environment,
			"since_start", since.Format(time.RFC3339),
			"dropped_completed", len(jobs)-len(mappedJobs),
			"drop_reasons", dropReasons,
			"drop_examples", dropSamples,
		)
	}

	c.logger.Info(
		"Synced completed Prow runs for environment.",
		"environment", environment,
		"job_names", jobNameList,
		"fetched_total", fetchStats.FetchedJobs,
		"matched_completed", len(jobs),
		"mapped_completed", len(mappedJobs),
		"upserted_runs", len(runRecords),
		"since_start", since.Format(time.RFC3339),
	)
	return nil
}

func (c *sourceProwRunsController) getCheckpointTime(ctx context.Context, environment string) (time.Time, error) {
	checkpoint, found, err := c.store.GetCheckpoint(ctx, prowRunsCheckpointNameForEnvironment(environment))
	if err != nil {
		return time.Time{}, fmt.Errorf("get prow runs checkpoint for environment %q: %w", environment, err)
	}
	if !found {
		return time.Time{}, nil
	}
	if parsed, ok := parseTimestamp(checkpoint.Value); ok {
		return parsed.UTC(), nil
	}
	c.logger.Info("Prow runs checkpoint timestamp is invalid; ignoring saved value.", "environment", environment, "value", checkpoint.Value)
	return time.Time{}, nil
}

func (c *sourceProwRunsController) resolveSince(lastCheckpoint time.Time) time.Time {
	floor := time.Now().UTC().Add(-c.deps.Source.ProwRecentWindow)
	if lastCheckpoint.IsZero() {
		return floor
	}
	since := lastCheckpoint.UTC().Add(-sourceProwRunsReplayWindow)
	if since.Before(floor) {
		return floor
	}
	return since
}

func fetchProwJobsSnapshot(ctx context.Context, client prowjobs.Client) ([]prowjobs.Job, prowRunsFetchStats, error) {
	jobs, err := client.ListJobs(ctx)
	if err != nil {
		return nil, prowRunsFetchStats{}, err
	}
	return jobs, prowRunsFetchStats{FetchedJobs: len(jobs)}, nil
}

func mapProwJobToRunRecord(prowBaseURL string, environment string, job prowjobs.Job) (contracts.RunRecord, bool) {
	record, ok, _, _ := mapProwJobToRunRecordDetailed(prowBaseURL, environment, job)
	return record, ok
}

func mapProwJobToRunRecordDetailed(prowBaseURL string, environment string, job prowjobs.Job) (contracts.RunRecord, bool, string, string) {
	jobName := strings.TrimSpace(job.Spec.Job)
	if jobName == "" {
		return contracts.RunRecord{}, false, "missing_job_name", "spec.job is empty"
	}
	if !prowjobs.IsTerminalState(job.Status.State) {
		return contracts.RunRecord{}, false, "non_terminal_state", fmt.Sprintf("state=%q", strings.TrimSpace(job.Status.State))
	}
	if job.Status.StartTime.IsZero() {
		return contracts.RunRecord{}, false, "missing_start_time", "status.startTime is zero"
	}

	runURL, err := prowartifacts.CanonicalRunURL(prowBaseURL, job.Status.URL)
	if err != nil {
		return contracts.RunRecord{}, false, "unsupported_run_url", err.Error()
	}

	record := contracts.RunRecord{
		Environment: normalizeEnvironment(environment),
		RunURL:      runURL,
		JobName:     jobName,
		Failed:      prowjobs.FailedFromState(job.Status.State),
		OccurredAt:  job.Status.StartTime.UTC().Format(time.RFC3339Nano),
	}

	if job.Spec.Refs != nil && len(job.Spec.Refs.Pulls) > 0 {
		record.PRNumber = job.Spec.Refs.Pulls[0].Number
		record.PRSHA = strings.TrimSpace(job.Spec.Refs.Pulls[0].SHA)
	}

	if !sourceoptions.SupportsPRLookupForEnvironment(environment) {
		record.MergedPR = true
		record.PostGoodCommit = true
	}

	return record, true, "", ""
}

func filterCompletedJobsByNamesAndSince(jobs []prowjobs.Job, jobNames sets.Set[string], since time.Time) []prowjobs.Job {
	if len(jobNames) == 0 {
		return []prowjobs.Job{}
	}

	filtered := make([]prowjobs.Job, 0, len(jobs))
	for _, job := range jobs {
		if !jobNames.Has(strings.TrimSpace(job.Spec.Job)) {
			continue
		}
		if !prowjobs.IsTerminalState(job.Status.State) || job.Status.StartTime.IsZero() {
			continue
		}
		if job.Status.StartTime.UTC().Before(since.UTC()) {
			continue
		}
		filtered = append(filtered, job)
	}
	return filtered
}

func computeNextProwRunsCheckpoint(previous time.Time, jobs []prowjobs.Job) time.Time {
	next := previous.UTC()
	for _, job := range jobs {
		if job.Status.StartTime.IsZero() {
			continue
		}
		startedAt := job.Status.StartTime.UTC()
		if startedAt.After(next) {
			next = startedAt
		}
	}
	return next
}

func buildProwRunDropSample(job prowjobs.Job, reason string, detail string) prowRunDropSample {
	sample := prowRunDropSample{
		Reason:    strings.TrimSpace(reason),
		Detail:    strings.TrimSpace(detail),
		JobName:   strings.TrimSpace(job.Spec.Job),
		State:     strings.TrimSpace(job.Status.State),
		BuildID:   strings.TrimSpace(job.Status.BuildID),
		RawRunURL: strings.TrimSpace(job.Status.URL),
	}
	if !job.Status.StartTime.IsZero() {
		sample.StartTime = job.Status.StartTime.UTC().Format(time.RFC3339Nano)
	}
	if !job.Status.CompletionTime.IsZero() {
		sample.CompletionTime = job.Status.CompletionTime.UTC().Format(time.RFC3339Nano)
	}
	return sample
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func prowRunsCheckpointNameForEnvironment(environment string) string {
	return SourceProwRunsControllerName + "." + normalizeEnvironment(environment)
}
