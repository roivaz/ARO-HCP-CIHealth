package phase1

import (
	"sort"
	"strings"

	failureextractor "ci-failure-atlas/pkg/failurepatterns/extractor"
	semanticcontracts "ci-failure-atlas/pkg/semantic/contracts"
)

func Normalize(workset []semanticcontracts.Phase1WorksetRecord) []semanticcontracts.Phase1NormalizedRecord {
	out := make([]semanticcontracts.Phase1NormalizedRecord, 0, len(workset))
	for _, row := range workset {
		evidence := failureextractor.ExtractWithOptions(row.RawText, failureextractor.ExtractOptions{
			TestName: row.TestName,
		})
		out = append(out, semanticcontracts.Phase1NormalizedRecord{
			SchemaVersion:           semanticcontracts.CurrentSchemaVersion,
			Environment:             strings.TrimSpace(row.Environment),
			RowID:                   row.RowID,
			GroupKey:                row.GroupKey,
			Lane:                    row.Lane,
			JobName:                 row.JobName,
			TestName:                row.TestName,
			TestSuite:               row.TestSuite,
			SignatureID:             row.SignatureID,
			OccurredAt:              row.OccurredAt,
			RunURL:                  row.RunURL,
			PRNumber:                row.PRNumber,
			PostGoodCommit:          row.PostGoodCommit,
			RawText:                 row.RawText,
			NormalizedText:          row.NormalizedText,
			CanonicalEvidencePhrase: evidence.CanonicalEvidencePhrase,
			SearchQueryPhrase:       evidence.SearchQueryPhrase,
			ProviderAnchor:          evidence.ProviderAnchor,
			GenericPhrase:           evidence.GenericPhrase,
			Phase1Key:               failureextractor.FailurePatternKey(evidence),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		if out[i].JobName != out[j].JobName {
			return out[i].JobName < out[j].JobName
		}
		if out[i].TestName != out[j].TestName {
			return out[i].TestName < out[j].TestName
		}
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt < out[j].OccurredAt
		}
		if out[i].RunURL != out[j].RunURL {
			return out[i].RunURL < out[j].RunURL
		}
		if out[i].SignatureID != out[j].SignatureID {
			return out[i].SignatureID < out[j].SignatureID
		}
		return out[i].RowID < out[j].RowID
	})

	return out
}
