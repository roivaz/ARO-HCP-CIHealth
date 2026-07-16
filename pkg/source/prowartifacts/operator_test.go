package prowartifacts

import (
	"sort"
	"testing"
)

const operatorJUnitFixture = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="step graph" tests="9" failures="7">
    <testcase name="Build image aro-hcp-backend from the repository"></testcase>
    <testcase name="Build image aro-hcp-frontend from the repository">
      <failure message="">error occurred handling build aro-hcp-frontend-amd64: the build aro-hcp-frontend-amd64 failed after 2m26s with reason DockerBuildFailed</failure>
    </testcase>
    <testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-provision-environment container test">
      <failure>raw provision log tail with ansi and timestamps 2026-07-14</failure>
    </testcase>
    <testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-test-local container test">
      <failure>raw test-local log tail exited with code 2</failure>
    </testcase>
    <testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-gather-observability container test">
      <failure>raw alerts gather log tail</failure>
    </testcase>
    <testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-gather-test-visualization container test">
      <failure>best-effort visualization log tail</failure>
    </testcase>
    <testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-deprovision-tracked-resource-groups container test">
      <failure>best-effort deprovision log tail</failure>
    </testcase>
    <testcase name="Run multi-stage test test phase">
      <failure>pod "e2e-parallel-aro-hcp-test-local" failed: Container test exited with code 2</failure>
    </testcase>
    <testcase name="Run multi-stage test post phase">
      <failure>pod "e2e-parallel-aro-hcp-gather-observability" failed</failure>
    </testcase>
  </testsuite>
</testsuites>`

func TestClassifyOperatorFailuresKeepsStrictStepsOnly(t *testing.T) {
	t.Parallel()

	got, err := classifyOperatorFailures("dev", "https://example.com/run/artifacts/junit_operator.xml", []byte(operatorJUnitFixture))
	if err != nil {
		t.Fatalf("classifyOperatorFailures returned error: %v", err)
	}

	type row struct {
		testName    string
		testSuite   string
		failureText string
		artifactURL string
	}
	gotRows := make([]row, 0, len(got))
	for _, failure := range got {
		gotRows = append(gotRows, row{
			testName:    failure.TestName,
			testSuite:   failure.TestSuite,
			failureText: failure.FailureText,
			artifactURL: failure.ArtifactURL,
		})
	}
	sort.Slice(gotRows, func(i, j int) bool { return gotRows[i].testName < gotRows[j].testName })

	want := []row{
		{
			testName:    "Build image aro-hcp-frontend from the repository",
			testSuite:   "step graph",
			failureText: `ci-operator step "Build image aro-hcp-frontend from the repository" failed`,
			artifactURL: "https://example.com/run/artifacts/junit_operator.xml",
		},
		{
			testName:    "aro-hcp-gather-observability",
			testSuite:   "step graph",
			failureText: `step "aro-hcp-gather-observability" container failed`,
			artifactURL: "https://example.com/run/artifacts/junit_operator.xml",
		},
		{
			testName:    "aro-hcp-provision-environment",
			testSuite:   "step graph",
			failureText: `step "aro-hcp-provision-environment" container failed`,
			artifactURL: "https://example.com/run/artifacts/junit_operator.xml",
		},
		{
			testName:    "aro-hcp-test-local",
			testSuite:   "step graph",
			failureText: `step "aro-hcp-test-local" container failed`,
			artifactURL: "https://example.com/run/artifacts/junit_operator.xml",
		},
	}

	if len(gotRows) != len(want) {
		t.Fatalf("unexpected row count: got=%d (%+v) want=%d", len(gotRows), gotRows, len(want))
	}
	for i := range want {
		if gotRows[i] != want[i] {
			t.Fatalf("row %d mismatch:\n got=%+v\nwant=%+v", i, gotRows[i], want[i])
		}
	}
}

func TestClassifyOperatorFailuresDropsPhaseAndBestEffort(t *testing.T) {
	t.Parallel()

	got, err := classifyOperatorFailures("dev", "artifacts/junit_operator.xml", []byte(operatorJUnitFixture))
	if err != nil {
		t.Fatalf("classifyOperatorFailures returned error: %v", err)
	}
	for _, failure := range got {
		switch failure.TestName {
		case "aro-hcp-gather-test-visualization", "aro-hcp-deprovision-tracked-resource-groups":
			t.Fatalf("best-effort step %q should have been dropped", failure.TestName)
		case "":
			t.Fatalf("phase-aggregate case produced an empty step ref row")
		}
	}
}

func TestClassifyOperatorFailuresEmptyOnNoFailures(t *testing.T) {
	t.Parallel()

	payload := `<testsuites><testsuite name="step graph" tests="1" failures="0"><testcase name="Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-test-local container test"></testcase></testsuite></testsuites>`
	got, err := classifyOperatorFailures("dev", "artifacts/junit_operator.xml", []byte(payload))
	if err != nil {
		t.Fatalf("classifyOperatorFailures returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no failures, got %+v", got)
	}
}
