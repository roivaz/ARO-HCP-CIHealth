package runlog

import (
	"strings"
	"testing"

	readmodelrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/runlog"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

func TestDayRunHistorySearchBoxAndScriptRender(t *testing.T) {
	t.Parallel()

	box := runLogDaySearchBoxHTML()
	if !strings.Contains(box, `id="run-log-search"`) {
		t.Fatalf("expected search input in box, got %q", box)
	}
	if !strings.Contains(box, `type="search"`) {
		t.Fatalf("expected search-type input, got %q", box)
	}
	if !strings.Contains(box, `id="run-log-search-status"`) {
		t.Fatalf("expected search status element, got %q", box)
	}
	if !strings.Contains(box, `id="run-log-search-empty"`) {
		t.Fatalf("expected empty-result message element, got %q", box)
	}

	script := runLogDaySearchScriptTag()
	if !strings.Contains(script, `getElementById("run-log-search")`) {
		t.Fatalf("expected search script to wire up the input, got %q", script)
	}
	if !strings.Contains(script, "tr.run-row") {
		t.Fatalf("expected search script to select run rows, got %q", script)
	}
}

func TestDayRunHistoryRunRowCarriesSearchClass(t *testing.T) {
	t.Parallel()

	rendered := runLogDayRunRowHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			JobName:     "periodic-ci",
			OccurredAt:  "2026-03-16T08:00:00Z",
			Failed:      true,
		},
	})
	if !strings.Contains(rendered, `<tr class="run-row">`) {
		t.Fatalf("expected run row to carry the run-row class for filtering, got %q", rendered)
	}
}

func TestDayRunHistoryFailureDetailsHTMLSkipsNonArtifactBackedFailures(t *testing.T) {
	t.Parallel()

	rendered := runLogDayFailureDetailsHTML(readmodelrunlog.JobHistoryRunRow{
		FailureRows: []readmodelrunlog.JobHistoryFailureRow{
			{
				FailureText:       "job failed and CFA synthesized a non-artifact-backed row",
				NonArtifactBacked: true,
			},
		},
	})
	if rendered != "" {
		t.Fatalf("expected no expander for non-artifact-backed-only row, got %q", rendered)
	}
}

func TestDayRunHistoryFailureDetailsHTMLRendersArtifactBackedFailures(t *testing.T) {
	t.Parallel()

	rendered := runLogDayFailureDetailsHTML(readmodelrunlog.JobHistoryRunRow{
		FailureRows: []readmodelrunlog.JobHistoryFailureRow{
			{
				FailureText:       "real junit-backed failure text",
				NonArtifactBacked: false,
			},
		},
	})
	if !strings.Contains(rendered, "unknown (1)") {
		t.Fatalf("expected lane category header for artifact-backed row, got %q", rendered)
	}
	if !strings.Contains(rendered, "<details class=\"lane-group\" open>") {
		t.Fatalf("expected single-failure category to render expanded, got %q", rendered)
	}
}

func TestDayRunHistoryFailureDetailsHTMLGroupsByLane(t *testing.T) {
	t.Parallel()

	rendered := runLogDayFailureDetailsHTML(readmodelrunlog.JobHistoryRunRow{
		FailureRows: []readmodelrunlog.JobHistoryFailureRow{
			{
				FailureText: "alert KubeAPIDown fired",
				Lane:        "alert",
			},
			{
				FailureText: "e2e test failed",
				Lane:        "e2e",
			},
		},
	})

	if !strings.Contains(rendered, "e2e (1)") {
		t.Fatalf("expected e2e category header, got %q", rendered)
	}
	if !strings.Contains(rendered, "alert (1)") {
		t.Fatalf("expected alert category header, got %q", rendered)
	}
	if e2eIdx, alertIdx := strings.Index(rendered, "e2e (1)"), strings.Index(rendered, "alert (1)"); e2eIdx < 0 || alertIdx < 0 || e2eIdx > alertIdx {
		t.Fatalf("expected e2e category to precede alert category, got %q", rendered)
	}
	if strings.Contains(rendered, "Failure details") {
		t.Fatalf("did not expect a single wrapping 'Failure details' expander, got %q", rendered)
	}
}

