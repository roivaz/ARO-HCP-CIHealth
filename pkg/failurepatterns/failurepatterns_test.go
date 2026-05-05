package failurepatterns

import (
	"testing"
	"time"

	failurepatterncontracts "ci-failure-atlas/pkg/failurepatterns/contracts"
)

func TestBuildPresenceResolverFromFailurePatternsFiltersToWindow(t *testing.T) {
	t.Parallel()

	resolver, err := BuildPresenceResolverFromFailurePatterns(BuildPresenceFromFailurePatternsOptions{
		EndTime:       time.Date(2026, time.March, 24, 0, 0, 0, 0, time.UTC),
		LookbackWeeks: 4,
		FailurePatterns: []failurepatterncontracts.FailurePatternRecord{
			{
				Environment:             "dev",
				CanonicalEvidencePhrase: "OAuth timeout while waiting for cluster operator",
				SearchQueryPhrase:       "oauth timeout",
				References: []failurepatterncontracts.ReferenceRecord{
					{
						RunURL:      "https://prow.example.com/view/too-old",
						OccurredAt:  "2026-02-16T09:00:00Z",
						SignatureID: "sig-too-old",
					},
					{
						RunURL:      "https://prow.example.com/view/prior",
						OccurredAt:  "2026-03-09T09:00:00Z",
						SignatureID: "sig-prior",
					},
					{
						RunURL:      "https://prow.example.com/view/current",
						OccurredAt:  "2026-03-23T09:00:00Z",
						SignatureID: "sig-current",
						PRNumber:    4101,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build presence resolver from rows: %v", err)
	}

	presence := resolver.PresenceFor(PatternKey{
		Environment: "dev",
		Phrase:      "OAuth timeout while waiting for cluster operator",
		SearchQuery: "oauth timeout",
	})
	if got, want := presence.PriorWeeksPresent, 1; got != want {
		t.Fatalf("unexpected prior weeks present: got=%d want=%d", got, want)
	}
	if got, want := len(presence.PriorWeekStarts), 1; got != want {
		t.Fatalf("unexpected prior week starts length: got=%d want=%d", got, want)
	}
	if got, want := presence.PriorWeekStarts[0], "2026-03-09"; got != want {
		t.Fatalf("unexpected prior week start: got=%q want=%q", got, want)
	}
	if got, want := presence.PriorJobsAffected, 2; got != want {
		t.Fatalf("unexpected prior jobs affected: got=%d want=%d", got, want)
	}
	if got, want := presence.PriorLastSeenAt.Format(time.RFC3339), "2026-03-09T09:00:00Z"; got != want {
		t.Fatalf("unexpected prior last seen at: got=%q want=%q", got, want)
	}
}
