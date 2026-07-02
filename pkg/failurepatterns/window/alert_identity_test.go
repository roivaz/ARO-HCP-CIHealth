package window

import (
	"testing"

	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

// TestNormalizeRawFailureRecordPreservesArtifactPath guards against regressing
// the alert lane: the artifact path is the fact used to derive the alert lane
// and must survive window-side normalization. Dropping it makes alert failures
// classify as "unknown" and breaks alert failure-pattern grouping.
func TestNormalizeRawFailureRecordPreservesArtifactPath(t *testing.T) {
	t.Parallel()

	const artifactPath = "https://example.com/run/artifacts/e2e-parallel/aro-hcp-gather-observability/artifacts/junit_alerts.xml"
	got := normalizeRawFailureRecord(storecontracts.RawFailureRecord{
		Environment:  "dev",
		RowID:        "row-1",
		RunURL:       "https://example.com/run",
		TestSuite:    "aro-hcp-tests",
		TestName:     "[aro-hcp-observability] [hcp] alert KubePodNotReady does not fire",
		SignatureID:  "sig-1",
		OccurredAt:   "2026-07-01T12:00:00Z",
		ArtifactPath: artifactPath,
	}, "dev", "")

	if got.ArtifactPath != artifactPath {
		t.Fatalf("artifact path not preserved: got=%q want=%q", got.ArtifactPath, artifactPath)
	}
}

func TestAlertIdentityFromTestName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		testName      string
		wantOK        bool
		wantCanonical string
		wantKey       string
	}{
		{
			name:          "hcp scope",
			testName:      "[aro-hcp-observability] [hcp] alert KubeAPIDown does not fire",
			wantOK:        true,
			wantCanonical: "alert [hcp] KubeAPIDown fired",
			wantKey:       "alert hcp kubeapidown",
		},
		{
			name:          "svc scope distinct from hcp",
			testName:      "[aro-hcp-observability] [svc] alert KubeAPIDown does not fire",
			wantOK:        true,
			wantCanonical: "alert [svc] KubeAPIDown fired",
			wantKey:       "alert svc kubeapidown",
		},
		{
			name:     "non-alert test name",
			testName: "[sig-network] some unrelated e2e test",
			wantOK:   false,
		},
		{
			name:     "empty",
			testName: "",
			wantOK:   false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := alertIdentityFromTestName(tc.testName)
			if ok != tc.wantOK {
				t.Fatalf("ok mismatch: got=%v want=%v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.canonical != tc.wantCanonical {
				t.Fatalf("canonical mismatch: got=%q want=%q", got.canonical, tc.wantCanonical)
			}
			if got.key != tc.wantKey {
				t.Fatalf("key mismatch: got=%q want=%q", got.key, tc.wantKey)
			}
		})
	}
}

func TestAlertIdentityScopeDistinctKeys(t *testing.T) {
	t.Parallel()

	hcp, ok := alertIdentityFromTestName("[aro-hcp-observability] [hcp] alert SameName does not fire")
	if !ok {
		t.Fatalf("expected hcp identity to parse")
	}
	svc, ok := alertIdentityFromTestName("[aro-hcp-observability] [svc] alert SameName does not fire")
	if !ok {
		t.Fatalf("expected svc identity to parse")
	}
	if hcp.key == svc.key {
		t.Fatalf("expected scope to produce distinct keys, both were %q", hcp.key)
	}
}

func TestBucketSourceForLane(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		lane string
		want string
	}{
		{lane: "provision", want: "provision"},
		{lane: "e2e", want: "e2e"},
		{lane: "alert", want: "alert"},
		{lane: "unknown", want: "other"},
		{lane: "", want: "other"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.lane, func(t *testing.T) {
			t.Parallel()
			if got := bucketSourceForLane(tc.lane); got != tc.want {
				t.Fatalf("bucketSourceForLane(%q): got=%q want=%q", tc.lane, got, tc.want)
			}
		})
	}
}
