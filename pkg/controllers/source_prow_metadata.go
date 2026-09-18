package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"

	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

const (
	sourceProwMetadataReconcileInterval = 10 * time.Minute
	sourceProwMetadataFetchTimeout      = 45 * time.Second
)

type prowMetadataClient interface {
	GetRunTiming(ctx context.Context, runURL string) (prowartifacts.TimingResult, error)
	GetRunRegion(ctx context.Context, runURL string, relativePath string) (prowartifacts.RegionResult, error)
}

type sourceProwMetadataController struct {
	logger              logr.Logger
	reconcileInterval   time.Duration
	queue               workqueue.TypedRateLimitingInterface[string]
	activeWindow        time.Duration
	artifactRetryWindow time.Duration
	fetchTimeout        time.Duration
	envSet              map[string]struct{}
	regionEnvSet        map[string]struct{}

	store      contracts.Store
	prowClient prowMetadataClient
}

var _ Controller = (*sourceProwMetadataController)(nil)

func NewSourceProwMetadata(logger logr.Logger, deps Dependencies) (Controller, error) {
	return newSourceProwMetadataController(logger, deps, nil)
}

func newSourceProwMetadataController(logger logr.Logger, deps Dependencies, client prowMetadataClient) (*sourceProwMetadataController, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("source.prow.metadata: store dependency is required")
	}
	if deps.Source == nil {
		return nil, fmt.Errorf("source.prow.metadata: source options dependency is required")
	}
	if len(deps.Source.Environments) == 0 {
		return nil, fmt.Errorf("source.prow.metadata: no source environments configured")
	}
	if strings.TrimSpace(deps.Source.ProwArtifactsBaseURL) == "" {
		return nil, fmt.Errorf("source.prow.metadata: prow artifacts base URL is required")
	}

	envSet := make(map[string]struct{}, len(deps.Source.Environments))
	regionEnvSet := make(map[string]struct{}, len(deps.Source.Environments))
	for _, environment := range deps.Source.Environments {
		normalized := normalizeEnvironment(environment)
		if normalized == "" {
			continue
		}
		envSet[normalized] = struct{}{}
		if _, ok := sourceoptions.RunRegionArtifactPathForEnvironment(normalized); ok {
			regionEnvSet[normalized] = struct{}{}
		}
	}
	if client == nil {
		client = deps.ProwArtifacts
	}
	if client == nil {
		client = newProwArtifactsClient(deps.Source)
	}

	return &sourceProwMetadataController{
		logger: logger.WithValues("controller", SourceProwMetadataControllerName),
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: SourceProwMetadataControllerName,
			},
		),
		reconcileInterval:   sourceProwMetadataReconcileInterval,
		activeWindow:        activeReconcileWindow(deps.Source),
		artifactRetryWindow: deps.Source.ProwArtifactRetryWindow,
		fetchTimeout:        sourceProwMetadataFetchTimeout,
		envSet:              envSet,
		regionEnvSet:        regionEnvSet,
		store:               deps.Store,
		prowClient:          client,
	}, nil
}

func (c *sourceProwMetadataController) Run(ctx context.Context, threadiness int) {
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

func (c *sourceProwMetadataController) RunOnce(ctx context.Context, key string) error {
	c.logger.Info("Reconciling one key.", "key", key)
	return c.processKey(ctx, key)
}

func (c *sourceProwMetadataController) SyncOnce(ctx context.Context) error {
	keys, err := c.listKeys(ctx)
	if err != nil {
		return err
	}
	c.logger.Info("Starting one full sync.", "keys", len(keys))
	for i, key := range keys {
		c.logger.Info("Processing Prow run metadata sync.", "index", i+1, "total", len(keys), "key", key)
		if err := c.processKey(ctx, key); err != nil {
			return fmt.Errorf("failed processing key %q: %w", key, err)
		}
	}
	c.logger.Info("Completed one full sync.", "keys", len(keys))
	return nil
}

func (c *sourceProwMetadataController) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *sourceProwMetadataController) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.processKey(ctx, key); err != nil {
		utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("failed processing key %q: %w", key, err), "Error syncing; requeuing for later retry", "controller", SourceProwMetadataControllerName, "key", key)
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *sourceProwMetadataController) queueMetadata(ctx context.Context) {
	keys, err := c.listKeys(ctx)
	if err != nil {
		utilruntime.HandleErrorWithContext(ctx, err, "Failed listing keys for periodic enqueue", "controller", SourceProwMetadataControllerName)
		return
	}
	for _, key := range keys {
		c.queue.Add(key)
	}
}

