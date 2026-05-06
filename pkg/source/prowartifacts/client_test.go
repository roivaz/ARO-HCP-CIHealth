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
			name:   "prow URL",
			runURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:   "gcsweb URL",
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
			runURL: "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:   "test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
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
			runURL:      "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "gcsweb to deck",
			deckBaseURL: "https://prow.ci.openshift.org",
			runURL:      "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "gs to deck",
			deckBaseURL: "https://prow.ci.openshift.org",
			runURL:      "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
		},
		{
			name:        "prowjobs base path",
			deckBaseURL: "https://prow.ci.openshift.org/prowjobs.js",
			runURL:      "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			want:        "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4062/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
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
	failures, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/999")
	if err == nil {
		t.Fatalf("expected error when one deterministic junit path fails, failures=%v", failures)
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
	failures, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1001")
	if err != nil {
		t.Fatalf("expected HTML response to be treated as missing artifact, got err=%v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("expected only entrypoint failure to be returned, got=%d", len(failures))
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
	failures, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1002")
	if err != nil {
		t.Fatalf("expected unparseable junit content to be treated as missing artifact, got err=%v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("expected only entrypoint failure to be returned, got=%d", len(failures))
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
	failures, err := client.ListFailures(context.Background(), "dev", "https://prow.ci.openshift.org/view/gs/test-bucket/job/1000")
	if err != nil {
		t.Fatalf("expected retries to eventually succeed, got err=%v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("unexpected failure count after retry success: got=%d want=1", len(failures))
	}

	mu.Lock()
	entrypointRequests := requestCountByPath["/gcs/test-bucket/job/1000/artifacts/e2e-parallel/aro-hcp-provision-environment/artifacts/junit_entrypoint.xml"]
	mu.Unlock()
	if entrypointRequests != 3 {
		t.Fatalf("expected 3 attempts for retryable 429, got=%d", entrypointRequests)
	}
}
