package phase1

import (
	semanticcontracts "ci-failure-atlas/pkg/semantic/contracts"
	"strings"
	"testing"
)

func TestBuildFallbackSearchPhraseSkipsPlaceholderOnlyAssertionBlock(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/mise_routing.go:89]: Expected
    <string>: ...
to equal
    <string>: ...`

	got := buildFallbackSearchPhrase([]semanticcontracts.Phase1WorksetRecord{{RawText: raw}})
	if got != "" {
		t.Fatalf("expected placeholder-only assertion block to produce no fallback search phrase, got=%q", got)
	}
}

func TestFallbackSearchSourceUsesFailWrapperForPlaceholderOnlyAssertion(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/mise_routing.go:89]: Expected
    <string>: ...
to equal
    <string>: ...`

	runURL, signatureID, phrase, found := fallbackSearchSource([]semanticcontracts.Phase1WorksetRecord{{
		RunURL:      "https://prow.ci.example/view/1",
		SignatureID: "sig-1",
		RawText:     raw,
	}})
	if !found {
		t.Fatalf("expected fallback search source to find a literal wrapper line")
	}
	if runURL != "https://prow.ci.example/view/1" || signatureID != "sig-1" {
		t.Fatalf("unexpected search source metadata: runURL=%q signatureID=%q", runURL, signatureID)
	}
	if !strings.HasPrefix(strings.ToLower(phrase), "fail [github.com/azure/aro-hcp/test/e2e/mise_routing.go:89]: expected") {
		t.Fatalf("expected fallback search phrase to use the literal fail wrapper line, got=%q", phrase)
	}
}

func TestContextualizeCanonicalWithTestName(t *testing.T) {
	t.Parallel()

	timeoutGot := contextualizeCanonicalWithTestName("Timed out after <duration>s.", "Engineering should expose expected metrics")
	timeoutWant := "Engineering should expose expected metrics: Timed out after <duration>s."
	if timeoutGot != timeoutWant {
		t.Fatalf("unexpected timeout contextualization: got=%q want=%q", timeoutGot, timeoutWant)
	}

	assertionGot := contextualizeCanonicalWithTestName("assertion failed: expected values to equal", "MISE routing returns the versioned frontend")
	assertionWant := "MISE routing returns the versioned frontend: assertion failed: expected values to equal"
	if assertionGot != assertionWant {
		t.Fatalf("unexpected assertion contextualization: got=%q want=%q", assertionGot, assertionWant)
	}
}

func TestCompileDoesNotFlagSearchSourceNotFoundWhenFallbackSourceSucceeds(t *testing.T) {
	t.Parallel()

	testName := "MISE Routing routes to the correct frontend based on version header MISE v2 when x-ms-mise-version header is set"
	groupKey := buildGroupKey("int", "e2e", "periodic-ci", testName)
	workset := []semanticcontracts.Phase1WorksetRecord{{
		SchemaVersion: semanticcontracts.CurrentSchemaVersion,
		Environment:   "int",
		RowID:         "row-1",
		GroupKey:      groupKey,
		Lane:          "e2e",
		JobName:       "periodic-ci",
		TestName:      testName,
		TestSuite:     "e2e",
		SignatureID:   "sig-1",
		OccurredAt:    "2026-04-24T10:00:00Z",
		RunURL:        "https://prow.ci.example/view/1",
		RawText: `fail [github.com/Azure/ARO-HCP/test/e2e/mise_routing.go:89]: Expected
    <string>: ...
to equal
    <string>: ...`,
	}}
	assignments := []semanticcontracts.Phase1AssignmentRecord{{
		SchemaVersion:                    semanticcontracts.CurrentSchemaVersion,
		Environment:                      "int",
		RowID:                            "row-1",
		GroupKey:                         groupKey,
		Phase1LocalClusterKey:            "cluster-1",
		CanonicalEvidencePhraseCandidate: "assertion failed: expected values to equal",
		Confidence:                       "medium",
	}}

	clusters, reviewItems, err := Compile(workset, assignments)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected one cluster, got=%d", len(clusters))
	}
	cluster := clusters[0]
	if !strings.HasPrefix(strings.ToLower(cluster.SearchQueryPhrase), "fail [github.com/azure/aro-hcp/test/e2e/mise_routing.go:89]: expected") {
		t.Fatalf("expected fallback search source to provide a literal fail wrapper line, got=%q", cluster.SearchQueryPhrase)
	}
	for _, item := range reviewItems {
		if item.Reason == "search_query_source_not_found" {
			t.Fatalf("did not expect search_query_source_not_found when fallbackSearchSource succeeds: %+v", item)
		}
	}
}

func TestSeverityForReviewItem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason  string
		support int
		want    string
	}{
		{"likely_undermerged", 6, "high"},
		{"likely_undermerged", 2, "medium"},
		{"high_sample_variance", 5, "high"},
		{"high_sample_variance", 2, "medium"},
		{"ambiguous_provider_merge", 4, "high"},
		{"ambiguous_provider_merge", 1, "medium"},
		{"insufficient_inner_error", 5, "medium"},
		{"insufficient_inner_error", 1, "low"},
		{"low_confidence_evidence", 5, "medium"},
		{"low_confidence_evidence", 1, "low"},
		{"placeholder_dominated_canonical", 1, "medium"},
		{"short_uninformative_canonical", 1, "medium"},
		{"single_occurrence", 1, "low"},
		{"search_query_source_not_found", 1, "low"},
	}
	for _, tc := range cases {
		got := severityForReviewItem(tc.reason, tc.support, "some canonical")
		if got != tc.want {
			t.Errorf("severityForReviewItem(%q, %d): got=%q want=%q", tc.reason, tc.support, got, tc.want)
		}
	}
}