func (c *sourceProwMetadataController) listKeys(ctx context.Context) ([]string, error) {
	environments := make([]string, 0, len(c.envSet))
	for environment := range c.envSet {
		environments = append(environments, environment)
	}
	startTime := time.Now().UTC().Add(-c.activeWindow)
	timingRuns, err := c.store.ListRunsNeedingTimingMetadata(ctx, environments, startTime)
	if err != nil {
		return nil, fmt.Errorf("list runs needing timing metadata: %w", err)
	}

	keys := make(map[string]struct{}, len(timingRuns))
	for _, run := range timingRuns {
		keys[normalizeEnvironment(run.Environment)+"|"+strings.TrimSpace(run.RunURL)] = struct{}{}
	}

	regionEnvironments := make([]string, 0, len(c.regionEnvSet))
	for environment := range c.regionEnvSet {
		regionEnvironments = append(regionEnvironments, environment)
	}
	regionRuns, err := c.store.ListRunsNeedingRegionMetadata(ctx, regionEnvironments, startTime)
	if err != nil {
		return nil, fmt.Errorf("list runs needing region metadata: %w", err)
	}
	for _, run := range regionRuns {
		if !runSupportsRegionMetadata(run) {
			continue
		}
		keys[normalizeEnvironment(run.Environment)+"|"+strings.TrimSpace(run.RunURL)] = struct{}{}
	}

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (c *sourceProwMetadataController) isEnvironmentEnabled(environment string) bool {
	_, enabled := c.envSet[normalizeEnvironment(environment)]
	return enabled
}

func (c *sourceProwMetadataController) processKey(ctx context.Context, key string) error {
	environment, runURL, err := splitEnvironmentRunKey(key)
	if err != nil {
		return err
	}
	if !c.isEnvironmentEnabled(environment) {
		return nil
	}

	run, found, err := c.store.GetRun(ctx, environment, runURL)
	if err != nil {
		return fmt.Errorf("get run metadata for key %q: %w", key, err)
	}
	if !found {
		return nil
	}
	timingPending := !runTimingMetadataTerminal(run)
	regionPending := c.runSupportsRegionMetadata(run) && !runRegionMetadataTerminal(run)
	if !timingPending && !regionPending {
		return nil
	}

	var (
		timingResult prowartifacts.TimingResult
		timingErr    error
		regionResult prowartifacts.RegionResult
		regionErr    error
		waitGroup    sync.WaitGroup
	)
	if timingPending {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			timingResult, timingErr = c.fetchTimingMetadata(ctx, run)
		}()
	}
	if regionPending {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			regionResult, regionErr = c.fetchRegionMetadata(ctx, run)
		}()
	}
	waitGroup.Wait()

	errs := make([]error, 0, 2)
	if timingPending {
		if timingErr != nil {
			errs = append(errs, timingErr)
		} else if err := c.persistTimingMetadata(ctx, run, timingResult); err != nil {
			errs = append(errs, err)
		}
	}
	if regionPending {
		if regionErr != nil {
			errs = append(errs, regionErr)
		} else if err := c.persistRegionMetadata(ctx, run, regionResult); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *sourceProwMetadataController) fetchTimingMetadata(ctx context.Context, run contracts.RunRecord) (prowartifacts.TimingResult, error) {
	fetchCtx, cancel := c.fetchContext(ctx)
	defer cancel()

	result, err := c.prowClient.GetRunTiming(fetchCtx, run.RunURL)
	if err != nil {
		return prowartifacts.TimingResult{}, fmt.Errorf("fetch run timing for %q: %w", run.RunURL, err)
	}
	return result, nil
}

