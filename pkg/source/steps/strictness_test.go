package steps

import (
	"testing"
)

func TestIsStrictStep(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		environment string
		stepRef     string
		want        bool
	}{
		{name: "dev provision is strict", environment: "dev", stepRef: "aro-hcp-provision-environment", want: true},
		{name: "dev test-local is strict", environment: "dev", stepRef: "aro-hcp-test-local", want: true},
		{name: "dev alerts gather is strict (best_effort:false)", environment: "dev", stepRef: "aro-hcp-gather-observability", want: true},
		{name: "dev gather-snapshot is strict (default)", environment: "dev", stepRef: "aro-hcp-gather-snapshot", want: true},
		{name: "dev deprovision-environment is strict (default)", environment: "dev", stepRef: "aro-hcp-deprovision-environment", want: true},
		{name: "dev lease-release is strict (default)", environment: "dev", stepRef: "aro-hcp-lease-release", want: true},
		{name: "dev gather-test-visualization is best-effort", environment: "dev", stepRef: "aro-hcp-gather-test-visualization", want: false},
		{name: "dev gather-custom-link-tools is best-effort", environment: "dev", stepRef: "aro-hcp-gather-custom-link-tools", want: false},
		{name: "dev gather-provision-failure is best-effort", environment: "dev", stepRef: "aro-hcp-gather-provision-failure", want: false},
		{name: "dev gather-visualization is best-effort", environment: "dev", stepRef: "aro-hcp-gather-visualization", want: false},
		{name: "dev deprovision-tracked-resource-groups is best-effort", environment: "dev", stepRef: "aro-hcp-deprovision-tracked-resource-groups", want: false},
		{name: "case-insensitive env and step", environment: "DEV", stepRef: "ARO-HCP-Gather-Test-Visualization", want: false},
		{name: "prod persistent test is strict", environment: "prod", stepRef: "aro-hcp-test-persistent", want: true},
		{name: "int persistent gather is best-effort", environment: "int", stepRef: "aro-hcp-gather-custom-link-tools", want: false},
		{name: "unknown step defaults to strict", environment: "dev", stepRef: "aro-hcp-some-new-step", want: true},
		{name: "unknown environment defaults to strict", environment: "qa", stepRef: "aro-hcp-gather-test-visualization", want: true},
		{name: "empty step is not strict", environment: "dev", stepRef: "", want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsStrictStep(tc.environment, tc.stepRef); got != tc.want {
				t.Fatalf("IsStrictStep(%q, %q) = %v, want %v", tc.environment, tc.stepRef, got, tc.want)
			}
		})
	}
}

func TestStepRefFromPod(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		pod  string
		want string
	}{
		{name: "local e2e test pod", pod: "e2e-parallel-aro-hcp-test-local", want: "aro-hcp-test-local"},
		{name: "gather pod", pod: "e2e-parallel-aro-hcp-gather-observability", want: "aro-hcp-gather-observability"},
		{name: "already a ref", pod: "aro-hcp-provision-environment", want: "aro-hcp-provision-environment"},
		{name: "mixed case trimmed", pod: "  E2E-Parallel-ARO-HCP-Test-Local ", want: "aro-hcp-test-local"},
		{name: "no aro-hcp anchor returns normalized input", pod: "some-other-pod", want: "some-other-pod"},
		{name: "empty", pod: "", want: ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := StepRefFromPod(tc.pod); got != tc.want {
				t.Fatalf("StepRefFromPod(%q) = %q, want %q", tc.pod, got, tc.want)
			}
		})
	}
}

func TestWorkflowForEnvironment(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		environment string
		want        Workflow
		wantOK      bool
	}{
		{environment: "dev", want: WorkflowLocalE2E, wantOK: true},
		{environment: "int", want: WorkflowPersistentE2E, wantOK: true},
		{environment: "stg", want: WorkflowPersistentE2E, wantOK: true},
		{environment: "prod", want: WorkflowPersistentE2E, wantOK: true},
		{environment: " DEV ", want: WorkflowLocalE2E, wantOK: true},
		{environment: "qa", want: "", wantOK: false},
	}

	for _, tc := range testCases {
		got, ok := WorkflowForEnvironment(tc.environment)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Fatalf("WorkflowForEnvironment(%q) = (%q, %v), want (%q, %v)", tc.environment, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestBestEffortStepsAreDeclaredInWorkflow guards against a best-effort entry
// referencing a step that is not part of its workflow composition.
func TestBestEffortStepsAreDeclaredInWorkflow(t *testing.T) {
	t.Parallel()

	for workflow, definition := range workflowDefinitions {
		declared := map[string]bool{}
		for _, step := range definition.allSteps() {
			declared[step] = true
		}
		for step := range definition.bestEffort {
			if !declared[step] {
				t.Errorf("workflow %q: best-effort step %q is not in the pre/test/post composition", workflow, step)
			}
		}
	}
}
