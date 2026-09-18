package prowartifacts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestArtifactPrefixFromRunURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runURL string
		want   string
	}{
		{
			name:   "current prow URL",
			runURL: "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:   "legacy bucket remains unchanged",
			runURL: "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:   "legacy deck URL",
			runURL: "https://prow.ci.openshift.org/view/gcs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:   "gs URL",
			runURL: "gs://test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ArtifactPrefixFromRunURL(tt.runURL)
			if err != nil {
				t.Fatalf("ArtifactPrefixFromRunURL(%q): %v", tt.runURL, err)
			}
			if got != tt.want {
				t.Fatalf("ArtifactPrefixFromRunURL(%q) mismatch: got=%q want=%q", tt.runURL, got, tt.want)
			}
		})
	}
}

func TestIsBatchRunURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runURL string
		want   bool
	}{
		{
			name:   "batch prow URL",
			runURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498",
			want:   true,
		},
		{
			name:   "batch gs URL",
			runURL: "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498",
			want:   true,
		},
		{
			name:   "batch storage artifact URL",
			runURL: "https://storage.googleapis.com/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2074433538186285056/artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml",
			want:   true,
		},
		{
			name:   "pr check prow URL",
			runURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   false,
		},
		{
			name:   "pr check gs URL",
			runURL: "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   false,
		},
		{
			name:   "empty",
			runURL: "",
			want:   false,
		},
		{
			name:   "unsupported",
			runURL: "https://example.com/not/a/prow/url",
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsBatchRunURL(tt.runURL); got != tt.want {
				t.Fatalf("IsBatchRunURL(%q): got=%v want=%v", tt.runURL, got, tt.want)
			}
		})
	}
}

func TestCanonicalRunURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		deckBaseURL string
		runURL      string
		want        string
	}{
		{
			name:        "already canonical",
			deckBaseURL: "https://prow.ci.openshift.org",
			runURL:      "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "legacy bucket remains on legacy deck URL",
			deckBaseURL: "https://prow.ci.openshift.org",
			runURL:      "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "gs to deck",
			deckBaseURL: "https://prow.ci.openshift.org",
			runURL:      "gs://test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "prowjobs base path",
			deckBaseURL: "https://prow.ci.openshift.org/prowjobs.js",
			runURL:      "gs://test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CanonicalRunURL(tt.deckBaseURL, tt.runURL)
			if err != nil {
				t.Fatalf("CanonicalRunURL(%q, %q): %v", tt.deckBaseURL, tt.runURL, err)
			}
			if got != tt.want {
				t.Fatalf("CanonicalRunURL(%q, %q) mismatch: got=%q want=%q", tt.deckBaseURL, tt.runURL, got, tt.want)
			}
		})
	}
}

