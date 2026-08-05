package extractor

import (
	"regexp"
	"strings"
)

var reCollapseWhitespace = regexp.MustCompile(`\s+`)

var genericCodes = map[string]struct{}{
	"deploymentfailed":       {},
	"internalservererror":    {},
	"conflict":               {},
	"badrequest":             {},
	"multipleerrorsoccurred": {},
	// Codes where the detail message carries meaningful context.
	"notfound":              {},
	"resourcenotfound":      {},
	"invalidrequestcontent": {},
	"invalidtemplate":       {},
}

var wrapperOnly = map[string]struct{}{
	"unexpected error": {},
	"msg:":             {},
	"err:":             {},
	"caused by:":       {},
	"step errored":     {},
	// Gomega assertion wrappers. The real error is the inner value.
	"...":                                 {},
	"expected success, but got an error:": {},
	"occurred":                            {},
	"cause: {":                            {},
}

var assertionTailPrefixes = []string{
	"to be true",
	"to be false",
	"to equal",
	"to have occurred",
	"to match error",
	"to match",
	"to contain substring",
	"to be nil",
	"to be empty",
	"to be numerically",
	"to have len",
	"to have length",
	"to have key",
	"to consist of",
}

func collapseWS(value string) string {
	return reCollapseWhitespace.ReplaceAllString(strings.TrimSpace(value), " ")
}

func isGenericCode(value string) bool {
	_, ok := genericCodes[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

// alwaysDetailedProviders are Azure resource providers for which we always
// attempt to surface the inner "code"/"message" detail, regardless of
// whether the outer ERROR CODE happens to be in the genericCodes allowlist.
// Microsoft.RedHatOpenShift is the RP under test here, so any error code it
// returns - including ones we haven't seen yet - should come through with
// full detail rather than requiring a follow-up patch to genericCodes each
// time a new code surfaces.
var alwaysDetailedProviders = map[string]struct{}{
	"microsoft.redhatopenshift": {},
}

func isAlwaysDetailedProvider(provider string) bool {
	_, ok := alwaysDetailedProviders[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}
