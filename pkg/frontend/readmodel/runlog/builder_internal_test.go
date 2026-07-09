package runlog

import (
	"testing"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/failurepatterns"
	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
)

type staticPresenceResolver struct {
	presence failurepatterns.PatternPresence
}

func (r staticPresenceResolver) PresenceFor(failurepatterns.PatternKey) failurepatterns.PatternPresence {
	return r.presence
}

func TestBuildJobHistoryReferenceIndexUsesHistoryResolverForPriorWeeks(t *testing.T) {
	t.Parallel()

	cluster := readmodelpatterns.FailurePatternReportCluster{
		Environment:             "dev",
		Phase2ClusterID:         "cluster-1",
		CanonicalEvidencePhrase: "timeout during CreateHCPClusterAndWait; context deadline exceeded",
		SearchQueryPhrase:       "timeout during CreateHCPClusterAndWait; context deadline exceeded",
		SupportCount:            1,
		References: []readmodelpatterns.FailurePatternReportReference{
			{
				RowID:          "row-1",
				RunURL:         "https://prow.example.com/view/1",
				OccurredAt:     "2026-05-01T14:40:19Z",
				SignatureID:    "sig-1",
				PRNumber:       123,
				PostGoodCommit: false,
			},
		},
	}

	index := buildJobHistoryReferenceIndex(
		[]readmodelpatterns.FailurePatternReportCluster{cluster},
		staticPresenceResolver{presence: failurepatterns.PatternPresence{PriorWeeksPresent: 4}},
	)

	rowKey := jobHistoryReferenceRowKey("dev", "row-1")
	got, ok := index[rowKey]
	if !ok {
		t.Fatalf("expected row %q to be indexed", rowKey)
	}
	if got.PriorWeeksPresent != 4 {
		t.Fatalf("unexpected prior weeks present: got=%d want=4", got.PriorWeeksPresent)
	}
	if got.BadPRScore != 0 {
		t.Fatalf("expected history-backed prior weeks to suppress bad PR score, got=%d", got.BadPRScore)
	}
}

func TestBuildJobHistoryReferenceIndexUsesResolverBackedBadPRSignal(t *testing.T) {
	t.Parallel()

	cluster := readmodelpatterns.FailurePatternReportCluster{
		Environment:             "dev",
		Phase2ClusterID:         "cluster-1",
		CanonicalEvidencePhrase: "timeout during CreateHCPClusterAndWait; context deadline exceeded",
		SearchQueryPhrase:       "timeout during CreateHCPClusterAndWait; context deadline exceeded",
		SupportCount:            2,
		References: []readmodelpatterns.FailurePatternReportReference{
			{
				RowID:          "row-1",
				RunURL:         "https://prow.example.com/view/1",
				OccurredAt:     "2026-05-01T14:40:19Z",
				SignatureID:    "sig-1",
				PRNumber:       123,
				PostGoodCommit: false,
			},
			{
				RowID:          "row-2",
				RunURL:         "https://prow.example.com/view/2",
				OccurredAt:     "2026-05-01T15:40:19Z",
				SignatureID:    "sig-2",
				PRNumber:       456,
				PostGoodCommit: false,
			},
		},
	}

	index := buildJobHistoryReferenceIndex(
		[]readmodelpatterns.FailurePatternReportCluster{cluster},
		staticPresenceResolver{presence: failurepatterns.PatternPresence{
			BadPRScore:   3,
			BadPRReasons: []string{"resolver-backed"},
		}},
	)

	rowKey := jobHistoryReferenceRowKey("dev", "row-1")
	got, ok := index[rowKey]
	if !ok {
		t.Fatalf("expected row %q to be indexed", rowKey)
	}
	if got.BadPRScore != 3 {
		t.Fatalf("unexpected resolver-backed bad PR score: got=%d want=3", got.BadPRScore)
	}
	if len(got.BadPRReasons) != 1 || got.BadPRReasons[0] != "resolver-backed" {
		t.Fatalf("unexpected resolver-backed bad PR reasons: %+v", got.BadPRReasons)
	}
}

func TestFilterRunsByFailedAt(t *testing.T) {
	t.Parallel()

	provisionRun := JobHistoryRunRow{Lanes: []string{"provision"}}
	e2eRun := JobHistoryRunRow{Lanes: []string{"e2e"}}
	alertRun := JobHistoryRunRow{Lanes: []string{"alert"}}
	mixedRun := JobHistoryRunRow{Lanes: []string{"provision", "e2e"}}
	otherRun := JobHistoryRunRow{Lanes: []string{"unknown"}}
	passingRun := JobHistoryRunRow{Lanes: nil}
	all := []JobHistoryRunRow{provisionRun, e2eRun, alertRun, mixedRun, otherRun, passingRun}

	testCases := []struct {
		name     string
		failedAt []string
		want     int
	}{
		{name: "empty selection keeps all runs", failedAt: nil, want: len(all)},
		{name: "provision keeps provision and mixed", failedAt: []string{"provision"}, want: 2},
		{name: "e2e keeps e2e and mixed", failedAt: []string{"e2e"}, want: 2},
		{name: "alert keeps alert only", failedAt: []string{"alert"}, want: 1},
		{name: "other buckets unknown lane", failedAt: []string{"other"}, want: 1},
		{name: "multiple sources union", failedAt: []string{"provision", "alert"}, want: 3},
		{name: "unknown source matches nothing", failedAt: []string{"bogus"}, want: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := filterRunsByFailedAt(all, testCase.failedAt)
			if len(got) != testCase.want {
				t.Fatalf("filterRunsByFailedAt(%v) kept %d runs, want %d", testCase.failedAt, len(got), testCase.want)
			}
		})
	}
}
