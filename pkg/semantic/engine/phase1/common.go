package phase1

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

var (
	reCollapseWhitespace = regexp.MustCompile(`\s+`)
)

func collapseWS(value string) string {
	return reCollapseWhitespace.ReplaceAllString(strings.TrimSpace(value), " ")
}

func defaultKeyPart(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func buildGroupKey(environment string, lane string, jobName string, testName string) string {
	env := strings.TrimSpace(environment)
	if env == "" {
		return defaultKeyPart(lane, "unknown") + "|" + defaultKeyPart(jobName, "unknown") + "|" + defaultKeyPart(testName, "unknown")
	}
	return defaultKeyPart(env, "unknown") + "|" + defaultKeyPart(lane, "unknown") + "|" + defaultKeyPart(jobName, "unknown") + "|" + defaultKeyPart(testName, "unknown")
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildRowID(runURL string, signatureID string, occurredAt string) string {
	seed := strings.TrimSpace(runURL) + "|" + strings.TrimSpace(signatureID) + "|" + strings.TrimSpace(occurredAt)
	return fingerprint(seed)
}

func buildRowIDWithEnvironment(environment string, runURL string, signatureID string, occurredAt string) string {
	seed := strings.TrimSpace(environment) + "|" + strings.TrimSpace(runURL) + "|" + strings.TrimSpace(signatureID) + "|" + strings.TrimSpace(occurredAt)
	return fingerprint(seed)
}

func normalizeReason(value string) string {
	normalized := strings.ToLower(collapseWS(value))
	return strings.ReplaceAll(normalized, " ", "_")
}

func sortedKeys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
