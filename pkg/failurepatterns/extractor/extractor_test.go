package extractor

import (
	"strings"
	"testing"
)

func TestExtractUsesHTTP502StatusLineWhenOnlySignal(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_versions.go:42]: Unexpected error:
    <*exported.ResponseError | 0xc000736630>:
    GET https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/providers/Microsoft.RedHatOpenShift/locations/uksouth/hcpOpenShiftVersions
    --------------------------------------------------------------------------------
    RESPONSE 502: 502 Bad Gateway
    ERROR CODE UNAVAILABLE
    --------------------------------------------------------------------------------
    Response contained no body
    --------------------------------------------------------------------------------
    {
        ErrorCode: "",
        StatusCode: 502,
    }`

	pattern := Extract(raw)
	if pattern.CanonicalEvidencePhrase != "RESPONSE 502: 502 Bad Gateway" {
		t.Fatalf("expected canonical phrase to use HTTP response status line, got=%q", pattern.CanonicalEvidencePhrase)
	}
	if pattern.SearchQueryPhrase != "RESPONSE 502: 502 Bad Gateway" {
		t.Fatalf("expected search phrase to use HTTP response status line, got=%q", pattern.SearchQueryPhrase)
	}
}

func TestExtractPrefersDeserializationWhenCommandErrorIsBareExitStatus(t *testing.T) {
	t.Parallel()

	raw := `goroutine 1383 gp=0xc00161cfc0 m=nil [sync.WaitGroup.Wait, 3 minutes]:
runtime.gopark(0xc001729af0?, 0x2a657d4?, 0x20?, 0xb9?, 0x7ffb3a4a5d06?)
Command Error: exit status 2
Deserializaion Error: no output from command
crypto/tls.(*Conn).readFromUntil(0xc000806e08, {0x81cbfc0, 0xc000d38128}, 0xc0003829d0?)`

	pattern := Extract(raw)
	if pattern.CanonicalEvidencePhrase != "Deserializaion Error: no output from command" {
		t.Fatalf("expected canonical phrase to use deserialization no-output line, got=%q", pattern.CanonicalEvidencePhrase)
	}
	if pattern.SearchQueryPhrase != "Deserializaion Error: no output from command" {
		t.Fatalf("expected search phrase to use deserialization no-output line, got=%q", pattern.SearchQueryPhrase)
	}
}

func TestChooseSearchPhraseFallsBackToActionableErrorLine(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_authorized_cidrs_connectivity.go:133]: Unexpected error:
    <*fmt.wrapError | 0xc00097b920>: 
    failed to create HCP cluster cidr-connectivity-test: failed starting cluster creation "cidr-connectivity-test" in resourcegroup="e2e-cidr-connectivity-f9k9vw": PUT https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/e2e-cidr-connectivity-f9k9vw/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cidr-connectivity-test
    {
        msg: "failed to create HCP cluster cidr-connectivity-test",
        err: <*azcore.ResponseError | 0xc0002a6d80>{
            ErrorCode: "",
        },
    }`

	got := ChooseSearchPhrase(raw, nil)
	lowered := strings.ToLower(got)
	if strings.HasPrefix(lowered, "fail [") {
		t.Fatalf("expected search phrase to avoid framework wrapper line, got=%q", got)
	}
	if !strings.Contains(lowered, "failed to create hcp cluster") {
		t.Fatalf("expected search phrase to include actionable failure line, got=%q", got)
	}
}

func TestFailurePatternKeyRemovesPlaceholderTokens(t *testing.T) {
	t.Parallel()

	key := FailurePatternKey(FailurePattern{
		CanonicalEvidencePhrase: "Error for <uuid> at <url> with payload <hex>",
	})
	if key != "error for at with payload" {
		t.Fatalf("unexpected failure pattern key: got=%q", key)
	}
}
