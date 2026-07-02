package options

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
)

type EnvironmentDefaults struct {
	SippyRelease            string
	SippyJobNames           []string
	DeterministicJUnitPaths []string
	SupportsPRLookup        bool
}

type RuntimeDefaults struct {
	SippyBaseURL            string
	ProwBaseURL             string
	ProwArtifactsBaseURL    string
	SippyOrg                string
	SippyRepo               string
	GitHubRepoOwner         string
	GitHubRepoName          string
	HistoryHorizonWeeks     int
	ProwRecentWindow        time.Duration
	ProwArtifactRetryWindow time.Duration
	DefaultEnvironments     []string
	DefaultJUnitPaths       []string
	Environments            map[string]EnvironmentDefaults
}

var supportedEnvironmentOrder = []string{"dev", "int", "stg", "prod"}

var defaultRuntimeDefaults = RuntimeDefaults{
	SippyBaseURL:            "https://sippy.dptools.openshift.org",
	ProwBaseURL:             "https://prow.ci.openshift.org",
	ProwArtifactsBaseURL:    "https://storage.googleapis.com",
	SippyOrg:                "Azure",
	SippyRepo:               "ARO-HCP",
	GitHubRepoOwner:         "Azure",
	GitHubRepoName:          "ARO-HCP",
	HistoryHorizonWeeks:     4,
	ProwRecentWindow:        10 * time.Hour,
	ProwArtifactRetryWindow: 1 * time.Hour,
	DefaultEnvironments:     []string{"dev"},
	DefaultJUnitPaths: []string{
		"prowjob_junit.xml",
	},
	Environments: map[string]EnvironmentDefaults{
		"dev": {
			SippyRelease: "Presubmits",
			SippyJobNames: []string{
				"pull-ci-Azure-ARO-HCP-main-e2e-parallel",
			},
			DeterministicJUnitPaths: []string{
				"artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml",
				"artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml",
				"artifacts/e2e-parallel/aro-hcp-gather-observability/artifacts/junit_alerts.xml",
				"prowjob_junit.xml",
			},
			SupportsPRLookup: true,
		},
		"int": {
			SippyRelease: "aro-integration",
			SippyJobNames: []string{
				"periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel",
				"branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel",
			},
			DeterministicJUnitPaths: []string{
				"artifacts/integration-e2e-parallel/aro-hcp-test-persistent/artifacts/junit.xml",
				"prowjob_junit.xml",
			},
		},
		"stg": {
			SippyRelease: "aro-stage",
			SippyJobNames: []string{
				"periodic-ci-Azure-ARO-HCP-main-periodic-stage-e2e-parallel",
				"periodic-ci-Azure-ARO-HCP-main-periodic-stage-e2e-parallel-ocp-nightly",
				"branch-ci-Azure-ARO-HCP-main-e2e-stage-e2e-parallel",
			},
			DeterministicJUnitPaths: []string{
				"artifacts/stage-e2e-parallel/aro-hcp-test-persistent/artifacts/junit.xml",
				"artifacts/stage-e2e-parallel-ocp-nightly/aro-hcp-test-persistent/artifacts/junit.xml",
				"prowjob_junit.xml",
			},
		},
		"prod": {
			SippyRelease: "aro-production",
			SippyJobNames: []string{
				"periodic-ci-Azure-ARO-HCP-main-periodic-prod-e2e-parallel",
				"periodic-ci-Azure-ARO-HCP-main-periodic-prod-e2e-parallel-ocp-nightly",
				"branch-ci-Azure-ARO-HCP-main-e2e-prod-e2e-parallel",
			},
			DeterministicJUnitPaths: []string{
				"artifacts/prod-e2e-parallel/aro-hcp-test-persistent/artifacts/junit.xml",
				"artifacts/prod-e2e-parallel-ocp-nightly/aro-hcp-test-persistent/artifacts/junit.xml",
				"prowjob_junit.xml",
			},
		},
	},
}

func DefaultRuntimeDefaults() RuntimeDefaults {
	return cloneRuntimeDefaults(defaultRuntimeDefaults)
}

func SupportedEnvironments() []string {
	return append([]string(nil), supportedEnvironmentOrder...)
}

func EnvironmentDefaultsFor(environment string) (EnvironmentDefaults, bool) {
	defaults, ok := defaultRuntimeDefaults.Environments[normalizeEnvironmentName(environment)]
	if !ok {
		return EnvironmentDefaults{}, false
	}
	return cloneEnvironmentDefaults(defaults), true
}

func SippyJobNamesForEnvironment(environment string) (sets.Set[string], bool) {
	defaults, ok := EnvironmentDefaultsFor(environment)
	if !ok {
		return nil, false
	}
	jobNames := normalizedStringSet(defaults.SippyJobNames...)
	return jobNames, len(jobNames) > 0
}

// Prow discovery and Sippy queries share the same canonical job-name mapping.
func ProwJobNamesForEnvironment(environment string) (sets.Set[string], bool) {
	return SippyJobNamesForEnvironment(environment)
}

func SupportsPRLookupForEnvironment(environment string) bool {
	defaults, ok := EnvironmentDefaultsFor(environment)
	if !ok {
		return false
	}
	return defaults.SupportsPRLookup
}

func DefaultJUnitPaths() []string {
	return append([]string(nil), defaultRuntimeDefaults.DefaultJUnitPaths...)
}

func DeterministicJUnitPathsByEnvironment() map[string][]string {
	out := make(map[string][]string, len(defaultRuntimeDefaults.Environments))
	for environment, defaults := range defaultRuntimeDefaults.Environments {
		if len(defaults.DeterministicJUnitPaths) == 0 {
			continue
		}
		out[environment] = append([]string(nil), defaults.DeterministicJUnitPaths...)
	}
	return out
}

func DefaultGitHubRepoOwner() string {
	return defaultRuntimeDefaults.GitHubRepoOwner
}

func DefaultGitHubRepoName() string {
	return defaultRuntimeDefaults.GitHubRepoName
}

func normalizeEnvironmentName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneRuntimeDefaults(in RuntimeDefaults) RuntimeDefaults {
	out := in
	out.DefaultEnvironments = append([]string(nil), in.DefaultEnvironments...)
	out.DefaultJUnitPaths = append([]string(nil), in.DefaultJUnitPaths...)
	out.Environments = make(map[string]EnvironmentDefaults, len(in.Environments))
	for environment, defaults := range in.Environments {
		out.Environments[environment] = cloneEnvironmentDefaults(defaults)
	}
	return out
}

func cloneEnvironmentDefaults(in EnvironmentDefaults) EnvironmentDefaults {
	out := in
	out.SippyJobNames = append([]string(nil), in.SippyJobNames...)
	out.DeterministicJUnitPaths = append([]string(nil), in.DeterministicJUnitPaths...)
	return out
}

func normalizedStringSet(values ...string) sets.Set[string] {
	out := sets.New[string]()
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		out.Insert(normalized)
	}
	return out
}
