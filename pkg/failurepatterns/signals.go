package failurepatterns

import "strings"

type BadPRSignalReference struct {
	RunURL      string
	OccurredAt  string
	SignatureID string
	PRNumber    int
}

type BadPRSignalEvidence struct {
	Environment             string
	AfterLastPushCount      int
	SeenInOtherEnvironments []string
	References              []BadPRSignalReference
	PriorWeeksPresent       int
}

func BadPRScoreAndReasons(evidence BadPRSignalEvidence) (int, []string) {
	if evidence.AfterLastPushCount > 0 {
		return 0, nil
	}
	if !badPROnlySeenInDev(evidence.Environment, evidence.SeenInOtherEnvironments) {
		return 0, nil
	}
	if !badPRSingleKnownPR(evidence.References) {
		return 0, nil
	}
	if evidence.PriorWeeksPresent > 0 {
		return 0, nil
	}
	return 3, []string{"post-good=0", "only seen in DEV", "only seen in one PR"}
}

func badPROnlySeenInDev(environment string, seenInOtherEnvironments []string) bool {
	if normalizeEnvironment(environment) != "dev" {
		return false
	}
	for _, value := range seenInOtherEnvironments {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func badPRSingleKnownPR(references []BadPRSignalReference) bool {
	if len(references) == 0 {
		return false
	}
	uniquePRs := map[int]struct{}{}
	for _, reference := range references {
		if reference.PRNumber <= 0 {
			return false
		}
		uniquePRs[reference.PRNumber] = struct{}{}
	}
	return len(uniquePRs) == 1
}