func (c *sourceProwMetadataController) persistTimingMetadata(ctx context.Context, run contracts.RunRecord, result prowartifacts.TimingResult) error {
	now := time.Now().UTC()
	firstCheckedAt := strings.TrimSpace(run.TimingMetadataFirstCheckedAt)
	if _, ok := parseTimestamp(firstCheckedAt); !ok {
		firstCheckedAt = now.Format(time.RFC3339Nano)
	}
	run.TimingMetadataFirstCheckedAt = firstCheckedAt
	run.TimingMetadataCheckedAt = now.Format(time.RFC3339Nano)
	switch result.Outcome {
	case prowartifacts.ArtifactOutcomeFound:
		run.StartedAt = result.StartedAt
		run.CompletedAt = result.CompletedAt
		if run.CompletedAt != "" {
			run.TimingMetadataState = contracts.RunTimingMetadataStateFound
		} else if metadataRetryWindowElapsed(run.TimingMetadataFirstCheckedAt, c.artifactRetryWindow, now) {
			run.TimingMetadataState = contracts.RunTimingMetadataStateInvalid
		} else {
			run.TimingMetadataState = contracts.RunTimingMetadataStatePending
		}
	case prowartifacts.ArtifactOutcomeForbidden:
		run.StartedAt = ""
		run.CompletedAt = ""
		run.TimingMetadataState = contracts.RunTimingMetadataStateForbidden
	case prowartifacts.ArtifactOutcomeInvalid:
		run.StartedAt = ""
		run.CompletedAt = ""
		if metadataRetryWindowElapsed(run.TimingMetadataFirstCheckedAt, c.artifactRetryWindow, now) {
			run.TimingMetadataState = contracts.RunTimingMetadataStateInvalid
		} else {
			run.TimingMetadataState = contracts.RunTimingMetadataStatePending
		}
	case prowartifacts.ArtifactOutcomeMissing:
		if metadataRetryWindowElapsed(run.TimingMetadataFirstCheckedAt, c.artifactRetryWindow, now) {
			run.TimingMetadataState = contracts.RunTimingMetadataStateMissing
		} else {
			run.TimingMetadataState = contracts.RunTimingMetadataStatePending
		}
	default:
		return fmt.Errorf("fetch run timing for %q returned unknown artifact outcome %q", run.RunURL, result.Outcome)
	}

	if err := c.store.UpdateRunTimingMetadata(ctx, run); err != nil {
		return fmt.Errorf("update run timing metadata for %q: %w", run.RunURL, err)
	}
	c.logger.Info("Updated Prow run timing metadata.", "run_url", run.RunURL, "started_at", run.StartedAt, "completed_at", run.CompletedAt, "state", run.TimingMetadataState)
	return nil
}

func (c *sourceProwMetadataController) fetchRegionMetadata(ctx context.Context, run contracts.RunRecord) (prowartifacts.RegionResult, error) {
	artifactPath, ok := sourceoptions.RunRegionArtifactPathForEnvironment(run.Environment)
	if !ok {
		return prowartifacts.RegionResult{}, nil
	}
	fetchCtx, cancel := c.fetchContext(ctx)
	defer cancel()

	result, err := c.prowClient.GetRunRegion(fetchCtx, run.RunURL, artifactPath)
	if err != nil {
		return prowartifacts.RegionResult{}, fmt.Errorf("fetch run region for %q: %w", run.RunURL, err)
	}
	return result, nil
}

