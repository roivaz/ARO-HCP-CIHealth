package lanes

import "testing"

func TestClassifyLane(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		environment string
		testSuite   string
		testName    string
		wantLane    Lane
	}{
		{
			name:        "dev provision by step graph regex",
			environment: "dev",
			testSuite:   "step graph",
			testName:    "Run pipeline step Microsoft.Azure.ARO.HCP.Region",
			wantLane:    LaneProvision,
		},
		{
			name:        "dev e2e by suite",
			environment: "dev",
			testSuite:   "rp-api-compat-all/parallel",
			testName:    "any",
			wantLane:    LaneE2E,
		},
		{
			name:        "int e2e by suite",
			environment: "int",
			testSuite:   "integration/parallel",
			testName:    "any",
			wantLane:    LaneE2E,
		},
		{
			name:        "dev step graph non aro test is unknown",
			environment: "dev",
			testSuite:   "step graph",
			testName:    "Run pipeline step Other.Service",
			wantLane:    LaneUnknown,
		},
		{
			name:        "unknown environment is unknown lane",
			environment: "qa",
			testSuite:   "integration/parallel",
			testName:    "any",
			wantLane:    LaneUnknown,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyLane(tc.environment, tc.testSuite, tc.testName)
			if got != tc.wantLane {
				t.Fatalf("unexpected lane: got=%q want=%q", got, tc.wantLane)
			}
		})
	}
}

func TestDeriveLane(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		environment  string
		artifactPath string
		testSuite    string
		testName     string
		wantLane     Lane
	}{
		{
			name:         "alert artifact path overrides suite classification",
			environment:  "dev",
			artifactPath: "artifacts/e2e-parallel/aro-hcp-gather-observability/artifacts/junit_alerts.xml",
			testSuite:    "rp-api-compat-all/parallel",
			testName:     "[aro-hcp-observability] [hcp] alert SomeAlert does not fire",
			wantLane:     LaneAlert,
		},
		{
			name:         "alert artifact path with full url",
			environment:  "dev",
			artifactPath: "https://example.com/gcs/bucket/run/artifacts/junit_alerts.xml",
			testSuite:    "aro-hcp-tests",
			testName:     "anything",
			wantLane:     LaneAlert,
		},
		{
			name:         "alert artifact path with query string",
			environment:  "dev",
			artifactPath: "https://example.com/junit_alerts.xml?download=1",
			testSuite:    "aro-hcp-tests",
			testName:     "anything",
			wantLane:     LaneAlert,
		},
		{
			name:         "non-alert artifact falls back to suite classification",
			environment:  "dev",
			artifactPath: "artifacts/e2e-parallel/junit_e2e.xml",
			testSuite:    "rp-api-compat-all/parallel",
			testName:     "any",
			wantLane:     LaneE2E,
		},
		{
			name:         "empty artifact path falls back to suite classification",
			environment:  "dev",
			artifactPath: "",
			testSuite:    "step graph",
			testName:     "Run pipeline step Microsoft.Azure.ARO.HCP.Region",
			wantLane:     LaneProvision,
		},
		{
			name:         "unclassified non-alert is unknown",
			environment:  "dev",
			artifactPath: "artifacts/e2e-parallel/junit_e2e.xml",
			testSuite:    "step graph",
			testName:     "Run pipeline step Other.Service",
			wantLane:     LaneUnknown,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveLane(tc.environment, tc.artifactPath, tc.testSuite, tc.testName)
			if got != tc.wantLane {
				t.Fatalf("unexpected lane: got=%q want=%q", got, tc.wantLane)
			}
		})
	}
}

func TestIsAlertArtifactPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		want bool
	}{
		{name: "bare filename", path: "junit_alerts.xml", want: true},
		{name: "suffix match", path: "a/b/c/junit_alerts.xml", want: true},
		{name: "case insensitive", path: "A/B/JUNIT_ALERTS.XML", want: true},
		{name: "with fragment", path: "a/junit_alerts.xml#frag", want: true},
		{name: "empty", path: "", want: false},
		{name: "different file", path: "a/junit_e2e.xml", want: false},
		{name: "substring not suffix", path: "junit_alerts.xml.bak", want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isAlertArtifactPath(tc.path); got != tc.want {
				t.Fatalf("isAlertArtifactPath(%q): got=%v want=%v", tc.path, got, tc.want)
			}
		})
	}
}

func TestFiltersForEnvironment(t *testing.T) {
	t.Parallel()

	filters, ok := FiltersForEnvironment("dev")
	if !ok {
		t.Fatalf("expected filters for dev")
	}
	if len(filters) != 2 {
		t.Fatalf("unexpected filter count for dev: got=%d want=2", len(filters))
	}
	if filters[0].TestSuite != "rp-api-compat-all/parallel" {
		t.Fatalf("unexpected first suite filter: got=%q", filters[0].TestSuite)
	}
	if filters[1].TestSuite != "step graph" || filters[1].TestNameRegex == "" {
		t.Fatalf("unexpected second filter: %+v", filters[1])
	}

	if _, ok := FiltersForEnvironment("unknown"); ok {
		t.Fatalf("expected no filters for unknown environment")
	}
}
