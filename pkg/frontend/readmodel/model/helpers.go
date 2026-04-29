package model

import (
	"sort"
	"strings"
)

func NormalizeEnvironment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizePhrase(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(trimmed), " "))
}

func NormalizeStringSlice(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		normalized := NormalizeEnvironment(value)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return SortedStringSet(set)
}

func SortedStringSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
