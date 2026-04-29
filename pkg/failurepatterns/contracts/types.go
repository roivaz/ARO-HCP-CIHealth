package contracts

type ReferenceRecord struct {
	RowID          string `json:"row_id,omitempty"`
	RunURL         string `json:"run_url"`
	OccurredAt     string `json:"occurred_at"`
	SignatureID    string `json:"signature_id"`
	PRNumber       int    `json:"pr_number"`
	PostGoodCommit bool   `json:"post_good_commit"`
}

type ContributingTestRecord struct {
	Lane         string `json:"lane"`
	JobName      string `json:"job_name"`
	TestName     string `json:"test_name"`
	SupportCount int    `json:"support_count"`
}

type FailurePatternRecord struct {
	Environment                  string                   `json:"environment"`
	Phase2ClusterID              string                   `json:"phase2_cluster_id"`
	CanonicalEvidencePhrase      string                   `json:"canonical_evidence_phrase"`
	SearchQueryPhrase            string                   `json:"search_query_phrase"`
	SearchQuerySourceRunURL      string                   `json:"search_query_source_run_url"`
	SearchQuerySourceSignatureID string                   `json:"search_query_source_signature_id"`
	SupportCount                 int                      `json:"support_count"`
	SeenPostGoodCommit           bool                     `json:"seen_post_good_commit"`
	PostGoodCommitCount          int                      `json:"post_good_commit_count"`
	ContributingTestsCount       int                      `json:"contributing_tests_count"`
	ContributingTests            []ContributingTestRecord `json:"contributing_tests"`
	MemberPhase1ClusterIDs       []string                 `json:"member_phase1_cluster_ids"`
	MemberSignatureIDs           []string                 `json:"member_signature_ids"`
	References                   []ReferenceRecord        `json:"references"`
}

type ReviewItemRecord struct {
	Environment                          string            `json:"environment"`
	ReviewItemID                         string            `json:"review_item_id"`
	Phase                                string            `json:"phase"`
	Reason                               string            `json:"reason"`
	Severity                             string            `json:"severity,omitempty"`
	ProposedCanonicalEvidencePhrase      string            `json:"proposed_canonical_evidence_phrase"`
	ProposedSearchQueryPhrase            string            `json:"proposed_search_query_phrase"`
	ProposedSearchQuerySourceRunURL      string            `json:"proposed_search_query_source_run_url"`
	ProposedSearchQuerySourceSignatureID string            `json:"proposed_search_query_source_signature_id"`
	SourcePhase1ClusterIDs               []string          `json:"source_phase1_cluster_ids"`
	MemberSignatureIDs                   []string          `json:"member_signature_ids"`
	References                           []ReferenceRecord `json:"references"`
}
