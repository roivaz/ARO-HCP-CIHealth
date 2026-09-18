package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestSourceProwMetadataStoresTimingAndSelectedRegion(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2100878007617982464"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	client := &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{
			Outcome:     prowartifacts.ArtifactOutcomeFound,
			StartedAt:   "2026-09-18T10:19:12Z",
			CompletedAt: "2026-09-18T12:27:55Z",
		},
		regionResult: prowartifacts.RegionResult{
			Outcome: prowartifacts.ArtifactOutcomeFound,
			Region:  "westus3",
		},
	}
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, client)
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}

	run, found := store.GetStoredRun("dev", runURL)
	if !found {
		t.Fatalf("expected run to remain stored")
	}
	if run.Region != "westus3" {
		t.Fatalf("unexpected region: got=%q want=%q", run.Region, "westus3")
	}
	if run.RegionMetadataState != contracts.RunRegionMetadataStateFound {
		t.Fatalf("unexpected metadata state: got=%q want=%q", run.RegionMetadataState, contracts.RunRegionMetadataStateFound)
	}
	if run.StartedAt != "2026-09-18T10:19:12Z" || run.CompletedAt != "2026-09-18T12:27:55Z" {
		t.Fatalf("unexpected timing metadata: started=%q completed=%q", run.StartedAt, run.CompletedAt)
	}
	if run.TimingMetadataState != contracts.RunTimingMetadataStateFound {
		t.Fatalf("unexpected timing metadata state: got=%q want=%q", run.TimingMetadataState, contracts.RunTimingMetadataStateFound)
	}
}

func TestSourceProwMetadataFetchesTimingAndRegionConcurrently(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/concurrent"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	client := &blockingProwMetadataClient{
		timingStarted: make(chan struct{}, 1),
		regionStarted: make(chan struct{}, 1),
		release:       make(chan struct{}),
	}
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, client)
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		result <- controller.processKey(context.Background(), "dev|"+runURL)
	}()

	released := false
	defer func() {
		if !released {
			close(client.release)
		}
	}()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for timingStarted, regionStarted := false, false; !timingStarted || !regionStarted; {
		select {
		case <-client.timingStarted:
			timingStarted = true
		case <-client.regionStarted:
			regionStarted = true
		case <-timeout.C:
			t.Fatalf("timing and region fetches did not start concurrently")
		}
	}
	close(client.release)
	released = true

	if err := <-result; err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.TimingMetadataState != contracts.RunTimingMetadataStateFound || run.RegionMetadataState != contracts.RunRegionMetadataStateFound {
		t.Fatalf("expected both metadata updates to persist, got=%+v", run)
	}
}

func TestSourceProwMetadataPersistsSuccessfulFetchWhenOtherFetchFails(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/partial"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{
			Outcome:     prowartifacts.ArtifactOutcomeFound,
			StartedAt:   "2026-09-18T10:19:12Z",
			CompletedAt: "2026-09-18T12:27:55Z",
		},
		regionErr: context.DeadlineExceeded,
	})
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected region fetch error, got=%v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.TimingMetadataState != contracts.RunTimingMetadataStateFound {
		t.Fatalf("expected successful timing metadata to persist, got=%+v", run)
	}
	if run.RegionMetadataState != "" {
		t.Fatalf("expected failed region fetch to remain unresolved, got=%+v", run)
	}
}

func TestSourceProwMetadataTreatsForbiddenAsTerminal(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2100878007617982464"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	client := &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{Outcome: prowartifacts.ArtifactOutcomeForbidden},
		regionResult: prowartifacts.RegionResult{Outcome: prowartifacts.ArtifactOutcomeForbidden},
	}
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, client)
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error for terminal forbidden result: %v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.RegionMetadataState != contracts.RunRegionMetadataStateForbidden {
		t.Fatalf("unexpected metadata state: got=%q want=%q", run.RegionMetadataState, contracts.RunRegionMetadataStateForbidden)
	}
	if run.TimingMetadataState != contracts.RunTimingMetadataStateForbidden {
		t.Fatalf("unexpected timing metadata state: got=%q want=%q", run.TimingMetadataState, contracts.RunTimingMetadataStateForbidden)
	}

	keys, err := controller.listKeys(context.Background())
	if err != nil {
		t.Fatalf("listKeys returned error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected terminally forbidden run not to be queued again, got=%v", keys)
	}
}

func TestSourceProwMetadataUsesActiveHistoryWindow(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	recentURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/recent"
	oldURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/6970/pull-ci-Azure-ARO-HCP-main-e2e-parallel/old"
	store := newFakeRunStore(
		contracts.RunRecord{
			Environment: "dev",
			RunURL:      recentURL,
			JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
			OccurredAt:  now.Add(-6 * 24 * time.Hour).Format(time.RFC3339Nano),
		},
		contracts.RunRecord{
			Environment: "dev",
			RunURL:      oldURL,
			JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
			OccurredAt:  now.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano),
		},
	)
	options := testSourceOptions(t, []string{"dev"})
	options.HistoryHorizonWeeks = 1
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: options,
	}, &fakeProwMetadataClient{})
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	keys, err := controller.listKeys(context.Background())
	if err != nil {
		t.Fatalf("listKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "dev|"+recentURL {
		t.Fatalf("unexpected keys: got=%v want=[%q]", keys, "dev|"+recentURL)
	}
}

