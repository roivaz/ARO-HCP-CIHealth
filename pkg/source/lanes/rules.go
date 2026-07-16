package lanes

import (
	"regexp"
	"strings"
)

type Lane string

const (
	LaneUnknown   Lane = "unknown"
	LaneProvision Lane = "provision"
	LaneE2E       Lane = "e2e"
	LaneAlert     Lane = "alert"
)

// SourceOther is the bucket assigned to failures whose lane is not one of the
// primary single-source lanes (provision/e2e/alert). Unknown/unclassified
// failures collapse into this bucket for grouping and filtering.
const SourceOther = "other"

// BucketSource maps a failure lane to the single source dimension used to group
// and filter failure patterns. Provision/e2e/alert map to themselves; every
// other lane (including unknown) collapses to "other" so it aggregates
// predictably.
func BucketSource(lane string) string {
	switch normalizeLane(Lane(strings.ToLower(strings.TrimSpace(lane)))) {
	case LaneProvision:
		return string(LaneProvision)
	case LaneE2E:
		return string(LaneE2E)
	case LaneAlert:
		return string(LaneAlert)
	default:
		return SourceOther
	}
}

// FilterableSources lists the source values that failure-pattern rows can be
// filtered by, in display order.
func FilterableSources() []string {
	return []string{
		string(LaneProvision),
		string(LaneE2E),
		string(LaneAlert),
		SourceOther,
	}
}

// alertJUnitArtifactSuffix identifies the JUnit artifact that carries alert
// "does not fire" assertions. Alert failures cannot be classified by test
// suite/name (the alert suite name collides with the e2e suite), so they are
// keyed off the originating artifact path instead.
const alertJUnitArtifactSuffix = "junit_alerts.xml"

// operatorJUnitArtifactSuffix identifies the ci-operator step-graph junit used
// as a fallback failure source. Its synthesized rows carry the step ref as the
// test name and cannot be classified by the suite/name rules, so they are lane
// -mapped from the step ref instead.
const operatorJUnitArtifactSuffix = "junit_operator.xml"

type TestFilter struct {
	TestSuite     string
	TestNameRegex string
}

type Rule struct {
	Filter TestFilter
	Lane   Lane
}

type compiledTestRule struct {
	testSuite     string
	testNameRegex *regexp.Regexp
	lane          Lane
}

// defaultRulesByEnvironment is the built-in lane/filter rule set shared by
// ingestion, metrics rollups, and semantic/report consumers.
var defaultRulesByEnvironment = map[string][]Rule{
	"dev": {
		{
			Filter: TestFilter{
				TestSuite: "rp-api-compat-all/parallel",
			},
			Lane: LaneE2E,
		},
		{
			Filter: TestFilter{
				TestSuite:     "step graph",
				TestNameRegex: `Microsoft\.Azure\.ARO\.HCP`,
			},
			Lane: LaneProvision,
		},
	},
	"int": {
		{
			Filter: TestFilter{
				TestSuite: "integration/parallel",
			},
			Lane: LaneE2E,
		},
	},
	"stg": {
		{
			Filter: TestFilter{
				TestSuite: "stage/parallel",
			},
			Lane: LaneE2E,
		},
	},
	"prod": {
		{
			Filter: TestFilter{
				TestSuite: "prod/parallel",
			},
			Lane: LaneE2E,
		},
	},
}

var compiledRulesByEnvironment = compileRulesByEnvironment(defaultRulesByEnvironment)

func DefaultRulesByEnvironment() map[string][]Rule {
	out := make(map[string][]Rule, len(defaultRulesByEnvironment))
	for environment, rules := range defaultRulesByEnvironment {
		cloned := make([]Rule, 0, len(rules))
		for _, rule := range rules {
			cloned = append(cloned, Rule{
				Filter: TestFilter{
					TestSuite:     strings.TrimSpace(rule.Filter.TestSuite),
					TestNameRegex: strings.TrimSpace(rule.Filter.TestNameRegex),
				},
				Lane: normalizeLane(rule.Lane),
			})
		}
		out[environment] = cloned
	}
	return out
}