func TestDayRunHistoryFailureDetailsHTMLCollapsesMultiFailureCategory(t *testing.T) {
	t.Parallel()

	rendered := runLogDayFailureDetailsHTML(readmodelrunlog.JobHistoryRunRow{
		FailureRows: []readmodelrunlog.JobHistoryFailureRow{
			{FailureText: "first e2e failure", Lane: "e2e"},
			{FailureText: "second e2e failure", Lane: "e2e"},
		},
	})

	if !strings.Contains(rendered, "e2e (2)") {
		t.Fatalf("expected e2e category header with count 2, got %q", rendered)
	}
	if !strings.Contains(rendered, "<details class=\"lane-group\">") {
		t.Fatalf("expected multi-failure category to render collapsed, got %q", rendered)
	}
	if strings.Contains(rendered, "<details class=\"lane-group\" open>") {
		t.Fatalf("did not expect multi-failure category to be expanded by default, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLShowsRegressionIconForLikelyBadPR(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/run-1",
			PRNumber:    123,
			PRState:     "open",
			MergedPR:    false,
			Failed:      true,
		},
		SemanticRollups: readmodelrunlog.JobHistorySemanticRollups{
			ClusteredRows: 1,
		},
		BadPRScore:   3,
		BadPRReasons: []string{"post-good=0", "only seen in DEV", "only seen in one PR"},
	})
	if !strings.Contains(rendered, `class="signal-icon signal-regression"`) {
		t.Fatalf("expected regression signal icon in PR cell, got %q", rendered)
	}
	if !strings.Contains(rendered, "Likely regression") {
		t.Fatalf("expected Likely regression tooltip, got %q", rendered)
	}
	if !strings.Contains(rendered, "#123 (open)") {
		t.Fatalf("expected open PR label in PR cell, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLDoesNotUseRunLocalBadPRApproximation(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/run-1b",
			PRNumber:    123,
			PRState:     "open",
			MergedPR:    false,
			Failed:      true,
		},
		SemanticRollups: readmodelrunlog.JobHistorySemanticRollups{
			ClusteredRows: 1,
		},
	})
	if strings.Contains(rendered, `class="signal-icon signal-regression"`) {
		t.Fatalf("did not expect regression icon without weekly signature score, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLUsesMergedStateWhenMergedPR(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment:    "dev",
			RunURL:         "https://prow.example.com/view/run-2",
			PRNumber:       456,
			PRState:        "closed",
			MergedPR:       true,
			PostGoodCommit: true,
		},
	})
	if !strings.Contains(rendered, "#456 (merged)") {
		t.Fatalf("expected merged PR label in PR cell, got %q", rendered)
	}
	if strings.Contains(rendered, "#456 (closed)") {
		t.Fatalf("did not expect closed label for merged PR, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLUsesClosedStateWhenNotMerged(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment:    "int",
			RunURL:         "https://prow.example.com/view/run-3",
			PRNumber:       789,
			PRState:        "closed",
			MergedPR:       false,
			PostGoodCommit: true,
		},
	})
	if !strings.Contains(rendered, "#789 (closed)") {
		t.Fatalf("expected closed PR label in PR cell, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLDoesNotShowSignalIconForPassedRun(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/run-4",
			PRNumber:    321,
			PRState:     "open",
			Failed:      false,
		},
		SemanticRollups: readmodelrunlog.JobHistorySemanticRollups{
			ClusteredRows: 1,
		},
		BadPRScore:   3,
		BadPRReasons: []string{"post-good=0", "only seen in DEV", "only seen in one PR"},
	})
	if strings.Contains(rendered, `class="signal-icon`) {
		t.Fatalf("did not expect signal icon for passed run, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLDoesNotShowSignalIconForUnmatchedFailure(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/run-5",
			PRNumber:    654,
			PRState:     "open",
			Failed:      true,
		},
		SemanticRollups: readmodelrunlog.JobHistorySemanticRollups{
			ClusteredRows: 0,
			UnmatchedRows: 1,
		},
		BadPRScore:   3,
		BadPRReasons: []string{"post-good=0", "only seen in DEV", "only seen in one PR"},
	})
	if strings.Contains(rendered, `class="signal-icon`) {
		t.Fatalf("did not expect signal icon for unmatched-only failure, got %q", rendered)
	}
}

func TestDayRunHistoryPRHTMLShowsNewPatternIcon(t *testing.T) {
	t.Parallel()

	rendered := runLogDayPRHTML(readmodelrunlog.JobHistoryRunRow{
		Run: storecontracts.RunRecord{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/run-6",
			PRNumber:    999,
			PRState:     "open",
			Failed:      true,
		},
		SemanticRollups: readmodelrunlog.JobHistorySemanticRollups{
			ClusteredRows: 1,
		},
		FailureRows: []readmodelrunlog.JobHistoryFailureRow{
			{
				Lane: "e2e",
				SemanticAttachment: readmodelrunlog.JobHistorySemanticAttachment{
					Status:    "clustered",
					ClusterID: "fp-1",
				},
				PriorWeeksPresent: 0,
			},
		},
	})
	if !strings.Contains(rendered, `class="signal-icon signal-new"`) {
		t.Fatalf("expected new-pattern star icon in PR cell, got %q", rendered)
	}
	if !strings.Contains(rendered, "New failure pattern") {
		t.Fatalf("expected New failure pattern tooltip, got %q", rendered)
	}
}

func TestRunLogDayRunFlagsHTMLMarksBatchRuns(t *testing.T) {
	t.Parallel()

	batch := runLogDayRunFlagsHTML(storecontracts.RunRecord{
		Environment:    "dev",
		RunURL:         "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498",
		MergedPR:       true,
		PostGoodCommit: true,
	})
	if !strings.Contains(batch, ">batch<") {
		t.Fatalf("expected batch badge, got %q", batch)
	}
	if strings.Contains(batch, "post-good") || strings.Contains(batch, "merged PR") {
		t.Fatalf("did not expect post-good/merged PR badges on batch run, got %q", batch)
	}

	prCheck := runLogDayRunFlagsHTML(storecontracts.RunRecord{
		Environment:    "dev",
		RunURL:         "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		MergedPR:       true,
		PostGoodCommit: true,
	})
	if strings.Contains(prCheck, ">batch<") {
		t.Fatalf("did not expect batch badge on PR-check run, got %q", prCheck)
	}
	if !strings.Contains(prCheck, "post-good") || !strings.Contains(prCheck, "merged PR") {
		t.Fatalf("expected post-good and merged PR badges on PR-check run, got %q", prCheck)
	}
}
