package controllers

import (
	"testing"

	"ci-failure-atlas/pkg/store/contracts"
)

func TestExpectedRawFailureRowsWaitsForExplicitMissingArtifactMarker(t *testing.T) {
	t.Parallel()

	rows := expectedRawFailureRows(
		"dev",
		"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
		"2026-04-24T16:08:24Z",
		nil,
		true,
	)
	if len(rows) != 0 {
		t.Fatalf("expected no raw rows before terminal missing-artifact marker, got=%d", len(rows))
	}
}

func TestExpectedRawFailureRowsSynthesizesOnlyForMissingArtifactMarker(t *testing.T) {
	t.Parallel()

	rows := expectedRawFailureRows(
		"dev",
		"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
		"2026-04-24T16:08:24Z",
		[]contracts.ArtifactFailureRecord{
			{
				Environment: "dev",
				RunURL:      "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
				TestSuite:   artifactMissingMarkerTestSuite,
				TestName:    artifactMissingMarkerTestName,
			},
		},
		true,
	)
	if len(rows) != 1 {
		t.Fatalf("expected one synthetic raw row for missing-artifact marker, got=%d", len(rows))
	}
	if !rows[0].NonArtifactBacked {
		t.Fatalf("expected missing-artifact marker to synthesize a non-artifact-backed row")
	}
	if rows[0].RawText != rawFailureSyntheticText {
		t.Fatalf("unexpected synthetic raw text: got=%q want=%q", rows[0].RawText, rawFailureSyntheticText)
	}
}

func TestExpectedRawFailureRowsPrefersArtifactBackedRowsOverMarker(t *testing.T) {
	t.Parallel()

	rows := expectedRawFailureRows(
		"dev",
		"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
		"2026-04-24T16:08:24Z",
		[]contracts.ArtifactFailureRecord{
			{
				Environment:   "dev",
				ArtifactRowID: "row-1",
				RunURL:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
				TestSuite:     "suite",
				TestName:      "test",
				SignatureID:   "sig-1",
				FailureText:   "boom",
			},
			{
				Environment: "dev",
				RunURL:      "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4513/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2047709250477625344",
				TestSuite:   artifactMissingMarkerTestSuite,
				TestName:    artifactMissingMarkerTestName,
			},
		},
		true,
	)
	if len(rows) != 1 {
		t.Fatalf("expected artifact-backed rows to win over synthetic fallback, got=%d", len(rows))
	}
	if rows[0].NonArtifactBacked {
		t.Fatalf("expected artifact-backed row, got synthetic row")
	}
	if rows[0].TestName != "test" {
		t.Fatalf("unexpected artifact-backed row selected: got test=%q", rows[0].TestName)
	}
}
