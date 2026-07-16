package prowartifacts

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/steps"
)

// operatorArtifactPath is the ci-operator step-graph junit, used as a low
// -precedence fallback source when no higher-precedence per-step junit produced
// any failure for a failed run.
const operatorArtifactPath = "artifacts/junit_operator.xml"

// operatorStepGraphSuite is the junit suite name ci-operator uses for the step
// graph. It is preserved on synthesized fallback rows for provenance.
const operatorStepGraphSuite = "step graph"

var (
	// reOperatorContainerCase matches a per-step container testcase name, e.g.
	// "Run multi-stage test e2e-parallel - e2e-parallel-aro-hcp-test-local container test".
	// Group 1 is the pod name.
	reOperatorContainerCase = regexp.MustCompile(`(?i)^run multi-stage test .+? - (.+?) container test$`)

	// reMultiStageAggregate matches the non-attributable multi-stage aggregate
	// cases (per-phase and overall test) that must be dropped, e.g.
	// "Run multi-stage test e2e-parallel - e2e-parallel-pre phase" or
	// "Run multi-stage test e2e-parallel". These are covered by the per-step
	// container cases and carry no attributable step of their own.
	reMultiStageAggregate = regexp.MustCompile(`(?i)^run multi-stage test `)
)

// ListOperatorFallbackFailures fetches artifacts/junit_operator.xml and returns
// synthesized failures for the strict (job-failing) steps that failed. It is
// intended as a fallback: callers use it only when higher-precedence per-step
// junits yielded no failures. A missing operator artifact yields no failures and
// no error.
func (c *HTTPClient) ListOperatorFallbackFailures(ctx context.Context, environment string, runURL string) ([]Failure, error) {
	prefix, err := ArtifactPrefixFromRunURL(runURL)
	if err != nil {
		return nil, err
	}

	artifactURL := c.artifactURL(prefix, operatorArtifactPath)
	contents, found, err := c.fetchArtifact(ctx, artifactURL)
	if err != nil {
		return nil, err
	}
	if !found {
		return []Failure{}, nil
	}

	return classifyOperatorFailures(environment, artifactURL, contents)
}

// classifyOperatorFailures parses a junit_operator.xml payload and returns one
// synthesized failure per failed strict step. Two kinds of failing cases are
// kept:
//
//   - Multi-stage step-registry container cases ("Run multi-stage test ... container
//     test"): kept only when the resolved step ref is strict (best-effort steps
//     and the multi-stage phase/overall aggregate cases are dropped).
//   - Native ci-operator pipeline cases (image builds, source clone/import, release
//     assembly, etc.): always strict — there is no best-effort concept at the
//     native pipeline level, so any such failure fails the job and is kept.
//
// The failure text is canonicalized to a low-entropy, step-scoped phrase so that
// repeated failures of the same step cluster into a single failure pattern (the
// high-entropy container log tail / build reason is intentionally not used as the
// fingerprinting input).
func classifyOperatorFailures(environment string, artifactURL string, contents []byte) ([]Failure, error) {
	cases, err := parseJUnitFailures(contents, artifactURL)
	if err != nil {
		return nil, err
	}

	out := make([]Failure, 0, len(cases))
	seen := map[string]struct{}{}
	for _, failureCase := range cases {
		name := strings.TrimSpace(failureCase.TestName)
		if name == "" {
			continue
		}

		if match := reOperatorContainerCase.FindStringSubmatch(name); match != nil {
			stepRef := steps.StepRefFromPod(match[1])
			if stepRef == "" {
				continue
			}
			if !steps.IsStrictStep(environment, stepRef) {
				continue
			}
			if _, exists := seen[stepRef]; exists {
				continue
			}
			seen[stepRef] = struct{}{}
			out = append(out, Failure{
				ArtifactURL: strings.TrimSpace(artifactURL),
				TestName:    stepRef,
				TestSuite:   operatorStepGraphSuite,
				FailureText: fmt.Sprintf("step %q container failed", stepRef),
			})
			continue
		}

		if reMultiStageAggregate.MatchString(name) {
			// Multi-stage phase/overall aggregate case: not attributable to a
			// specific step; the per-step container cases already cover it.
			continue
		}

		// Native ci-operator pipeline step (image build, source, release, etc.).
		// These have no best-effort variant, so a failure is always job-failing.
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, Failure{
			ArtifactURL: strings.TrimSpace(artifactURL),
			TestName:    name,
			TestSuite:   operatorStepGraphSuite,
			FailureText: fmt.Sprintf("ci-operator step %q failed", name),
		})
	}

	return out, nil
}
