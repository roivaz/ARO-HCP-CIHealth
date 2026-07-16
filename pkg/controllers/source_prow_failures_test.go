package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

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

type fakeFailuresProwClient struct {
	granular      []prowartifacts.Failure
	operator      []prowartifacts.Failure
	listCalls     int
	operatorCalls int
}

func (f *fakeFailuresProwClient) ListFailures(_ context.Context, _ string, _ string) ([]prowartifacts.Failure, error) {
	f.listCalls++
	return append([]prowartifacts.Failure(nil), f.granular...), nil
}

func (f *fakeFailuresProwClient) ListOperatorFallbackFailures(_ context.Context, _ string, _ string) ([]prowartifacts.Failure, error) {
	f.operatorCalls++
	return append([]prowartifacts.Failure(nil), f.operator...), nil
}

type capturingFailuresStore struct {
	*fakeProwRunsStore
	upserted []contracts.ArtifactFailureRecord
}

func (s *capturingFailuresStore) UpsertArtifactFailures(_ context.Context, rows []contracts.ArtifactFailureRecord) error {
	s.upserted = append(s.upserted, rows...)
	return nil
}

func newFailuresTestController(t *testing.T, store contracts.Store, client prowartifacts.Client) *sourceProwFailuresController {
	t.Helper()
	opts := testSourceOptions(t, []string{"dev"})
	controller, err := newSourceProwFailuresController(logr.Discard(), Dependencies{
		Store:  store,
		Source: opts,
	}, client)
	if err != nil {
		t.Fatalf("newSourceProwFailuresController returned error: %v", err)
	}
	// Disable the retry-window so the missing-artifact marker is written
	// deterministically on the first pass instead of waiting.
	controller.artifactRetryWindow = 0
	return controller
}

func TestProcessKeyPrefersGranularFailuresOverOperatorFallback(t *testing.T) {
	store := &capturingFailuresStore{fakeProwRunsStore: &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}}
	client := &fakeFailuresProwClient{
		granular: []prowartifacts.Failure{{
			ArtifactURL: "https://example.com/run/artifacts/aro-hcp-test-local/junit.xml",
			TestName:    "some e2e test",
			TestSuite:   "rp/parallel",
			FailureText: "boom",
		}},
		operator: []prowartifacts.Failure{{
			ArtifactURL: "https://example.com/run/artifacts/junit_operator.xml",
			TestName:    "aro-hcp-provision-environment",
			TestSuite:   "step graph",
			FailureText: `step "aro-hcp-provision-environment" container failed`,
		}},
	}

	controller := newFailuresTestController(t, store, client)
	if err := controller.processKey(context.Background(), "dev|https://example.com/run"); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	if client.operatorCalls != 0 {
		t.Fatalf("expected operator fallback not to be called, got %d calls", client.operatorCalls)
	}
	if len(store.upserted) != 1 || store.upserted[0].TestName != "some e2e test" {
		t.Fatalf("expected 1 granular row, got %#v", store.upserted)
	}
}

func TestProcessKeyUsesOperatorFallbackWhenNoGranularFailures(t *testing.T) {
	store := &capturingFailuresStore{fakeProwRunsStore: &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}}
	client := &fakeFailuresProwClient{
		granular: nil,
		operator: []prowartifacts.Failure{{
			ArtifactURL: "https://example.com/run/artifacts/junit_operator.xml",
			TestName:    "aro-hcp-provision-environment",
			TestSuite:   "step graph",
			FailureText: `step "aro-hcp-provision-environment" container failed`,
		}},
	}

	controller := newFailuresTestController(t, store, client)
	if err := controller.processKey(context.Background(), "dev|https://example.com/run"); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	if client.operatorCalls != 1 {
		t.Fatalf("expected operator fallback to be called once, got %d", client.operatorCalls)
	}
	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 fallback row, got %#v", store.upserted)
	}
	if store.upserted[0].TestName != "aro-hcp-provision-environment" {
		t.Fatalf("expected operator step row, got %#v", store.upserted[0])
	}
	if store.upserted[0].TestSuite == artifactMissingMarkerTestSuite {
		t.Fatalf("expected a real fallback row, got missing-artifact marker")
	}
}

func TestProcessKeyWritesMarkerWhenOperatorFallbackAlsoEmpty(t *testing.T) {
	store := &capturingFailuresStore{fakeProwRunsStore: &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}}
	client := &fakeFailuresProwClient{granular: nil, operator: nil}

	controller := newFailuresTestController(t, store, client)
	if err := controller.processKey(context.Background(), "dev|https://example.com/run"); err != nil {
		t.Fatalf("processKey returned error: %v", err)
	}
	if client.operatorCalls != 1 {
		t.Fatalf("expected operator fallback to be attempted once, got %d", client.operatorCalls)
	}
	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 marker row, got %#v", store.upserted)
	}
	if store.upserted[0].TestSuite != artifactMissingMarkerTestSuite {
		t.Fatalf("expected missing-artifact marker, got %#v", store.upserted[0])
	}
}
