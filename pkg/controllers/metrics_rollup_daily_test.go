package controllers

import (
	"context"
	"testing"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestIsMetricPostGoodRunCountsBatchRuns(t *testing.T) {
	t.Parallel()

	const (
		batchRunURL   = "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498"
		prCheckRunURL = "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488"
	)

	store := &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}
	// Batch run whose PR-based signal is NOT post-good; it should still count.
	store.runs[store.runKey("dev", batchRunURL)] = contracts.RunRecord{
		Environment:    "dev",
		RunURL:         batchRunURL,
		PostGoodCommit: false,
	}
	// PR-check run that is not post-good stays not-post-good.
	store.runs[store.runKey("dev", prCheckRunURL)] = contracts.RunRecord{
		Environment:    "dev",
		RunURL:         prCheckRunURL,
		PostGoodCommit: false,
	}

	tests := []struct {
		name   string
		runURL string
		want   bool
	}{
		{name: "batch run overrides non-post-good stored signal", runURL: batchRunURL, want: true},
		{name: "pr-check run without post-good stays false", runURL: prCheckRunURL, want: false},
		{name: "batch run not present in store still counts", runURL: "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2074433538186285056", want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runCache := map[string]contracts.RunRecord{}
			runFoundCache := map[string]bool{}
			got, err := isMetricPostGoodRun(context.Background(), store, "dev", tt.runURL, runCache, runFoundCache)
			if err != nil {
				t.Fatalf("isMetricPostGoodRun(%q): %v", tt.runURL, err)
			}
			if got != tt.want {
				t.Fatalf("isMetricPostGoodRun(%q): got=%v want=%v", tt.runURL, got, tt.want)
			}
			// Second call exercises the cache path and must be stable.
			got2, err := isMetricPostGoodRun(context.Background(), store, "dev", tt.runURL, runCache, runFoundCache)
			if err != nil {
				t.Fatalf("isMetricPostGoodRun(%q) cached: %v", tt.runURL, err)
			}
			if got2 != tt.want {
				t.Fatalf("isMetricPostGoodRun(%q) cached: got=%v want=%v", tt.runURL, got2, tt.want)
			}
		})
	}
}
