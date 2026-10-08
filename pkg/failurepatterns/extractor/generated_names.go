package extractor

import "strings"

// normalizeGeneratedNames is deliberately conservative: punctuation-bearing
// versions, API fields and SKUs are not candidates. Only generated suffixes on
// lowercase names and long opaque lowercase IDs are normalized. Ambiguity keeps
// the original text; no decisions depend on other failures in a window.
func normalizeGeneratedNames(text string) string {
	var out strings.Builder
	copied := 0
	for start := 0; start < len(text); {
		if text[start] == '<' {
			if end := strings.IndexByte(text[start:], '>'); end >= 0 {
				start += end + 1
				continue
			}
		}
		if !identityTokenByte(text[start]) {
			start++
			continue
		}
		end := start + 1
		for end < len(text) && identityTokenByte(text[end]) {
			end++
		}
		token := text[start:end]
		cut := generatedNameCut(token)
		if cut >= 0 {
			out.WriteString(text[copied : start+cut])
			out.WriteString("<id>")
			copied = end
		}
		start = end
	}
	if copied == 0 {
		return text
	}
	out.WriteString(text[copied:])
	return out.String()
}

func identityTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_'
}

func generatedNameCut(token string) int {
	// Never peel a random-looking tail off a version, domain, field or SKU.
	for i := range token {
		c := token[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return -1
		}
	}
	end := len(token)
	cut := -1
	for end > 0 {
		dash := strings.LastIndexByte(token[:end], '-')
		if dash < 1 {
			break
		}
		if !generatedSuffix(token[dash+1 : end]) {
			break
		}
		cut = dash + 1
		end = dash
	}
	if cut >= 0 {
		return cut
	}
	if len(token) >= 20 && !strings.Contains(token, "-") && generatedSuffix(token) {
		return 0
	}
	return -1
}

func generatedSuffix(s string) bool {
	if len(s) < 5 || len(s) > 32 {
		return false
	}
	letters, digits, transitions := 0, 0, 0
	kubernetesAlphabet := true
	previousDigit := false
	for i := range s {
		c := s[i]
		digit := c >= '0' && c <= '9'
		if digit {
			digits++
		} else if c >= 'a' && c <= 'z' {
			letters++
		} else {
			return false
		}
		if i > 0 && digit != previousDigit {
			transitions++
		}
		previousDigit = digit
		if !strings.ContainsRune("bcdfghjklmnpqrstvwxz2456789", rune(c)) {
			kubernetesAlphabet = false
		}
	}
	// Short suffixes need the Kubernetes generateName alphabet; longer ones
	// need repeated letter/digit alternation, not merely a numeric version.
	if len(s) == 5 || len(s) == 6 {
		return kubernetesAlphabet && letters >= 2 && digits >= 1
	}
	return len(s) >= 8 && letters >= 3 && digits >= 3 && transitions >= 3
}
