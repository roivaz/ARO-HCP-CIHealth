// Package steps classifies ARO-HCP CI ci-operator step-graph steps as strict
// (a failure fails the job) or best-effort (a failure is tolerated). The step
// registry does not expose this at runtime: neither junit_operator.xml nor
// ci-operator-step-graph.json carry the best_effort flag, so the classification
// is vendored here from the openshift/release step-registry aro-hcp workflows.
//
// Source of truth (openshift/release, ci-operator/step-registry/aro-hcp):
//   - <workflow>/aro-hcp-<workflow>-workflow.yaml  (pre/test/post composition,
//     allow_best_effort_post_steps: true)
//   - <step>/aro-hcp-<step>-ref.yaml               (best_effort flag; default false)
//
// A step is strict unless its ref is explicitly best_effort:true. Unknown steps
// (not present in the vendored workflow) are treated as strict so that operator
// -fallback ingestion never silently drops a real failure.
package steps

import "strings"

// Workflow identifies an aro-hcp multi-stage test workflow in the step registry.
type Workflow string

const (
	// WorkflowLocalE2E provisions an environment and runs the local e2e suite.
	// Used by the dev pull-ci-Azure-ARO-HCP-main-e2e-parallel job.
	WorkflowLocalE2E Workflow = "local-e2e"
	// WorkflowPersistentE2E runs the e2e suite against a persistent environment
	// (no provision/deprovision). Used by int/stg/prod periodic e2e jobs.
	WorkflowPersistentE2E Workflow = "persistent-e2e"
	// WorkflowE2E runs the e2e suite against an existing environment without
	// lease management.
	WorkflowE2E Workflow = "e2e"
)

// workflowDefinition captures the vendored step composition and best-effort set
// for one workflow. Only the best-effort subset drives strictness; the ordered
// phase lists are retained for the drift-guard test and documentation.
type workflowDefinition struct {
	pre        []string
	test       []string
	post       []string
	bestEffort map[string]bool
}

// allSteps returns every step ref declared by the workflow, in pre/test/post
// order. Used by the drift-guard test to compare against the live registry.
func (w workflowDefinition) allSteps() []string {
	out := make([]string, 0, len(w.pre)+len(w.test)+len(w.post))
	out = append(out, w.pre...)
	out = append(out, w.test...)
	out = append(out, w.post...)
	return out
}

// workflowDefinitions is the vendored strictness data. Keep in sync with the
// openshift/release aro-hcp workflows (see the drift-guard test).
var workflowDefinitions = map[Workflow]workflowDefinition{
	WorkflowLocalE2E: {
		pre:  []string{"aro-hcp-lease-acquire", "aro-hcp-write-config", "aro-hcp-provision-environment"},
		test: []string{"aro-hcp-test-local"},
		post: []string{
			"aro-hcp-deprovision-tracked-resource-groups",
			"aro-hcp-gather-provision-failure",
			"aro-hcp-gather-visualization",
			"aro-hcp-gather-test-visualization",
			"aro-hcp-gather-custom-link-tools",
			"aro-hcp-gather-observability",
			"aro-hcp-gather-snapshot",
			"aro-hcp-deprovision-environment",
			"aro-hcp-lease-release",
		},
		bestEffort: map[string]bool{
			"aro-hcp-deprovision-tracked-resource-groups": true,
			"aro-hcp-gather-provision-failure":            true,
			"aro-hcp-gather-visualization":                true,
			"aro-hcp-gather-test-visualization":           true,
			"aro-hcp-gather-custom-link-tools":            true,
		},
	},
	WorkflowPersistentE2E: {
		pre:  []string{"aro-hcp-lease-acquire", "aro-hcp-write-config"},
		test: []string{"aro-hcp-test-persistent"},
		post: []string{
			"aro-hcp-gather-test-visualization",
			"aro-hcp-gather-custom-link-tools",
			"aro-hcp-lease-release",
		},
		bestEffort: map[string]bool{
			"aro-hcp-gather-test-visualization": true,
			"aro-hcp-gather-custom-link-tools":  true,
		},
	},
	WorkflowE2E: {
		pre:  []string{"aro-hcp-write-config"},
		test: []string{"aro-hcp-test-persistent"},
		post: []string{
			"aro-hcp-gather-test-visualization",
			"aro-hcp-gather-custom-link-tools",
		},
		bestEffort: map[string]bool{
			"aro-hcp-gather-test-visualization": true,
			"aro-hcp-gather-custom-link-tools":  true,
		},
	},
}

// workflowByEnvironment maps a CIHealth environment to the workflow its tracked
// e2e-parallel job runs. dev provisions on demand (local-e2e); int/stg/prod run
// against persistent environments.
var workflowByEnvironment = map[string]Workflow{
	"dev":  WorkflowLocalE2E,
	"int":  WorkflowPersistentE2E,
	"stg":  WorkflowPersistentE2E,
	"prod": WorkflowPersistentE2E,
}

// WorkflowForEnvironment returns the workflow associated with an environment.
func WorkflowForEnvironment(environment string) (Workflow, bool) {
	workflow, ok := workflowByEnvironment[normalizeEnvironment(environment)]
	return workflow, ok
}

// IsStrictStep reports whether a failure of stepRef in the given environment's
// workflow should be treated as job-relevant (strict). Best-effort steps return
// false. Unknown environments and unknown steps default to strict so that
// operator-fallback ingestion never drops a real failure.
func IsStrictStep(environment string, stepRef string) bool {
	normalizedStep := normalizeStepRef(stepRef)
	if normalizedStep == "" {
		return false
	}
	workflow, ok := workflowByEnvironment[normalizeEnvironment(environment)]
	if !ok {
		return true
	}
	definition, ok := workflowDefinitions[workflow]
	if !ok {
		return true
	}
	return !definition.bestEffort[normalizedStep]
}

// StepRefFromPod derives the step ref from a multi-stage pod/case name by
// stripping the leading "<test>-" prefix that ci-operator prepends (e.g.
// "e2e-parallel-aro-hcp-test-local" -> "aro-hcp-test-local"). The aro-hcp prefix
// is the stable anchor across jobs.
func StepRefFromPod(podName string) string {
	trimmed := normalizeStepRef(podName)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "aro-hcp-"); idx >= 0 {
		return trimmed[idx:]
	}
	return trimmed
}

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeStepRef(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
