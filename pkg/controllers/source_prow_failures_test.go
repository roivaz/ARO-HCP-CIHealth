package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestSourceProwFailuresTreatsForbiddenAsTerminal(t *testing.T) {
	t.Parallel()

	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2100878007617982464"
	store := newFakeRunStore(contracts.RunRecord{
		Environment: "dev",
		RunURL:      runURL,
		JobName:     "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		Failed:      true,
		OccurredAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	client := &fakeProwArtifactsClient{
		failuresResult: prowartifacts.FailureListResult{
			Outcome: prowartifacts.ArtifactOutcomeForbidden,
		},
	}
	controller, err := newSourceProwFailuresController(logr.Discard(), Dependencies{
		Store:  store,
		Source: testSourceOptions(t, []string{"dev"}),
	}, client)
	if err != nil {
		t.Fatalf("new source prow failures controller: %v", err)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("processKey returned error for terminal forbidden result: %v", err)
	}
	rows, err := store.ListArtifactFailuresByRun(context.Background(), "dev", runURL)
	if err != nil {
		t.Fatalf("list artifact failures: %v", err)
	}
	if len(rows) != 1 || rows[0].TestName != artifactMissingMarkerTestName {
		t.Fatalf("expected terminal missing-artifact marker, got=%+v", rows)
	}
	if client.listFailuresCalls != 1 {
		t.Fatalf("unexpected ListFailures calls: got=%d want=1", client.listFailuresCalls)
	}

	if err := controller.processKey(context.Background(), "dev|"+runURL); err != nil {
		t.Fatalf("second processKey returned error: %v", err)
	}
	if client.listFailuresCalls != 1 {
		t.Fatalf("expected stored terminal marker to suppress refetch, got calls=%d", client.listFailuresCalls)
	}
}

func TestShouldWriteMissingArtifactMarkerWaitsForRetryWindow(t *testing.T) {
	t.Parallel()

	store := &fakeCheckpointStore{
		checkpoints: map[string]contracts.CheckpointRecord{},
	}
	environment := "dev"
	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488"
	retryWindow := 10 * time.Minute
	start := mustParseRFC3339(t, "2026-04-20T10:00:00Z")

	shouldWrite, err := shouldWriteMissingArtifactMarker(context.Background(), store, retryWindow, environment, runURL, start)
	if err != nil {
		t.Fatalf("first retry decision returned error: %v", err)
	}
	if shouldWrite {
		t.Fatalf("expected first empty artifact fetch to defer marker write")
	}

	checkpointName := artifactRetryCheckpointName(environment, runURL)
	checkpoint, found, err := store.GetCheckpoint(context.Background(), checkpointName)
	if err != nil {
		t.Fatalf("get checkpoint after first decision: %v", err)
	}
	if !found {
		t.Fatalf("expected retry checkpoint to be stored")
	}
	if checkpoint.Value != start.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected first-seen checkpoint value: got=%q want=%q", checkpoint.Value, start.Format(time.RFC3339Nano))
	}

	shouldWrite, err = shouldWriteMissingArtifactMarker(context.Background(), store, retryWindow, environment, runURL, start.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("second retry decision returned error: %v", err)
	}
	if shouldWrite {
		t.Fatalf("expected retry window to keep deferring marker write")
	}

	shouldWrite, err = shouldWriteMissingArtifactMarker(context.Background(), store, retryWindow, environment, runURL, start.Add(11*time.Minute))
	if err != nil {
		t.Fatalf("third retry decision returned error: %v", err)
	}
	if !shouldWrite {
		t.Fatalf("expected marker write once retry window has elapsed")
	}
}

type fakeCheckpointStore struct {
	checkpoints map[string]contracts.CheckpointRecord
}

func (f *fakeCheckpointStore) UpsertCheckpoints(_ context.Context, rows []contracts.CheckpointRecord) error {
	for _, row := range rows {
		f.checkpoints[row.Name] = row
	}
	return nil
}

func (f *fakeCheckpointStore) GetCheckpoint(_ context.Context, name string) (contracts.CheckpointRecord, bool, error) {
	row, found := f.checkpoints[name]
	return row, found, nil
}

type fakeProwArtifactsClient struct {
	failuresResult    prowartifacts.FailureListResult
	failuresErr       error
	listFailuresCalls int
}

func (f *fakeProwArtifactsClient) FetchArtifact(_ context.Context, _ string, _ string) (prowartifacts.ArtifactResult, error) {
	return prowartifacts.ArtifactResult{}, nil
}

func (f *fakeProwArtifactsClient) ListFailures(_ context.Context, _ string, _ string) (prowartifacts.FailureListResult, error) {
	f.listFailuresCalls++
	return f.failuresResult, f.failuresErr
}

func (f *fakeProwArtifactsClient) GetRunRegion(_ context.Context, _ string, _ string) (prowartifacts.RegionResult, error) {
	return prowartifacts.RegionResult{}, nil
}

func (f *fakeProwArtifactsClient) GetRunTiming(_ context.Context, _ string) (prowartifacts.TimingResult, error) {
	return prowartifacts.TimingResult{}, nil
}