func TestHTTPClientListFailuresReturnsErrorWhenOneDeterministicPathFails(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCountByPath := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCountByPath[r.URL.Path]++
		mu.Unlock()

		switch r.URL.Path {
		case "/gcs/test-bucket/job/999/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml":
			_, _ = w.Write([]byte(`<testsuite name="entrypoint">
<testcase classname="entry.suite" name="entry-test">
	<failure message="entrypoint failed">infra step failed</failure>
</testcase>
</testsuite>`))
			return
		case "/gcs/test-bucket/job/999/prowjob_junit.xml":
			http.Error(w, "temporary upstream failure", http.StatusBadGateway)
			return
		case "/gcs/test-bucket/job/999/artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml":
			http.NotFound(w, r)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/999")
	if err == nil {
		t.Fatalf("expected error when one deterministic junit path fails, result=%v", result)
	}
	if !strings.Contains(err.Error(), "prowjob_junit.xml") {
		t.Fatalf("expected error to reference failing junit path, got=%v", err)
	}

	mu.Lock()
	prowjobRequests := requestCountByPath["/gcs/test-bucket/job/999/prowjob_junit.xml"]
	mu.Unlock()
	if prowjobRequests != 3 {
		t.Fatalf("expected 3 attempts for retryable gateway error, got=%d", prowjobRequests)
	}
}

func TestHTTPClientListFailuresTreatsHTMLAsNotFoundWithoutRetry(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCountByPath := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCountByPath[r.URL.Path]++
		mu.Unlock()

		switch r.URL.Path {
		case "/gcs/test-bucket/job/1001/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml":
			_, _ = w.Write([]byte(`<testsuite name="entrypoint">
<testcase classname="entry.suite" name="entry-test">
	<failure message="entrypoint failed">infra step failed</failure>
</testcase>
</testsuite>`))
			return
		case "/gcs/test-bucket/job/1001/prowjob_junit.xml":
			_, _ = w.Write([]byte(`<html><body>directory listing</body></html>`))
			return
		case "/gcs/test-bucket/job/1001/artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml":
			http.NotFound(w, r)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1001")
	if err != nil {
		t.Fatalf("expected HTML response to be treated as missing artifact, got err=%v", err)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected only entrypoint failure to be returned, got=%d", len(result.Failures))
	}

	mu.Lock()
	prowjobRequests := requestCountByPath["/gcs/test-bucket/job/1001/prowjob_junit.xml"]
	mu.Unlock()
	if prowjobRequests != 1 {
		t.Fatalf("expected no retries for HTML content, got requests=%d", prowjobRequests)
	}
}

func TestHTTPClientListFailuresTreatsUnparseableJUnitAsTerminalMissing(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCountByPath := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCountByPath[r.URL.Path]++
		mu.Unlock()

		switch r.URL.Path {
		case "/gcs/test-bucket/job/1002/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml":
			_, _ = w.Write([]byte(`<testsuite name="entrypoint">
<testcase classname="entry.suite" name="entry-test">
	<failure message="entrypoint failed">infra step failed</failure>
</testcase>
</testsuite>`))
			return
		case "/gcs/test-bucket/job/1002/prowjob_junit.xml":
			_, _ = w.Write([]byte(`<testsuite`))
			return
		case "/gcs/test-bucket/job/1002/artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml":
			http.NotFound(w, r)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1002")
	if err != nil {
		t.Fatalf("expected unparseable junit content to be treated as missing artifact, got err=%v", err)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected only entrypoint failure to be returned, got=%d", len(result.Failures))
	}

	mu.Lock()
	prowjobRequests := requestCountByPath["/gcs/test-bucket/job/1002/prowjob_junit.xml"]
	mu.Unlock()
	if prowjobRequests != 1 {
		t.Fatalf("expected no retries for unparseable junit content, got requests=%d", prowjobRequests)
	}
}

func TestHTTPClientListFailuresRetries429AndSucceeds(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCountByPath := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCountByPath[r.URL.Path]++
		count := requestCountByPath[r.URL.Path]
		mu.Unlock()

		switch r.URL.Path {
		case "/gcs/test-bucket/job/1000/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml":
			if count < 3 {
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`<testsuite name="entrypoint">
<testcase classname="entry.suite" name="entry-test">
	<failure message="entrypoint failed">infra step failed</failure>
</testcase>
</testsuite>`))
			return
		case "/gcs/test-bucket/job/1000/prowjob_junit.xml":
			http.NotFound(w, r)
			return
		case "/gcs/test-bucket/job/1000/artifacts/e2e-parallel/aro-hcp-test-local/artifacts/junit.xml":
			http.NotFound(w, r)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1000")
	if err != nil {
		t.Fatalf("expected retries to eventually succeed, got err=%v", err)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("unexpected failure count after retry success: got=%d want=1", len(result.Failures))
	}

	mu.Lock()
	entrypointRequests := requestCountByPath["/gcs/test-bucket/job/1000/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml"]
	mu.Unlock()
	if entrypointRequests != 3 {
		t.Fatalf("expected 3 attempts for retryable 429, got=%d", entrypointRequests)
	}
}

func TestHTTPClientFetchArtifactTreatsForbiddenAsTerminal(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.FetchArtifact(
		context.Background(),
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/forbidden",
		"artifact.txt",
	)
	if err != nil {
		t.Fatalf("FetchArtifact returned error for terminal forbidden response: %v", err)
	}
	if result.Outcome != ArtifactOutcomeForbidden {
		t.Fatalf("unexpected outcome: got=%q want=%q", result.Outcome, ArtifactOutcomeForbidden)
	}
	if requests != 1 {
		t.Fatalf("expected forbidden response not to be retried, got requests=%d", requests)
	}
}

func TestHTTPClientListFailuresReturnsForbiddenOutcome(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{
		ArtifactsBaseURL:  server.URL + "/gcs",
		DefaultJUnitPaths: []string{"first.xml", "second.xml"},
	})
	result, err := client.ListFailures(
		context.Background(),
		"unknown",
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/forbidden",
	)
	if err != nil {
		t.Fatalf("ListFailures returned error for terminal forbidden response: %v", err)
	}
	if result.Outcome != ArtifactOutcomeForbidden {
		t.Fatalf("unexpected outcome: got=%q want=%q", result.Outcome, ArtifactOutcomeForbidden)
	}
	if requests != 2 {
		t.Fatalf("expected each deterministic path to be attempted once without retries, got requests=%d", requests)
	}
}

func TestHTTPClientGetRunTimingParsesProwJobStatus(t *testing.T) {
	t.Parallel()

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": {
				"startTime": "2026-09-18T10:19:12Z",
				"completionTime": "2026-09-18T12:27:55Z"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.GetRunTiming(
		context.Background(),
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/complete",
	)
	if err != nil {
		t.Fatalf("GetRunTiming returned error: %v", err)
	}
	if requestedPath != "/gcs/test-bucket/job/complete/prowjob.json" {
		t.Fatalf("unexpected artifact path: got=%q", requestedPath)
	}
	if result.Outcome != ArtifactOutcomeFound {
		t.Fatalf("unexpected outcome: got=%q want=%q", result.Outcome, ArtifactOutcomeFound)
	}
	if result.StartedAt != "2026-09-18T10:19:12Z" || result.CompletedAt != "2026-09-18T12:27:55Z" {
		t.Fatalf("unexpected timing: started=%q completed=%q", result.StartedAt, result.CompletedAt)
	}
}

func TestHTTPClientGetRunTimingReturnsIncompleteProwJobAsFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":{"startTime":"2026-09-18T10:19:12Z"}}`))
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.GetRunTiming(
		context.Background(),
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/running",
	)
	if err != nil {
		t.Fatalf("GetRunTiming returned error: %v", err)
	}
	if result.Outcome != ArtifactOutcomeFound || result.StartedAt == "" || result.CompletedAt != "" {
		t.Fatalf("unexpected incomplete timing result: %+v", result)
	}
}

func TestHTTPClientGetRunTimingTreatsInvalidStatusAsInvalid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"status": {
				"startTime": "2026-09-18T12:27:55Z",
				"completionTime": "2026-09-18T10:19:12Z"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.GetRunTiming(
		context.Background(),
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/invalid",
	)
	if err != nil {
		t.Fatalf("GetRunTiming returned error: %v", err)
	}
	if result.Outcome != ArtifactOutcomeInvalid {
		t.Fatalf("unexpected outcome: got=%q want=%q", result.Outcome, ArtifactOutcomeInvalid)
	}
}

func TestParseRuntimeRegion(t *testing.T) {
	t.Parallel()

	payload := []byte("\x1b[37m[09:45:53.580]\x1b[0m INFO: Acquired slot and wrote shared artifacts \x1b[90m{\n" +
		`  "environment": "dev",` + "\n" +
		`  "runtimeRegion": "westus3",` + "\n" +
		`  "slotName": "aro-hcp-dev-shard1-slot-03"` + "\n" +
		"}\x1b[0m\n")

	region, err := parseRuntimeRegion(payload)
	if err != nil {
		t.Fatalf("parseRuntimeRegion returned error: %v", err)
	}
	if region != "westus3" {
		t.Fatalf("unexpected region: got=%q want=%q", region, "westus3")
	}
}

func TestHTTPClientGetRunRegionTreatsMalformedLogAsInvalid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("lease acquired without structured metadata"))
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(ClientOptions{ArtifactsBaseURL: server.URL + "/gcs"})
	result, err := client.GetRunRegion(
		context.Background(),
		"https://prow.ci.openshift.org/view/gs/test-bucket/job/invalid",
		"artifacts/e2e-parallel/aro-hcp-lease-acquire/build-log.txt",
	)
	if err != nil {
		t.Fatalf("GetRunRegion returned error: %v", err)
	}
	if result.Outcome != ArtifactOutcomeInvalid {
		t.Fatalf("unexpected outcome: got=%q want=%q", result.Outcome, ArtifactOutcomeInvalid)
	}
}