func TestSourceProwMetadataMarksMissingAfterRetryWindow(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/missing"
	firstCheckedAt := time.Now().UTC().Add(-2 * time.Hour)
	store := newFakeRunStore(contracts.RunRecord{
		Environment:                  "dev",
		RunURL:                       runURL,
		JobName:                      "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		OccurredAt:                   time.Now().UTC().Format(time.RFC3339Nano),
		RegionMetadataState:          contracts.RunRegionMetadataStatePending,
		RegionMetadataFirstCheckedAt: firstCheckedAt.Format(time.RFC3339Nano),
		RegionMetadataCheckedAt:      firstCheckedAt.Format(time.RFC3339Nano),
	})
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{
			Outcome:     prowartifacts.ArtifactOutcomeFound,
			StartedAt:   "2026-09-18T08:00:00Z",
			CompletedAt: "2026-09-18T09:00:00Z",
		},
		regionResult: prowartifacts.RegionResult{Outcome: prowartifacts.ArtifactOutcomeMissing},
	})
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}
	controller.artifactRetryWindow = time.Hour

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.RegionMetadataState != contracts.RunRegionMetadataStateMissing {
		t.Fatalf("unexpected metadata state: got=%q want=%q", run.RegionMetadataState, contracts.RunRegionMetadataStateMissing)
	}
	if run.RegionMetadataFirstCheckedAt != firstCheckedAt.Format(time.RFC3339Nano) {
		t.Fatalf("expected first-check timestamp to remain stable, got=%q", run.RegionMetadataFirstCheckedAt)
	}
	checkedAt, ok := parseTimestamp(run.RegionMetadataCheckedAt)
	if !ok || !checkedAt.After(firstCheckedAt) {
		t.Fatalf("expected last-check timestamp to advance, got=%q", run.RegionMetadataCheckedAt)
	}
}

func TestSourceProwMetadataStoresTimingForRunWithoutRegionSupport(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/logs/periodic-ci-Azure-ARO-HCP-main-images/2100878007617982464"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "periodic-ci-Azure-ARO-HCP-main-images",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	client := &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{
			Outcome:     prowartifacts.ArtifactOutcomeFound,
			StartedAt:   "2026-09-18T10:19:12Z",
			CompletedAt: "2026-09-18T12:27:55Z",
		},
	}
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, client)
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.TimingMetadataState != contracts.RunTimingMetadataStateFound {
		t.Fatalf("unexpected timing metadata state: %+v", run)
	}
	if client.regionCalls != 0 {
		t.Fatalf("expected no region artifact request for unsupported job, got=%d", client.regionCalls)
	}
}

func TestSourceProwMetadataKeepsIncompleteTimingPendingDuringRetryWindow(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/logs/periodic-ci-Azure-ARO-HCP-main-images/2100878007617982465"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "periodic-ci-Azure-ARO-HCP-main-images",
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	controller, err := newSourceProwMetadataController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, &fakeProwMetadataClient{
		timingResult: prowartifacts.TimingResult{
			Outcome:   prowartifacts.ArtifactOutcomeFound,
			StartedAt: "2026-09-18T10:19:12Z",
		},
	})
	if err != nil {
		t.Fatalf("new source prow metadata controller: %v", err)
	}
	controller.artifactRetryWindow = time.Hour

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	run, _ := store.GetStoredRun("dev", runURL)
	if run.TimingMetadataState != contracts.RunTimingMetadataStatePending || run.StartedAt == "" || run.CompletedAt != "" {
		t.Fatalf("expected incomplete timing to remain pending, got=%+v", run)
	}
}

func newFakeRunStore(runs ...contracts.RunRecord) *fakeProwRunsStore {
	store := &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}
	for _, run := range runs {
		store.runs[store.runKey(run.Environment, run.RunURL)] = run
	}
	return store
}

type fakeProwMetadataClient struct {
	timingResult prowartifacts.TimingResult
	timingErr    error
	regionResult prowartifacts.RegionResult
	regionErr    error
	timingCalls  int
	regionCalls  int
}

type blockingProwMetadataClient struct {
	timingStarted chan struct{}
	regionStarted chan struct{}
	release       chan struct{}
}

func (f *blockingProwMetadataClient) GetRunTiming(_ context.Context, _ string) (prowartifacts.TimingResult, error) {
	f.timingStarted <- struct{}{}
	<-f.release
	return prowartifacts.TimingResult{
		Outcome:     prowartifacts.ArtifactOutcomeFound,
		StartedAt:   "2026-09-18T10:19:12Z",
		CompletedAt: "2026-09-18T12:27:55Z",
	}, nil
}

func (f *blockingProwMetadataClient) GetRunRegion(_ context.Context, _ string, _ string) (prowartifacts.RegionResult, error) {
	f.regionStarted <- struct{}{}
	<-f.release
	return prowartifacts.RegionResult{
		Outcome: prowartifacts.ArtifactOutcomeFound,
		Region:  "westus3",
	}, nil
}

func (f *fakeProwMetadataClient) GetRunTiming(_ context.Context, _ string) (prowartifacts.TimingResult, error) {
	f.timingCalls++
	return f.timingResult, f.timingErr
}

func (f *fakeProwMetadataClient) GetRunRegion(_ context.Context, _ string, _ string) (prowartifacts.RegionResult, error) {
	f.regionCalls++
	return f.regionResult, f.regionErr
}