func TestIsPlaceholderDominatedCanonical(t *testing.T) {
	t.Parallel()

	dominated := "<cluster> <resource-group> <url> at failed"
	if !isPlaceholderDominatedCanonical(dominated) {
		t.Errorf("expected %q to be placeholder-dominated", dominated)
	}
	normal := "failed waiting for deployment in resource group <resource-group>"
	if isPlaceholderDominatedCanonical(normal) {
		t.Errorf("expected %q to NOT be placeholder-dominated", normal)
	}
}

func TestIsShortUninformativeCanonical(t *testing.T) {
	t.Parallel()

	short := []string{"failed", "error", "timeout"}
	for _, s := range short {
		if !isShortUninformativeCanonical(s) {
			t.Errorf("expected %q to be short/uninformative", s)
		}
	}
	ok := []string{"ERROR CODE: NotFound", "context deadline exceeded", "OAuth timeout waiting for response"}
	for _, s := range ok {
		if isShortUninformativeCanonical(s) {
			t.Errorf("expected %q to NOT be short/uninformative", s)
		}
	}
}

func TestNearDuplicateKeyStripsPlaceholders(t *testing.T) {
	t.Parallel()

	a := nearDuplicateKey("failed waiting for deployment in <resource-group>")
	b := nearDuplicateKey("failed waiting for deployment in <cluster>")
	if a != b {
		t.Errorf("expected placeholder-stripped keys to match:\n  a=%q\n  b=%q", a, b)
	}
}

func TestTokenSetJaccardOverlap(t *testing.T) {
	t.Parallel()

	if got := tokenSetJaccardOverlap("a b c d e", "a b c d f"); got < 0.60 || got > 0.90 {
		t.Errorf("expected moderate overlap, got=%f", got)
	}
	if got := tokenSetJaccardOverlap("a b c", "a b c"); got != 1.0 {
		t.Errorf("expected perfect overlap, got=%f", got)
	}
	if got := tokenSetJaccardOverlap("a b c", "x y z"); got != 0.0 {
		t.Errorf("expected zero overlap, got=%f", got)
	}
}

func TestIsKnownTerminalCanonical(t *testing.T) {
	t.Parallel()

	terminal := []string{
		"Interrupted by User",
		"interrupted by user",
		"INTERRUPTED BY USER",
		"Command Error: signal: killed",
		"command error: signal: killed",
	}
	for _, phrase := range terminal {
		if !isKnownTerminalCanonical(phrase) {
			t.Errorf("expected %q to be recognized as a known terminal canonical", phrase)
		}
	}

	nonTerminal := []string{
		"Command Error: exit status 1",
		"context deadline exceeded",
		"failure",
		"ERROR CODE: DeploymentFailed; provider Microsoft.Network",
	}
	for _, phrase := range nonTerminal {
		if isKnownTerminalCanonical(phrase) {
			t.Errorf("expected %q NOT to be recognized as a known terminal canonical", phrase)
		}
	}
}

func TestSharesStructuredErrorPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want bool
	}{
		{
			a:    "error code: internalservererror; detail code authorizationfailed; detail message microsoft.network/networksecuritygroups/read",
			b:    "error code: internalservererror; detail code authorizationfailed; detail message microsoft.network/virtualnetworks/read",
			want: true,
		},
		{
			a:    "error running helm release deployment step, failed to deploy helm release: resource not ready, name: arobit-forwarder",
			b:    "error running helm release deployment step, failed to deploy helm release: resource not ready, name: aro-hcp-backend",
			want: true,
		},
		{
			a:    "resource not ready, name: multicluster-engine-operator, kind: Deployment, status: InProgress context deadline exceeded",
			b:    "resource not ready, name: grc-policy-propagator, kind: Deployment, status: InProgress context deadline exceeded",
			want: true,
		},
		{
			a:    "failed post-install: resource not ready, name: finalize-mce, kind: Job, status: InProgress context deadline exceeded",
			b:    "failed post-install: resource not ready, name: finalize-mce-config, kind: Job, status: InProgress context deadline exceeded",
			want: true,
		},
		{
			a:    `level=error msg="step errored." serviceGroup=Microsoft.Azure.ARO.HCP.ACM step=deploy-mce err="error running helm release deployment step, failed to deploy helm release: resource not ready, name: finalize-mce, kind: job, status: inprogress\ncontext deadline exceeded"`,
			b:    `level=error msg="step errored." serviceGroup=Microsoft.Azure.ARO.HCP.ACM step=deploy-mce-config err="error running helm release deployment step, failed to deploy helm release: resource not ready, name: grc-policy-propagator, kind: deployment, status: inprogress\ncontext deadline exceeded"`,
			want: true,
		},
		{
			a:    "timeout during createhcpclusterandwait; context deadline exceeded",
			b:    "timeout during getadminrestconfigforhcpcluster while waiting for hcpcluster creds",
			want: false,
		},
	}
	for _, tt := range tests {
		if got := sharesStructuredErrorPrefix(tt.a, tt.b); got != tt.want {
			t.Errorf("sharesStructuredErrorPrefix(%q, %q) = %v, want %v", tt.a[:40], tt.b[:40], got, tt.want)
		}
	}
}
