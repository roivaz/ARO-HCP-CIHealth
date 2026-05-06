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