func FiltersForEnvironment(environment string) ([]TestFilter, bool) {
	rules, ok := defaultRulesByEnvironment[normalizeEnvironment(environment)]
	if !ok || len(rules) == 0 {
		return nil, false
	}
	out := make([]TestFilter, 0, len(rules))
	for _, rule := range rules {
		out = append(out, TestFilter{
			TestSuite:     strings.TrimSpace(rule.Filter.TestSuite),
			TestNameRegex: strings.TrimSpace(rule.Filter.TestNameRegex),
		})
	}
	return out, true
}

// DeriveLane resolves the lane for a failure. Alert failures are identified by
// their originating JUnit artifact (the alert artifact), since their test
// suite/name cannot be distinguished from the e2e suite. Operator step-graph
// fallback failures are lane-mapped from their step ref. All other failures fall
// back to the suite/name classification rules.
func DeriveLane(environment string, artifactPath string, testSuite string, testName string) Lane {
	if isAlertArtifactPath(artifactPath) {
		return LaneAlert
	}
	if isOperatorArtifactPath(artifactPath) {
		return operatorStepLane(testName)
	}
	return ClassifyLane(environment, testSuite, testName)
}

func isAlertArtifactPath(artifactPath string) bool {
	return hasArtifactSuffix(artifactPath, alertJUnitArtifactSuffix)
}

func isOperatorArtifactPath(artifactPath string) bool {
	return hasArtifactSuffix(artifactPath, operatorJUnitArtifactSuffix)
}

func hasArtifactSuffix(artifactPath string, suffix string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(artifactPath))
	if trimmed == "" {
		return false
	}
	if idx := strings.IndexAny(trimmed, "?#"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return trimmed == suffix || strings.HasSuffix(trimmed, "/"+suffix)
}

// operatorStepLane maps an operator fallback step ref to a lane. Steps whose
// failure maps to an existing single-source lane are classified accordingly;
// every other strict step (build/merge/source/cleanup) is left unknown and
// collapses into the "other" bucket.
func operatorStepLane(stepRef string) Lane {
	ref := strings.ToLower(strings.TrimSpace(stepRef))
	switch {
	case strings.Contains(ref, "provision-environment"):
		return LaneProvision
	case strings.Contains(ref, "test-local"), strings.Contains(ref, "test-persistent"):
		return LaneE2E
	case strings.Contains(ref, "gather-observability"):
		return LaneAlert
	default:
		return LaneUnknown
	}
}

func ClassifyLane(environment string, testSuite string, testName string) Lane {
	rules, ok := compiledRulesByEnvironment[normalizeEnvironment(environment)]
	if !ok || len(rules) == 0 {
		return LaneUnknown
	}
	suite := strings.TrimSpace(testSuite)
	if suite == "" {
		return LaneUnknown
	}
	name := strings.TrimSpace(testName)
	for _, rule := range rules {
		if rule.testSuite != suite {
			continue
		}
		if rule.testNameRegex == nil {
			return rule.lane
		}
		if rule.testNameRegex.MatchString(name) {
			return rule.lane
		}
	}
	return LaneUnknown
}

func compileRulesByEnvironment(raw map[string][]Rule) map[string][]compiledTestRule {
	out := map[string][]compiledTestRule{}
	for environment, rules := range raw {
		normalizedEnvironment := normalizeEnvironment(environment)
		if normalizedEnvironment == "" {
			continue
		}
		compiled := make([]compiledTestRule, 0, len(rules))
		for _, rule := range rules {
			suite := strings.TrimSpace(rule.Filter.TestSuite)
			if suite == "" {
				continue
			}
			normalizedLane := normalizeLane(rule.Lane)
			if normalizedLane == LaneUnknown {
				continue
			}

			var testNameRegex *regexp.Regexp
			if pattern := strings.TrimSpace(rule.Filter.TestNameRegex); pattern != "" {
				testNameRegex = regexp.MustCompile(pattern)
			}
			compiled = append(compiled, compiledTestRule{
				testSuite:     suite,
				testNameRegex: testNameRegex,
				lane:          normalizedLane,
			})
		}
		if len(compiled) > 0 {
			out[normalizedEnvironment] = compiled
		}
	}
	return out
}

func normalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeLane(value Lane) Lane {
	switch strings.TrimSpace(string(value)) {
	case string(LaneProvision):
		return LaneProvision
	case string(LaneE2E):
		return LaneE2E
	case string(LaneAlert):
		return LaneAlert
	default:
		return LaneUnknown
	}
}