func (c *sourceProwMetadataController) persistRegionMetadata(ctx context.Context, run contracts.RunRecord, result prowartifacts.RegionResult) error {
	now := time.Now().UTC()
	firstCheckedAt := strings.TrimSpace(run.RegionMetadataFirstCheckedAt)
	if _, ok := parseTimestamp(firstCheckedAt); !ok {
		firstCheckedAt = now.Format(time.RFC3339Nano)
	}
	run.RegionMetadataFirstCheckedAt = firstCheckedAt
	run.RegionMetadataCheckedAt = now.Format(time.RFC3339Nano)
	switch result.Outcome {
	case prowartifacts.ArtifactOutcomeFound:
		run.Region = result.Region
		run.RegionMetadataState = contracts.RunRegionMetadataStateFound
	case prowartifacts.ArtifactOutcomeForbidden:
		run.Region = ""
		run.RegionMetadataState = contracts.RunRegionMetadataStateForbidden
	case prowartifacts.ArtifactOutcomeInvalid:
		run.Region = ""
		if metadataRetryWindowElapsed(run.RegionMetadataFirstCheckedAt, c.artifactRetryWindow, now) {
			run.RegionMetadataState = contracts.RunRegionMetadataStateInvalid
		} else {
			run.RegionMetadataState = contracts.RunRegionMetadataStatePending
		}
	case prowartifacts.ArtifactOutcomeMissing:
		if metadataRetryWindowElapsed(run.RegionMetadataFirstCheckedAt, c.artifactRetryWindow, now) {
			run.RegionMetadataState = contracts.RunRegionMetadataStateMissing
		} else {
			run.RegionMetadataState = contracts.RunRegionMetadataStatePending
		}
	default:
		return fmt.Errorf("fetch run region for %q returned unknown artifact outcome %q", run.RunURL, result.Outcome)
	}

	if err := c.store.UpdateRunRegionMetadata(ctx, run); err != nil {
		return fmt.Errorf("update run region metadata for %q: %w", run.RunURL, err)
	}
	c.logger.Info("Updated Prow run region metadata.", "run_url", run.RunURL, "region", run.Region, "state", run.RegionMetadataState)
	return nil
}

func (c *sourceProwMetadataController) fetchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.fetchTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.fetchTimeout)
}

func metadataRetryWindowElapsed(firstCheckedAtValue string, retryWindow time.Duration, now time.Time) bool {
	if retryWindow <= 0 {
		return true
	}
	firstCheckedAt, ok := parseTimestamp(firstCheckedAtValue)
	return ok && now.Sub(firstCheckedAt.UTC()) >= retryWindow
}

func (c *sourceProwMetadataController) runSupportsRegionMetadata(run contracts.RunRecord) bool {
	if _, ok := c.regionEnvSet[normalizeEnvironment(run.Environment)]; !ok {
		return false
	}
	return runSupportsRegionMetadata(run)
}

func runSupportsRegionMetadata(run contracts.RunRecord) bool {
	jobNames, ok := sourceoptions.ProwJobNamesForEnvironment(run.Environment)
	return ok && jobNames.Has(strings.TrimSpace(run.JobName))
}

func runTimingMetadataTerminal(run contracts.RunRecord) bool {
	if strings.TrimSpace(run.StartedAt) != "" && strings.TrimSpace(run.CompletedAt) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(run.TimingMetadataState)) {
	case contracts.RunTimingMetadataStateFound,
		contracts.RunTimingMetadataStateForbidden,
		contracts.RunTimingMetadataStateMissing,
		contracts.RunTimingMetadataStateInvalid:
		return true
	default:
		return false
	}
}

func runRegionMetadataTerminal(run contracts.RunRecord) bool {
	if strings.TrimSpace(run.Region) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(run.RegionMetadataState)) {
	case contracts.RunRegionMetadataStateFound,
		contracts.RunRegionMetadataStateForbidden,
		contracts.RunRegionMetadataStateMissing,
		contracts.RunRegionMetadataStateInvalid:
		return true
	default:
		return false
	}
}
