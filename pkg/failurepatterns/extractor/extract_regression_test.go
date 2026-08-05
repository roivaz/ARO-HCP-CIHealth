package extractor

import (
	"fmt"
	"strings"
	"testing"
)

func extractEvidence(text string) FailurePattern {
	return Extract(text)
}

func extractEvidenceWithTestName(text string, testName string) FailurePattern {
	return ExtractWithOptions(text, ExtractOptions{TestName: testName})
}

func TestExtractEvidencePrefersPreStructDeadlineLine(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/gpu_nodepools_create_delete.go:96]: Unexpected error:
    <*fmt.wrapError | 0xc0004ac420>: 
    failed waiting for deployment "aro-hcp-demo" in resourcegroup="gpu-nodepools-NC4asT4v3-z4g56q" to finish: context deadline exceeded
    {
        msg: "failed waiting for deployment \"aro-hcp-demo\" in resourcegroup=\"gpu-nodepools-NC4asT4v3-z4g56q\" to finish: context deadline exceeded",
        err: <context.deadlineExceededError>{},
    }`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	lowered := strings.ToLower(got)
	if strings.Contains(lowered, "<context.deadlineexceedederror>{},") {
		t.Fatalf("expected context type stub to be excluded from canonical phrase, got=%q", got)
	}
	if !strings.Contains(lowered, "failed waiting for deployment") {
		t.Fatalf("expected deployment timeout line in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceAvoidsEmptyErrorCodeStructField(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_authorized_cidrs_connectivity.go:133]: Unexpected error:
    <*fmt.wrapError | 0xc00097b920>: 
    failed to create HCP cluster cidr-connectivity-test: failed starting cluster creation "cidr-connectivity-test" in resourcegroup="e2e-cidr-connectivity-f9k9vw": PUT https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/e2e-cidr-connectivity-f9k9vw/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cidr-connectivity-test
    --------------------------------------------------------------------------------
    RESPONSE 502: 502 Bad Gateway
    ERROR CODE UNAVAILABLE
    --------------------------------------------------------------------------------
    Response contained no body
    --------------------------------------------------------------------------------
    {
        msg: "failed to create HCP cluster cidr-connectivity-test",
        err: <*azcore.ResponseError | 0xc0002a6d80>{
            ErrorCode: "",
        },
    }`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	lowered := strings.ToLower(got)
	if strings.Contains(lowered, "errorcode: \"\"") || strings.Contains(lowered, "errorcode:\"\"") {
		t.Fatalf("expected empty error code struct field to be excluded from canonical phrase, got=%q", got)
	}
	if !strings.Contains(lowered, "failed to create hcp cluster") {
		t.Fatalf("expected cluster create failure line in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceAvoidsBraceOnlyCanonicalFromWrappedErrors(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_version_backlevel.go:193]: Unexpected error:
    <*fmt.wrapError | 0xc000a823a0>: 
    route host was never found: Get "https://agnhost-e2e-serving-app-p8ds6.apps.aro.example.net": tls: failed to verify certificate: x509: certificate signed by unknown authority
    {
        msg: "route host was never found",
        err: <*url.Error | 0xc000e42f90>{
            Err: <*tls.CertificateVerificationError | 0xc000e42f60>{
                Err: <*x509.UnknownAuthorityError | 0xc0003ca3f0>{},
            },
        },
    }`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	lowered := strings.ToLower(got)
	if got == "{" || got == "}" || got == "{}" || got == "null" {
		t.Fatalf("expected non-struct canonical phrase, got=%q", got)
	}
	if !strings.Contains(lowered, "route host was never found") && !strings.Contains(lowered, "certificate signed by unknown authority") {
		t.Fatalf("expected wrapped error details in canonical phrase, got=%q", got)
	}
}

func TestSafeSearchFromTextSkipsFrameworkWrapperLine(t *testing.T) {
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

	got := safeSearchFromText(raw)
	lowered := strings.ToLower(got)
	if strings.HasPrefix(lowered, "fail [") {
		t.Fatalf("expected safe search phrase to avoid framework wrapper line, got=%q", got)
	}
	if !strings.Contains(lowered, "failed to create hcp cluster") {
		t.Fatalf("expected safe search phrase to include actionable failure line, got=%q", got)
	}
}

func TestExtractEvidenceCollapsesGetAdminRESTConfigTimeoutVariants(t *testing.T) {
	t.Parallel()

	rawA := `failed waiting for hcpcluster="ea-list" in resourcegroup="external-auth-rg-pxk72q" to finish getting creds, caused by: timeout '10.000000' minutes exceeded during GetAdminRESTConfigForHCPCluster for cluster ea-list`
	rawB := `failed waiting for hcpcluster="ea-list" in resourcegroup="external-auth-rg-pxk72q" to finish getting creds, caused by: timeout '10.000000' minutes exceeded during GetAdminRESTConfigForHCPCluster for cluster ea-list in resource group external-auth-rg-pxk72q`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := "timeout during GetAdminRESTConfigForHCPCluster while waiting for hcpcluster creds"

	if gotA != want {
		t.Fatalf("unexpected canonical for short variant: got=%q want=%q", gotA, want)
	}
	if gotB != want {
		t.Fatalf("unexpected canonical for long variant: got=%q want=%q", gotB, want)
	}
}

func TestExtractEvidenceUsesHTTP502StatusLineWhenOnlySignal(t *testing.T) {
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

	evidence := extractEvidence(raw)
	if evidence.CanonicalEvidencePhrase != "RESPONSE 502: 502 Bad Gateway" {
		t.Fatalf("expected canonical phrase to use HTTP response status line, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if evidence.SearchQueryPhrase != "RESPONSE 502: 502 Bad Gateway" {
		t.Fatalf("expected search phrase to use HTTP response status line, got=%q", evidence.SearchQueryPhrase)
	}
}

func TestExtractEvidencePrefersDeserializationWhenCommandErrorIsBareExitStatus(t *testing.T) {
	t.Parallel()

	raw := `goroutine 1383 gp=0xc00161cfc0 m=nil [sync.WaitGroup.Wait, 3 minutes]:
runtime.gopark(0xc001729af0?, 0x2a657d4?, 0x20?, 0xb9?, 0x7ffb3a4a5d06?)
Command Error: exit status 2
Deserializaion Error: no output from command
crypto/tls.(*Conn).readFromUntil(0xc000806e08, {0x81cbfc0, 0xc000d38128}, 0xc0003829d0?)`

	evidence := extractEvidence(raw)
	if evidence.CanonicalEvidencePhrase != "Deserializaion Error: no output from command" {
		t.Fatalf("expected canonical phrase to use deserialization no-output line, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if evidence.SearchQueryPhrase != "Deserializaion Error: no output from command" {
		t.Fatalf("expected search phrase to use deserialization no-output line, got=%q", evidence.SearchQueryPhrase)
	}
}

func TestExtractEvidenceKeepsDeserializationNoOutputWithoutCommandError(t *testing.T) {
	t.Parallel()

	raw := `Deserializaion Error: no output from command
goroutine 1 [running]:
runtime.throw({0x1, 0x2})`

	evidence := extractEvidence(raw)
	if evidence.CanonicalEvidencePhrase != "Deserializaion Error: no output from command" {
		t.Fatalf("expected canonical phrase to remain deserialization no-output fallback, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesAzureInnerThrottlingCodeAndMessage(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "failed to get managed identity '/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/aro-hcp-test-msi-containers-dev-297/providers/Microsoft.ManagedIdentity/userAssignedIdentities/image-registry': GET https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/aro-hcp-test-msi-containers-dev-297/providers/Microsoft.ManagedIdentity/userAssignedIdentities/image-registry\n--------------------------------------------------------------------------------\nRESPONSE 429: 429 Too Many Requests\nERROR CODE: SubscriptionRequestsThrottled\n--------------------------------------------------------------------------------\n{\n  \"error\": {\n    \"code\": \"SubscriptionRequestsThrottled\",\n    \"message\": \"Number of 'read' requests for subscription 'XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX' actor 'd6b62dfa-87f5-49b3-bbcb-4a687c4faa96' exceeded. Please try again after '10' seconds after additional tokens are available. Refer to https://aka.ms/arm-throttling for additional information.\"\n  }\n}\n--------------------------------------------------------------------------------\n"
  }
}`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "ERROR CODE: InternalServerError") {
		t.Fatalf("expected canonical phrase to keep root error code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code SubscriptionRequestsThrottled") {
		t.Fatalf("expected canonical phrase to include inner throttling code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "detail message number of 'read' requests for subscription") {
		t.Fatalf("expected canonical phrase to include throttling message summary, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "provider Microsoft.ManagedIdentity") {
		t.Fatalf("expected provider Microsoft.ManagedIdentity to be included alongside inner detail, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesAzureNestedInvalidRequestDetail(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: DeploymentFailed
{
  "status": "Failed",
  "error": {
    "code": "DeploymentFailed",
    "message": "At least one resource deployment operation failed.",
    "details": [
      {
        "code": "Conflict",
        "message": "{\r\n  \"status\": \"Failed\",\r\n  \"error\": {\r\n    \"code\": \"ResourceDeploymentFailure\",\r\n    \"message\": \"The resource write operation failed to complete successfully, because it reached terminal provisioning state 'Failed'.\",\r\n    \"details\": [\r\n      {\r\n        \"code\": \"DeploymentFailed\",\r\n        \"message\": \"At least one resource deployment operation failed.\",\r\n        \"details\": [\r\n          {\r\n            \"code\": \"InvalidRequest\",\r\n            \"message\": \"The current utilization does not meet the criteria for both MaxTimeSeries and MaxEventsPerMinute quota requested. Please reach the required usage threshold of 50% of desired limit before requesting an increase, or request a limit increase of up to 200% of your current usage. For more details, see https://go.microsoft.com/fwlink/?linkid=2270124\"\r\n          }\r\n        ]\r\n      }\r\n    ]\r\n  }\r\n}"
      }
    ]
  }
}`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "ERROR CODE: DeploymentFailed") {
		t.Fatalf("expected canonical phrase to keep root deployment code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code InvalidRequest") {
		t.Fatalf("expected canonical phrase to include nested invalid request code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "detail message the current utilization does not meet the criteria") {
		t.Fatalf("expected canonical phrase to include quota message summary, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceKeepsGenericAzureConflictDetailCode(t *testing.T) {
	t.Parallel()

	raw := `time=2026-03-02T16:29:57.122Z level=ERROR msg="Step errored." err="failed to run ARM step: failed to wait for deployment completion: GET https://management.azure.com/subscriptions/123/resourcegroups/hcp-underlay/providers/Microsoft.EventGrid/namespaces/arohcp/providers/Microsoft.Resources/deployments/x
ERROR CODE: DeploymentFailed
{ "error": { "code": "DeploymentFailed", "details": [ { "code": "Conflict", "message": "operation failed due to an internal server error" } ] } }"`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code Conflict") {
		t.Fatalf("expected canonical phrase to keep generic inner conflict code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "provider microsoft.eventgrid") {
		t.Fatalf("expected provider Microsoft.EventGrid to be appended alongside detail code, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesAzureNestedRoleAssignmentLimitDetail(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: DeploymentFailed
{
  "status": "Failed",
  "error": {
    "code": "DeploymentFailed",
    "message": "At least one resource deployment operation failed.",
    "details": [
      {
        "code": "Conflict",
        "message": "{\r\n  \"error\": {\r\n    \"code\": \"RoleAssignmentLimitExceeded\",\r\n    \"message\": \"The role assignment limit for the subscription has been reached.\"\r\n  }\r\n}"
      }
    ]
  }
}`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code RoleAssignmentLimitExceeded") {
		t.Fatalf("expected canonical phrase to include role-assignment detail code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "detail message the role assignment limit for the subscription has been reached") {
		t.Fatalf("expected canonical phrase to include role-assignment detail message, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesAzureOverconstrainedAllocationDetail(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: DeploymentFailed
{
  "status": "Failed",
  "error": {
    "code": "DeploymentFailed",
    "message": "At least one resource deployment operation failed.",
    "details": [
      {
        "code": "OverconstrainedZonalAllocationRequest",
        "message": "Allocation failed. We do not have sufficient capacity for the requested VM size in this zone. Please try again later."
      }
    ]
  }
}`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code OverconstrainedZonalAllocationRequest") {
		t.Fatalf("expected canonical phrase to include zonal allocation detail code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "detail message allocation failed.") {
		t.Fatalf("expected canonical phrase to include normalized allocation failure summary, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceSkipsTruncatedAzureDetailCodeSuffix(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "inner payload",
    "details": [
      {
        "code": "SubscriptionRequestsThrottled",
        "message": "Number of 'read' requests for subscription 'XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX' actor '11111111-2222-3333-4444-555555555555' exceeded."
      }
    ]
  }
}
ERROR CODE: Subsc`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "detail code SubscriptionRequestsThrottled") {
		t.Fatalf("expected canonical phrase to keep full inner code instead of truncated suffix, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if strings.Contains(evidence.CanonicalEvidencePhrase, "detail code Subsc;") {
		t.Fatalf("expected canonical phrase to exclude truncated inner code suffix, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesRootAzureMessageWhenNoInnerCode(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: InternalServerError
{
  "id": "/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/providers/Microsoft.RedHatOpenShift/locations/eastus2euap/hcpOperationStatuses/b4b3429d-a139-425f-b110-f68346a4fe8b",
  "error": {
    "code": "InternalServerError",
    "message": "insufficient public IP address quota: required 2, available 0"
  }
}`

	evidence := extractEvidence(raw)
	if !strings.Contains(evidence.CanonicalEvidencePhrase, "ERROR CODE: InternalServerError") {
		t.Fatalf("expected canonical phrase to keep root code, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(strings.ToLower(evidence.CanonicalEvidencePhrase), "detail message insufficient public ip address quota: required <count>, available <count>") {
		t.Fatalf("expected canonical phrase to include normalized root Azure quota message, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceNormalizesQuotaCountVariants(t *testing.T) {
	t.Parallel()

	rawA := `ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "insufficient public IP address quota: required 2, available 0"
  }
}`
	rawB := `ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "insufficient public IP address quota: required 2, available 1"
  }
}`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected quota count variants to normalize to same canonical phrase: gotA=%q gotB=%q", gotA, gotB)
	}
	if !strings.Contains(strings.ToLower(gotA), "required <count>, available <count>") {
		t.Fatalf("expected normalized quota count placeholders in canonical phrase, got=%q", gotA)
	}
}

func TestExtractEvidenceEventuallyWrapperPrefersLineBeforeExpected(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_authorized_cidrs_connectivity.go:390]: Timed out after 600.002s.
The function passed to Eventually failed at /opt/app-root/src/github.com/Azure/ARO-HCP/test/e2e/cluster_authorized_cidrs_connectivity.go:389 with:
All ClusterOperators should report Available=True, but these are not available: [image-registry (Available=False)]
Expected
    <[]string | len:1, cap:1>: [
        "image-registry (Available=False)",
    ]
to be empty`

	evidence := extractEvidence(raw)
	lowered := strings.ToLower(evidence.CanonicalEvidencePhrase)
	if !strings.Contains(lowered, "all clusteroperators should report available=true, but these are not available") {
		t.Fatalf("expected canonical phrase to use context line before Expected, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if strings.Contains(lowered, "the function passed to eventually failed at") {
		t.Fatalf("expected Eventually wrapper line to be excluded from canonical phrase, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceContextDeadlinePrefersInnerDetail(t *testing.T) {
	t.Parallel()

	raw := `Unexpected error:
    <*errors.joinError | 0xc001101680>: 
    context deadline exceeded
    cluster operators not available: image-registry (Available=False, Progressing=True, Degraded=True)
    {
        errs: [
            <context.deadlineExceededError>{},
            <*errors.errorString | 0xc0006ec9b0>{
                s: "cluster operators not available: image-registry (Available=False, Progressing=True, Degraded=True)",
            },
        ],
    }`

	evidence := extractEvidence(raw)
	lowered := strings.ToLower(evidence.CanonicalEvidencePhrase)
	if !strings.Contains(lowered, "cluster operators not available: image-registry") {
		t.Fatalf("expected canonical phrase to include inner cluster-operator detail, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if lowered == "context deadline exceeded" {
		t.Fatalf("expected canonical phrase to be more specific than generic context deadline wrapper, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceSkipsIsTimeoutStructFieldAndUsesRouteHostLine(t *testing.T) {
	t.Parallel()

	raw := `Err: "no such host",
Name: "agnhost-e2e-serving-app-sckjc.apps.aro.example.net",
Server: "172.30.0.10:53",
IsTimeout: false,
IsTemporary: false,
IsNotFound: true,
occurred
fail [github.com/Azure/ARO-HCP/test/e2e/complete_cluster_create.go:137]: Unexpected error:
    <*fmt.wrapError | 0xc000e143c0>: 
    route host was never found: Get "https://agnhost-e2e-serving-app-sckjc.apps.aro.example.net": dial tcp: lookup agnhost-e2e-serving-app-sckjc.apps.aro.example.net on 172.30.0.10:53: no such host`

	evidence := extractEvidence(raw)
	lowered := strings.ToLower(evidence.CanonicalEvidencePhrase)
	if strings.Contains(lowered, "istimeout: false") {
		t.Fatalf("expected struct field dump line to be excluded from canonical phrase, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if !strings.Contains(lowered, "route host was never found") {
		t.Fatalf("expected canonical phrase to use route host failure line, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceNormalizesRouteHostLookupVariants(t *testing.T) {
	t.Parallel()

	rawA := `fail [github.com/Azure/ARO-HCP/test/e2e/complete_cluster_create.go:137]: Unexpected error:
route host was never found: Get "https://agnhost-e2e-serving-app-sckjc.apps.aro.u0e2e1n2t9u1a8h.4rck.j1302400.hcp.osadev.cloud": dial tcp: lookup agnhost-e2e-serving-app-sckjc.apps.aro.u0e2e1n2t9u1a8h.4rck.j1302400.hcp.osadev.cloud on 172.30.0.10:53: no such host`
	rawB := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_version_backlevel.go:194]: Unexpected error:
route host was never found: Get "https://agnhost-e2e-serving-app-9f7x5.apps.aro.l6q3l5t4y9r4i6k.15br.j8542976.hcp.osadev.cloud": dial tcp: lookup agnhost-e2e-serving-app-9f7x5.apps.aro.l6q3l5t4y9r4i6k.15br.j8542976.hcp.osadev.cloud on 172.30.0.10:53: no such host`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected route-host variants to normalize to same canonical phrase: gotA=%q gotB=%q", gotA, gotB)
	}
	if !strings.Contains(strings.ToLower(gotA), "lookup <host> on <dns-server>") {
		t.Fatalf("expected canonical phrase to normalize lookup host/server details, got=%q", gotA)
	}
}

func TestExtractEvidenceProwEntrypointTimestampsMerge(t *testing.T) {
	t.Parallel()

	rawA := `{"component":"entrypoint","file":"sigs.k8s.io/prow/pkg/entrypoint/run.go:169","func":"sigs.k8s.io/prow/pkg/entrypoint.Options.ExecuteProcess","level":"error","msg":"Process did not finish before 2h0m0s timeout","severity":"error","time":"2026-04-12T09:52:11Z"}`
	rawB := `{"component":"entrypoint","file":"sigs.k8s.io/prow/pkg/entrypoint/run.go:169","func":"sigs.k8s.io/prow/pkg/entrypoint.Options.ExecuteProcess","level":"error","msg":"Process did not finish before 2h0m0s timeout","severity":"error","time":"2026-04-16T22:04:25Z"}`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("prow entrypoint lines with different timestamps must canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "Process did not finish before 2h0m0s timeout") {
		t.Fatalf("expected prow entrypoint msg in canonical phrase, got=%q", gotA)
	}
}

func TestExtractEvidenceDialTCPAddressNormalized(t *testing.T) {
	t.Parallel()

	rawA := `fail [github.com/Azure/ARO-HCP/test/e2e/complete_cluster_create_multiversion.go:183]: Unexpected error:
    <*fmt.wrapError | 0xc000c8e620>: 
    route host was never found: Get "https://agnhost-e2e-serving-app-k8g25.apps.aro.u2q3n3k0t9h9m8l.pb5a.hcp.osadev.cloud": dial tcp 134.33.16.231:443: connect: connection timed out`
	rawB := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_version_backlevel.go:194]: Unexpected error:
    <*fmt.wrapError | 0xc001096040>: 
    route host was never found: Get "https://agnhost-e2e-serving-app-mbnnt.apps.aro.h7i5w5u7j5b3g2u.fova.hcp.osadev.cloud": dial tcp 20.40.25.244:443: connect: connection timed out`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("route-host dial-tcp errors with different IPs must canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if strings.Contains(gotA, "134.33") || strings.Contains(gotA, "20.40") {
		t.Fatalf("canonical phrase must not contain raw IP addresses, got=%q", gotA)
	}
}

func TestExtractEvidenceGomegaEllipsisNotExtracted(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/nodepool_update_nodes.go:262]: Expected success, but got an error:
    <*errors.errorString | 0xc000981550>: 
    expected 4 nodes, found 5
    ...
fail [github.com/Azure/ARO-HCP/test/e2e/nodepool_update_nodes.go:262]: Expected success, but got an error:
    <*errors.errorString | 0xc000981550>: 
    expected 4 nodes, found 5
    ...`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if got == "..." {
		t.Fatalf("canonical phrase must not be the Gomega ellipsis marker")
	}
	if !strings.Contains(got, "expected 4 nodes, found 5") {
		t.Fatalf("expected the inner assertion error in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceCreateHCPClusterTimeoutVariantsUnify(t *testing.T) {
	t.Parallel()

	want := "timeout during CreateHCPClusterAndWait; context deadline exceeded"

	cases := []struct {
		name string
		raw  string
	}{
		{
			"FromParam",
			`failed to create HCP cluster hcp-cluster, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: failed waiting for cluster="hcp-cluster" in resourcegroup="rg-abc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterFromParam for cluster hcp-cluster in resource group rg-abc, error: context deadline exceeded`,
		},
		{
			"20251223FromParam",
			`failed to create HCP cluster idms-e2e-hcp-cluster, caused by: timeout '45.000000' minutes exceeded during CreateHCPCluster20251223FromParam for cluster idms-e2e-hcp-cluster in resource group idms-v9cd6x, error: failed waiting for cluster="idms-e2e-hcp-cluster" in resourcegroup="idms-v9cd6x" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPCluster20251223FromParam for cluster idms-e2e-hcp-cluster in resource group idms-v9cd6x, error: context deadline exceeded`,
		},
		{
			"20251223AndWait",
			`failed waiting for cluster="cilium-cluster" in resourcegroup="e2e-cilium-hvlzkd" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPCluster20251223AndWait for cluster cilium-cluster in resource group e2e-cilium-hvlzkd, error: context deadline exceeded`,
		},
		{
			"AndWait",
			`failed waiting for cluster="cluster-ver-4-19" in resourcegroup="rg-cluster-back-version-g5hsfc" to finish creating, caused by: timeout '45.000000' minutes exceeded during CreateHCPClusterAndWait for cluster cluster-ver-4-19 in resource group rg-cluster-back-version-g5hsfc, error: context deadline exceeded`,
		},
	}

	for _, tc := range cases {
		got := extractEvidence(tc.raw).CanonicalEvidencePhrase
		if got != want {
			t.Errorf("case %q: got=%q want=%q", tc.name, got, want)
		}
	}
}

func TestExtractEvidenceGomegaSuccessFailureExtractsInnerError(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_create_private_kv.go:180]: Timed out after 600.005s.
router-default deployment logs should be fetchable
Expected success, but got an error:
    <*errors.errorString | 0xc001178270>: 
    deployment router-default -n openshift-ingress has no running pods
    ...`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if strings.EqualFold(got, "Expected success, but got an error:") {
		t.Fatalf("canonical phrase must not be the Gomega wrapper line, got=%q", got)
	}
	if !strings.Contains(strings.ToLower(got), "router-default") || !strings.Contains(strings.ToLower(got), "no running pods") {
		t.Fatalf("expected inner deployment error in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceErrorCodePreferredOverResponse404(t *testing.T) {
	t.Parallel()

	raw := `PUT https://management.azure.com/subscriptions/XXXX/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id/federatedIdentityCredentials/fic
--------------------------------------------------------------------------------
RESPONSE 404: 404 Not Found
ERROR CODE: NotFound
--------------------------------------------------------------------------------
{
  "error": {
    "code": "NotFound",
    "message": "MS Graph resource not found during Federated Identity Credential creation."
  }
}`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if strings.EqualFold(strings.TrimSpace(got), "RESPONSE 404: 404 Not Found") {
		t.Fatalf("ERROR CODE must be preferred over RESPONSE status line, got=%q", got)
	}
	if !strings.Contains(strings.ToLower(got), "notfound") {
		t.Fatalf("expected NotFound error code in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceInternalServerErrorDetailSuppressed(t *testing.T) {
	t.Parallel()

	rawWithDetail := `POST https://management.azure.com/subscriptions/XXXX/resourceGroups/rg/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster/requestAdminCredential
RESPONSE 500: 500 Internal Server Error
ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "Internal server error."
  }
}`
	rawWithout := `GET https://rp.example/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/abc123
RESPONSE 200: 200 OK
ERROR CODE: InternalServerError`

	gotWith := extractEvidence(rawWithDetail).CanonicalEvidencePhrase
	gotWithout := extractEvidence(rawWithout).CanonicalEvidencePhrase
	if gotWith != gotWithout {
		t.Fatalf("InternalServerError with and without generic detail must canonicalize identically:\n  with=%q\n  without=%q", gotWith, gotWithout)
	}
}

func TestExtractEvidenceLogfmtTimestampStripped(t *testing.T) {
	t.Parallel()

	rawA := `time=2026-04-17T11:04:19.211Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=delete-non-swift-user-nodepools err="failed to prepare kubeconfig: failed to ensure cluster admin role: /me request is only valid with delegated authentication flow."`
	rawB := `time=2026-04-18T09:00:00.000Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=delete-non-swift-user-nodepools err="failed to prepare kubeconfig: failed to ensure cluster admin role: /me request is only valid with delegated authentication flow."`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("logfmt lines with different timestamps must canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if strings.Contains(gotA, "2026-04-17") || strings.Contains(gotA, "2026-04-18") {
		t.Fatalf("canonical phrase must not contain raw timestamps, got=%q", gotA)
	}
}

func TestExtractEvidenceProwEntrypointExtractsMsgField(t *testing.T) {
	t.Parallel()

	raw := `{"component":"entrypoint","file":"sigs.k8s.io/prow/pkg/entrypoint/run.go:169","func":"sigs.k8s.io/prow/pkg/entrypoint.Options.ExecuteProcess","level":"error","msg":"Process did not finish before 2h0m0s timeout","severity":"error","time":"2026-04-18T01:21:20Z"}`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if got != "Process did not finish before 2h0m0s timeout" {
		t.Fatalf("expected clean msg value as canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceErrorCodeIncludesProvider(t *testing.T) {
	t.Parallel()

	raw := `GET https://management.azure.com/subscriptions/XXXX/providers/Microsoft.Network/virtualNetworks
RESPONSE 429: 429 Too Many Requests
ERROR CODE: ResourceCollectionRequestsThrottled`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(strings.ToLower(got), "provider microsoft.network") {
		t.Fatalf("expected provider Microsoft.Network in canonical phrase, got=%q", got)
	}
	if !strings.Contains(got, "ERROR CODE: ResourceCollectionRequestsThrottled") {
		t.Fatalf("expected error code preserved in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceRedHatOpenShiftProviderIncluded(t *testing.T) {
	t.Parallel()

	raw := `GET https://rp.example/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/abc
RESPONSE 500: 500 Internal Server Error
ERROR CODE: InternalServerError`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(strings.ToLower(got), "provider microsoft.redhatopenshift") {
		t.Fatalf("expected provider Microsoft.RedHatOpenShift in canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceOCPVersionStringNormalized(t *testing.T) {
	t.Parallel()

	rawV4 := `PUT https://rp.example/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster
RESPONSE 400: 400 Bad Request
ERROR CODE: InvalidRequestContent
{"error":{"code":"InvalidRequestContent","message":"Version 'openshift-v4.22.0-candidate' doesn't exist"}}`

	rawV5 := `PUT https://rp.example/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster
RESPONSE 400: 400 Bad Request
ERROR CODE: InvalidRequestContent
{"error":{"code":"InvalidRequestContent","message":"Version 'openshift-v5.1.0-candidate' doesn't exist"}}`

	gotV4 := extractEvidence(rawV4).CanonicalEvidencePhrase
	gotV5 := extractEvidence(rawV5).CanonicalEvidencePhrase
	if gotV4 != gotV5 {
		t.Fatalf("different OCP candidate versions must produce the same canonical:\n  v4=%q\n  v5=%q", gotV4, gotV5)
	}
	if strings.Contains(gotV4, "4.22.0") || strings.Contains(gotV4, "5.1.0") {
		t.Fatalf("canonical phrase must not contain raw version numbers, got=%q", gotV4)
	}
}

func TestExtractEvidenceClusterInternalIDNormalized(t *testing.T) {
	t.Parallel()

	rawA := `PATCH https://management.azure.com/subscriptions/XXXX/resourceGroups/rg-a/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster
RESPONSE 400: 400 Bad Request
ERROR CODE: InvalidRequestContent
{"error":{"code":"InvalidRequestContent","message":"Cluster '2pmeojr923nt08rchn2mn56al24muh61' is in state 'pending_update', can't update"}}`

	rawB := `PATCH https://management.azure.com/subscriptions/XXXX/resourceGroups/rg-b/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster
RESPONSE 400: 400 Bad Request
ERROR CODE: InvalidRequestContent
{"error":{"code":"InvalidRequestContent","message":"Cluster '2pni671e890elvabbe631mnqcb4pi6te' is in state 'pending_update', can't update"}}`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("same cluster-state error with different cluster IDs must canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if strings.Contains(gotA, "2pmeojr923nt08rchn2mn56al24muh61") {
		t.Fatalf("canonical phrase must not contain raw cluster IDs, got=%q", gotA)
	}
}

func TestCleanCanonicalNormalizesHCPApiHostname(t *testing.T) {
	t.Parallel()

	rawA := `tls: failed to verify certificate: x509: certificate is valid for api.a5t3f6u4j8a4a5h.4ufg.eastus2.aroapp-hcp.io, reserved.aroapp-hcp.io, not api.a5t3f6u4j8a4a5h.o0jt.eastus2.aroapp-hcp.io`
	rawB := `tls: failed to verify certificate: x509: certificate is valid for api.ea-cluster.kv02.uksouth.aroapp-hcp.io, reserved.aroapp-hcp.io, not api.ea-cluster.50y5.uksouth.aroapp-hcp.io`

	gotA := cleanCanonical(rawA)
	gotB := cleanCanonical(rawB)

	if strings.Contains(gotA, "a5t3f6u4j8a4a5h") || strings.Contains(gotA, "4ufg") {
		t.Fatalf("cleanCanonical should normalize HCP hostname tokens, got=%q", gotA)
	}
	if !strings.Contains(gotA, "<hcp-api-host>") {
		t.Fatalf("cleanCanonical should replace HCP hostname with <hcp-api-host>, got=%q", gotA)
	}
	if gotA != gotB {
		t.Fatalf("cert-mismatch errors from different clusters must canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
}

func TestExtractEvidenceNormalizesLongHCPApiHostnameBeforeTruncation(t *testing.T) {
	t.Parallel()

	rawA := `VerifyAllAPIServicesAvailable failed: failed to list all APIServices: Get "https://api.a5t3f6u4j8a4a5h.4ufg.eastus2.aroapp-hcp.io:443/apis/apiregistration.k8s.io/v1/apiservices": tls: failed to verify certificate: x509: certificate is valid for api.a5t3f6u4j8a4a5h.4ufg.eastus2.aroapp-hcp.io, reserved.aroapp-hcp.io, not api.a5t3f6u4j8a4a5h.o0jt.eastus2.aroapp-hcp.io`
	rawB := `VerifyAllAPIServicesAvailable failed: failed to list all APIServices: Get "https://api.ea-cluster.kv02.uksouth.aroapp-hcp.io:443/apis/apiregistration.k8s.io/v1/apiservices": tls: failed to verify certificate: x509: certificate is valid for api.ea-cluster.kv02.uksouth.aroapp-hcp.io, reserved.aroapp-hcp.io, not api.ea-cluster.50y5.uksouth.aroapp-hcp.io`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if !strings.Contains(gotA, "<hcp-api-host>") {
		t.Fatalf("expected canonical phrase to normalize HCP API hostnames, got=%q", gotA)
	}
	if gotA != gotB {
		t.Fatalf("long hostname variants should canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
}

func TestExtractEvidencePrefersCertMismatchDetailAcrossVerifyWrappers(t *testing.T) {
	t.Parallel()

	rawA := `VerifyAllAPIServicesAvailable failed: failed to list all APIServices: Get "https://api.i2o7e9m2u0f5e5k.kyxh.uksouth.aroapp-hcp.azure-test.net:443/apis/apiregistration.k8s.io/v1/apiservices": tls: failed to verify certificate: x509: certificate is valid for api.i2o7e9m2u0f5e5k.zser.uksouth.aroapp-hcp.azure-test.net, reserved.aroapp-hcp.azure-test.net, not api.i2o7e9m2u0f5e5k.kyxh.uksouth.aroapp-hcp.azure-test.net`
	rawB := `VerifyBasicAccess failed: failed to list services: Get "https://api.m4s3u7n2e5l1m2p.nay8.uksouth.aroapp-hcp.azure-test.net:443/api/v1/namespaces/default/services": tls: failed to verify certificate: x509: certificate is valid for api.m4s3u7n2e5l1m2p.kj53.uksouth.aroapp-hcp.azure-test.net, reserved.aroapp-hcp.azure-test.net, not api.m4s3u7n2e5l1m2p.nay8.uksouth.aroapp-hcp.azure-test.net`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("cert mismatch variants should canonicalize identically across wrapper differences:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "tls: failed to verify certificate") || !strings.Contains(gotA, "<hcp-api-host>") {
		t.Fatalf("expected canonical phrase to keep the leaf cert mismatch detail with normalized hosts, got=%q", gotA)
	}
	lowered := strings.ToLower(gotA)
	if strings.Contains(lowered, "verifybasicaccess failed") || strings.Contains(lowered, "verifyallapiservicesavailable failed") {
		t.Fatalf("expected wrapper-specific Verify* text to be excluded from canonical phrase, got=%q", gotA)
	}
}

func TestExtractEvidenceUsesModelDiffSummaryInsteadOfStructFieldLine(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_autoscaling.go:141]: Expected
    operation result model did not match expected model for type *armredhatopenshifthcp.HcpOpenShiftCluster:
      NodeDrainTimeoutMinutes: nil,
    to equal
      <string>: ...`

	evidence := extractEvidence(raw)
	lowered := strings.ToLower(evidence.CanonicalEvidencePhrase)
	if !strings.Contains(lowered, "operation result model did not match expected model") {
		t.Fatalf("expected canonical phrase to use the model-diff summary, got=%q", evidence.CanonicalEvidencePhrase)
	}
	if strings.Contains(lowered, "nodedraintimeoutminutes") {
		t.Fatalf("expected canonical phrase to skip interior struct-field lines, got=%q", evidence.CanonicalEvidencePhrase)
	}
}

func TestExtractEvidenceUsesStableLabelForPlaceholderOnlyEqualityAssertion(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/mise_routing.go:89]: Expected
    <string>: ...
to equal
    <string>: ...`

	evidence := extractEvidence(raw)
	if got, want := evidence.CanonicalEvidencePhrase, "assertion failed: expected values to equal"; got != want {
		t.Fatalf("unexpected canonical for placeholder-only equality assertion: got=%q want=%q", got, want)
	}
	if evidence.SearchQueryPhrase != "" {
		t.Fatalf("expected placeholder-only equality assertion to avoid a placeholder search query, got=%q", evidence.SearchQueryPhrase)
	}
}

func TestExtractEvidenceNormalizesTimedOutAfterPrecision(t *testing.T) {
	t.Parallel()

	rawA := `fail [github.com/Azure/ARO-HCP/test/e2e/exporter_metrics.go:114]: Timed out after 1800.001s.
Expected
    <bool>: false
to be true`
	rawB := `fail [github.com/Azure/ARO-HCP/test/e2e/exporter_metrics.go:114]: Timed out after 1800.002s.
Expected
    <bool>: false
to be true`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := "Timed out after <duration>s."
	if gotA != want {
		t.Fatalf("unexpected canonical for first timeout variant: got=%q want=%q", gotA, want)
	}
	if gotB != want {
		t.Fatalf("unexpected canonical for second timeout variant: got=%q want=%q", gotB, want)
	}
}

func TestExtractEvidenceContextualizesGenericTimedOutCanonicalWithTestName(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/exporter_metrics.go:114]: Timed out after 1800.001s.
Expected
    <bool>: false
to be true`

	got := extractEvidenceWithTestName(raw, "Engineering should be able to retrieve expected metrics from the /metrics endpoint").CanonicalEvidencePhrase
	want := "Engineering should be able to retrieve expected metrics from the /metrics endpoint: Timed out after <duration>s."
	if got != want {
		t.Fatalf("unexpected contextualized timeout canonical: got=%q want=%q", got, want)
	}
}

func TestExtractEvidenceContextualizesGenericAssertionCanonicalWithTestName(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/mise_routing.go:89]: Expected
    <string>: ...
to equal
    <string>: ...`

	got := extractEvidenceWithTestName(raw, "MISE routing returns the versioned frontend").CanonicalEvidencePhrase
	want := "MISE routing returns the versioned frontend: assertion failed: expected values to equal"
	if got != want {
		t.Fatalf("unexpected contextualized assertion canonical: got=%q want=%q", got, want)
	}
}

func TestExtractEvidenceStripsTransportGetWrapperFromTLSMismatch(t *testing.T) {
	t.Parallel()

	rawA := `Get "https://api.i2o7e9m2u0f5e5k.kyxh.uksouth.aroapp-hcp.azure-test.net:443/apis/apiregistration.k8s.io/v1/apiservices": tls: failed to verify certificate: x509: certificate is valid for api.i2o7e9m2u0f5e5k.zser.uksouth.aroapp-hcp.azure-test.net, reserved.aroapp-hcp.azure-test.net, not api.i2o7e9m2u0f5e5k.kyxh.uksouth.aroapp-hcp.azure-test.net`
	rawB := `tls: failed to verify certificate: x509: certificate is valid for api.m4s3u7n2e5l1m2p.kj53.uksouth.aroapp-hcp.azure-test.net, reserved.aroapp-hcp.azure-test.net, not api.m4s3u7n2e5l1m2p.nay8.uksouth.aroapp-hcp.azure-test.net`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("transport wrapped and plain TLS mismatch variants should canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if strings.Contains(strings.ToLower(gotA), `get "<url>`) {
		t.Fatalf("expected transport Get wrapper to be stripped from canonical phrase, got=%q", gotA)
	}
	if !strings.HasPrefix(strings.ToLower(gotA), "tls: failed to verify certificate:") {
		t.Fatalf("expected canonical phrase to keep the TLS mismatch detail, got=%q", gotA)
	}
}

func TestExtractEvidenceAlignsReleaseStatusInfoAndStepErrorDetails(t *testing.T) {
	t.Parallel()

	rawA := `time=2026-04-21T09:58:27.890Z level=INFO msg="Determined release status." release="mce-config" namespace="open-cluster-management" status=failed description="Release \"mce-config\" failed: resource not ready, name: grc-policy-propagator, kind: Deployment, status: InProgress\ncontext deadline exceeded"`
	rawB := `time=2026-04-21T09:58:28.016Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.ACM step=deploy-mce-config err="error running Helm release deployment Step, failed to deploy helm release: resource not ready, name: grc-policy-propagator, kind: Deployment, status: InProgress\ncontext deadline exceeded"`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("release-status info lines and step-error lines should canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	lowered := strings.ToLower(gotA)
	if strings.Contains(lowered, "determined release status") || strings.Contains(lowered, "error running helm release deployment step") {
		t.Fatalf("expected release wrapper text to be stripped from canonical phrase, got=%q", gotA)
	}
	if !strings.Contains(lowered, "resource not ready") || !strings.Contains(lowered, "grc-policy-propagator") {
		t.Fatalf("expected canonical phrase to retain the actionable release detail, got=%q", gotA)
	}
}

func TestExtractEvidenceNormalizesCleanupWorkflowDeletionVariants(t *testing.T) {
	t.Parallel()

	rawA := `failed to cleanup resource group: ordered cleanup workflow failed for admin-api-serialconsole-bootdiag-wfsm7c: Delete virtual networks: failed deleting sre-vnet-name (Microsoft.Network/virtualNetworks): DELETE https://management.azure.com/subscriptions/XXXX/resourceGroups/rg-a/providers/Microsoft.Network/virtualNetworks/sre-vnet-name`
	rawB := `failed to cleanup resource group: ordered cleanup workflow failed for admin-api-serialconsole-bootdiag-kgr5zt: Delete virtual networks: failed deleting customer-vnet (Microsoft.Network/virtualNetworks): GET https://management.azure.com/subscriptions/XXXX/resourceGroups/rg-b/providers/Microsoft.Network/virtualNetworks/customer-vnet`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("cleanup workflow deletion variants should canonicalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "<cleanup-target>") {
		t.Fatalf("expected cleanup target name to be normalized, got=%q", gotA)
	}
	if !strings.Contains(gotA, "<cleanup-resource>") {
		t.Fatalf("expected cleanup resource name to be normalized, got=%q", gotA)
	}
	if strings.Contains(strings.ToLower(gotA), "delete <url>") || strings.Contains(strings.ToLower(gotA), "get <url>") {
		t.Fatalf("expected transport method before cleanup URL to be normalized away, got=%q", gotA)
	}
}

func TestExtractEvidenceSplitsCandidateGraphTransportVariants(t *testing.T) {
	t.Parallel()

	rawDNS := `fail [github.com/Azure/ARO-HCP/test/util/framework/deployment_params.go:127]: failed to get latest install version for candidate channel: query candidate graph for candidate-4.20: Get "https://api.openshift.com/api/upgrades_info/v1/graph?channel=candidate-4.20": dial tcp: lookup api.openshift.com on 172.30.0.10:53: no such host`
	rawTimeout := `fail [github.com/Azure/ARO-HCP/test/util/framework/deployment_params.go:127]: failed to get latest install version for candidate channel: query candidate graph for candidate-4.20: Get "https://api.openshift.com/api/upgrades_info/v1/graph?channel=candidate-4.20": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`
	raw503 := `fail [github.com/Azure/ARO-HCP/test/util/framework/deployment_params.go:100]: failed to get latest install version for candidate channel: query candidate graph for candidate-4.20 returned 503 Service Unavailable: upstream connect error or disconnect/reset before headers. reset reason: overflow`

	gotDNS := extractEvidence(rawDNS).CanonicalEvidencePhrase
	gotTimeout := extractEvidence(rawTimeout).CanonicalEvidencePhrase
	got503 := extractEvidence(raw503).CanonicalEvidencePhrase

	if gotDNS == gotTimeout || gotDNS == got503 || gotTimeout == got503 {
		t.Fatalf("expected candidate-graph transport variants to stay distinct:\n  dns=%q\n  timeout=%q\n  503=%q", gotDNS, gotTimeout, got503)
	}
	if !strings.Contains(strings.ToLower(gotDNS), "no such host") {
		t.Fatalf("expected DNS lookup failure detail in canonical phrase, got=%q", gotDNS)
	}
	if !strings.Contains(strings.ToLower(gotTimeout), "awaiting headers") {
		t.Fatalf("expected client-timeout detail in canonical phrase, got=%q", gotTimeout)
	}
	if !strings.Contains(strings.ToLower(got503), "503 service unavailable") {
		t.Fatalf("expected 503 detail in canonical phrase, got=%q", got503)
	}
	if strings.Contains(gotDNS, "candidate-4.20") || strings.Contains(gotTimeout, "candidate-4.20") || strings.Contains(got503, "candidate-4.20") {
		t.Fatalf("expected candidate-channel version to be normalized away:\n  dns=%q\n  timeout=%q\n  503=%q", gotDNS, gotTimeout, got503)
	}
}

func TestExtractEvidenceMergesAzureManagedClusterResourceNotFoundVariants(t *testing.T) {
	t.Parallel()

	rawA := `time=2026-04-30T19:15:08.598Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=svc-mgmt-permissions err="failed to run ARM step: failed to poll deployment: failed to wait for deployment completion
ERROR CODE: DeploymentFailed
{ "error": { "code": "DeploymentFailed", "details": [ { "code": "ResourceNotFound", "message": "The Resource 'Microsoft.ContainerService/managedClusters/prow-j1425280-mgmt-1' under resource group 'hcp-underlay-prow-j1425280-mgmt-1' was not found." } ] } }"`
	rawB := `time=2026-05-04T07:35:50.067Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=svc-mgmt-permissions err="failed to run ARM step: failed to poll deployment: failed to wait for deployment completion
ERROR CODE: DeploymentFailed
{ "error": { "code": "DeploymentFailed", "details": [ { "code": "ResourceNotFound", "message": "The Resource 'Microsoft.ContainerService/managedClusters/prow-j7955840-mgmt-1' under resource group 'hcp-underlay-prow-j7955840-mgmt-1' was not found." } ] } }"`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase

	if gotA != gotB {
		t.Fatalf("expected generated managed-cluster names to normalize to the same canonical phrase:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "'Microsoft.ContainerService/managedClusters/<resource>'") {
		t.Fatalf("expected managed-cluster resource path placeholder, got=%q", gotA)
	}
	if !strings.Contains(gotA, "resource group '<resource-group>'") {
		t.Fatalf("expected quoted resource-group placeholder, got=%q", gotA)
	}
}

func TestExtractEvidenceMergesVaultAlreadyExistsAcrossGeneratedNames(t *testing.T) {
	t.Parallel()

	rawA := `fail [github.com/Azure/ARO-HCP/test/e2e/nodepool_version_upgrade.go:579]: failed to create cluster customer resources
Unexpected error:
    <*fmt.wrapError | 0xc0008a3560>: 
    failed to create customer-infra: failed waiting for deployment "customer-infra-np-downgrade-xrk9p4-fvbtlh" in resourcegroup="rg-np-downgrade-xrk9p4-7gbnwz" to finish: GET https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourcegroups/rg-np-downgrade-xrk9p4-7gbnwz/providers/Microsoft.Resources/deployments/customer-infra-np-downgrade-xrk9p4-fvbtlh/operationStatuses/08584175744362285058
    --------------------------------------------------------------------------------
    RESPONSE 200: 200 OK
    ERROR CODE: DeploymentFailed
    --------------------------------------------------------------------------------
    {
      "status": "Failed",
      "error": {
        "code": "DeploymentFailed",
        "message": "At least one resource deployment operation failed. Please list deployment operations for details. Please see https://aka.ms/arm-deployment-operations for usage details.",
        "details": [
          {
            "code": "Conflict",
            "message": "{\r\n  \"error\": {\r\n    \"code\": \"VaultAlreadyExists\",\r\n    \"message\": \"The vault name 'cust-kv-7rz373fqcqw3a' is already in use. Vault names are globally unique so it is possible that the name is already taken.\"\r\n  }\r\n}"
          }
        ]
      }
    }
    --------------------------------------------------------------------------------
    
    ...
occurred`
	rawB := `fail [github.com/Azure/ARO-HCP/test/e2e/control_plane_automated_z_stream_upgrade.go:92]: failed to create customer resources for z-stream cluster "cluster-zstream-4-20-lmtsfr"
Unexpected error:
    <*fmt.wrapError | 0xc000d10f60>: 
    failed to create customer-infra: failed waiting for deployment "customer-infra-cluster-zstream-4-20-lmtsfr-7g6lz7" in resourcegroup="rg-zstream-upgrade-4-20-g9whvx" to finish: GET https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourcegroups/rg-zstream-upgrade-4-20-g9whvx/providers/Microsoft.Resources/deployments/customer-infra-cluster-zstream-4-20-lmtsfr-7g6lz7/operationStatuses/08584175816013259434
    --------------------------------------------------------------------------------
    RESPONSE 200: 200 OK
    ERROR CODE: DeploymentFailed
    --------------------------------------------------------------------------------
    {
      "status": "Failed",
      "error": {
        "code": "DeploymentFailed",
        "message": "At least one resource deployment operation failed. Please list deployment operations for details. Please see https://aka.ms/arm-deployment-operations for usage details.",
        "details": [
          {
            "code": "Conflict",
            "message": "{\r\n  \"error\": {\r\n    \"code\": \"VaultAlreadyExists\",\r\n    \"message\": \"The vault name 'cust-kv-chfzla5wndhwk' is already in use. Vault names are globally unique so it is possible that the name is already taken.\"\r\n  }\r\n}"
          }
        ]
      }
    }
    --------------------------------------------------------------------------------
    
    ...
occurred`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase

	if gotA != gotB {
		t.Fatalf("expected generated vault names to normalize to the same canonical phrase:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(strings.ToLower(gotA), "'<vault-name>' is already in use") {
		t.Fatalf("expected vault-name placeholder in canonical phrase, got=%q", gotA)
	}
}

func TestExtractEvidenceNormalizesResourceGroupsPathSegmentInResourceID(t *testing.T) {
	t.Parallel()

	rawA := `fail [github.com/Azure/ARO-HCP/test/e2e/oidc_issuer_workload_identity.go:296]: failed to create federated identity credential for service account system:serviceaccount:e2e-oidc-wi:wi-test-sa
Unexpected error:
    <*exported.ResponseError | 0xc002600150>: 
    PUT https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/oidc-wi-2jj8pc/providers/Microsoft.ManagedIdentity/userAssignedIdentities/e2e-oidc-wi-test/federatedIdentityCredentials/e2e-wi-fic
    --------------------------------------------------------------------------------
    RESPONSE 404: 404 Not Found
    ERROR CODE: NotFound
    --------------------------------------------------------------------------------
    {
      "error": {
        "code": "NotFound",
        "message": "Resource '/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourcegroups/oidc-wi-2jj8pc/providers/Microsoft.ManagedIdentity/userAssignedIdentities/e2e-oidc-wi-test' was not found."
      }
    }
    --------------------------------------------------------------------------------
    
    ...
occurred`
	rawB := `fail [github.com/Azure/ARO-HCP/test/e2e/oidc_issuer_workload_identity.go:296]: failed to create federated identity credential for service account system:serviceaccount:e2e-oidc-wi:wi-test-sa
Unexpected error:
    <*exported.ResponseError | 0xc0029906c0>: 
    PUT https://management.azure.com/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/oidc-wi-67rdmn/providers/Microsoft.ManagedIdentity/userAssignedIdentities/e2e-oidc-wi-test/federatedIdentityCredentials/e2e-wi-fic
    --------------------------------------------------------------------------------
    RESPONSE 404: 404 Not Found
    ERROR CODE: NotFound
    --------------------------------------------------------------------------------
    {
      "error": {
        "code": "NotFound",
        "message": "Resource '/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourcegroups/oidc-wi-67rdmn/providers/Microsoft.ManagedIdentity/userAssignedIdentities/e2e-oidc-wi-test' was not found."
      }
    }
    --------------------------------------------------------------------------------
    
    ...
occurred`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase

	if gotA != gotB {
		t.Fatalf("expected generated resource-group names embedded in a resource ID path to normalize to the same canonical phrase:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "/resourcegroups/<resource-group>/") {
		t.Fatalf("expected resourcegroups path-segment placeholder, got=%q", gotA)
	}
}

func TestExtractEvidenceDistinguishesContextDeadlineFailuresByAssertionHeader(t *testing.T) {
	t.Parallel()

	rawAdminCred := `fail [github.com/Azure/ARO-HCP/test/e2e/admin_credential_lifecycle.go:191]: failed to poll admin credential 1 to completion
Unexpected error:
    <context.deadlineExceededError>: 
    context deadline exceeded
occurred`
	rawFirstCluster := `fail [github.com/Azure/ARO-HCP/test/e2e/clusters_sharing_resgroup.go:131]: failed to wait for first cluster "basic-hcp-cluster" to complete creation (timeout '20.000000' minutes)
Unexpected error:
    <context.deadlineExceededError>: 
    context deadline exceeded
occurred`
	rawSecondCluster := `fail [github.com/Azure/ARO-HCP/test/e2e/clusters_sharing_resgroup.go:138]: failed to wait for second cluster "basic-hcp-cluster2" to complete creation (timeout '20.000000' minutes)
Unexpected error:
    <context.deadlineExceededError>: 
    context deadline exceeded
occurred`

	gotAdminCred := extractEvidence(rawAdminCred).CanonicalEvidencePhrase
	gotFirstCluster := extractEvidence(rawFirstCluster).CanonicalEvidencePhrase
	gotSecondCluster := extractEvidence(rawSecondCluster).CanonicalEvidencePhrase

	if gotAdminCred == gotFirstCluster || gotAdminCred == gotSecondCluster || gotFirstCluster == gotSecondCluster {
		t.Fatalf("expected distinct assertion failures to stay distinct instead of collapsing into the generic wrapper:\n  adminCred=%q\n  firstCluster=%q\n  secondCluster=%q", gotAdminCred, gotFirstCluster, gotSecondCluster)
	}
	for _, got := range []string{gotAdminCred, gotFirstCluster, gotSecondCluster} {
		if strings.EqualFold(strings.TrimSpace(got), "context deadline exceeded") {
			t.Fatalf("expected canonical phrase to be more specific than the generic context-deadline wrapper, got=%q", got)
		}
	}
	if !strings.Contains(strings.ToLower(gotAdminCred), "failed to poll admin credential") {
		t.Fatalf("expected admin-credential detail in canonical phrase, got=%q", gotAdminCred)
	}
	if !strings.Contains(strings.ToLower(gotFirstCluster), "first cluster") {
		t.Fatalf("expected first-cluster detail in canonical phrase, got=%q", gotFirstCluster)
	}
	if !strings.Contains(strings.ToLower(gotSecondCluster), "second cluster") {
		t.Fatalf("expected second-cluster detail in canonical phrase, got=%q", gotSecondCluster)
	}
}

func TestExtractEvidenceNormalizesOnClusterPhrase(t *testing.T) {
	t.Parallel()

	rawA := `fail [ ]: expected exactly one external auth <external-auth> on cluster ea-listcf4xll`
	rawB := `fail [ ]: expected exactly one external auth <external-auth> on cluster ea-listz4ptjc`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase

	if gotA != gotB {
		t.Fatalf("expected generated cluster-name suffixes after 'on cluster' to normalize to the same canonical phrase:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "on cluster <cluster>") {
		t.Fatalf("expected 'on cluster' placeholder, got=%q", gotA)
	}
}

func TestExtractEvidenceMergesStepErrorProgressPollingVariants(t *testing.T) {
	t.Parallel()

	rawA := `time=2026-07-27T12:24:07.823Z level=INFO msg="Running step." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=swift-vnet stamp=1 description="Step swift-vnet\n  Kind: Shell\n  Command: ./swift-vnet.sh\n"
time=2026-07-27T12:33:11.965Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=swift-vnet stamp=1 err="stamp 1: error running Shell Step, failed to execute shell command: Launching container group swift-vnet-aks-net as globalMSI...\nWaiting for swift-vnet-aks-net to finish (state: pending, elapsed 2s)...\nWaiting for swift-vnet-aks-net to finish (state: Running, elapsed 240s)...\n=== swift-vnet-aks-net logs ===\n[swift-vnet] dns readiness (login.microsoftonline.com): attempt 1 failed (elapsed 5s/480s; likely container DNS cold-start or RBAC propagation); retrying in 5s...\n[swift-vnet] dns readiness (login.microsoftonline.com): attempt 48 failed (elapsed 477s/480s; likely container DNS cold-start or RBAC propagation); retrying in 5s...\n[swift-vnet] dns readiness (login.microsoftonline.com): giving up after 49 attempt(s) / 487s (limit 480s)\n\nCleaning up swift-vnet-aks-net...\n✗ swift-vnet-aks-net exited with code 1\n exit status 1"`
	rawB := `time=2026-07-21T21:47:26.084Z level=INFO msg="Running step." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=swift-vnet stamp=1 description="Step swift-vnet\n  Kind: Shell\n  Command: ./swift-vnet.sh\n"
time=2026-07-21T21:56:33.375Z level=ERROR msg="Step errored." serviceGroup=Microsoft.Azure.ARO.HCP.Management.Infra resourceGroup=management step=swift-vnet stamp=1 err="stamp 1: error running Shell Step, failed to execute shell command: Launching container group swift-vnet-aks-net as globalMSI...\nWaiting for swift-vnet-aks-net to finish (state: pending, elapsed 1s)...\nWaiting for swift-vnet-aks-net to finish (state: Running, elapsed 190s)...\n=== swift-vnet-aks-net logs ===\n[swift-vnet] dns readiness (login.microsoftonline.com): attempt 1 failed (elapsed 5s/480s; likely container DNS cold-start or RBAC propagation); retrying in 5s...\n[swift-vnet] dns readiness (login.microsoftonline.com): attempt 46 failed (elapsed 457s/480s; likely container DNS cold-start or RBAC propagation); retrying in 5s...\n[swift-vnet] dns readiness (login.microsoftonline.com): giving up after 47 attempt(s) / 465s (limit 480s)\n\nCleaning up swift-vnet-aks-net...\n✗ swift-vnet-aks-net exited with code 1\n exit status 1"`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase

	if gotA != gotB {
		t.Fatalf("expected identical DNS-readiness retry-loop failures to normalize to the same canonical phrase despite differing elapsed-time/attempt counters:\n  A=%q\n  B=%q", gotA, gotB)
	}
	lowered := strings.ToLower(gotA)
	if strings.Contains(lowered, "elapsed") || strings.Contains(lowered, "pending") {
		t.Fatalf("expected repetitive progress-polling detail to be excluded from canonical phrase, got=%q", gotA)
	}
	if !strings.Contains(lowered, "giving up after") {
		t.Fatalf("expected canonical phrase to use the terminal giving-up summary, got=%q", gotA)
	}
}

func TestExtractEvidenceSurfacesClusterServiceNoMessagePlaceholder(t *testing.T) {
	t.Parallel()

	// ClusterService returns this exact JSON error body when it fails but
	// provides no further detail. Go's default json.Marshal HTML-escapes
	// '<'/'>' as \u003c/\u003e, so real payloads carry that literal escaping.
	rawSingleLeaf := `fail [github.com/Azure/ARO-HCP/test/e2e/nodepool_autoscaling.go:101]: failed to create HCP cluster np-autoscale-cluster
Unexpected error:
    <*fmt.wrapError | 0xc0015ba420>: 
    failed to create HCP cluster np-autoscale-cluster: failed waiting for cluster="np-autoscale-cluster" in resourcegroup="np-autoscaling-zkzp62mtgcxk" to finish creating: GET https://rp.example.hcpsvc.osadev.cloud/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/32fedae4-2ad1-481c-9c94-83f625471ccf
    --------------------------------------------------------------------------------
    RESPONSE 200: 200 OK
    ERROR CODE: InternalServerError
    --------------------------------------------------------------------------------
    {
      "id": "/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/32fedae4-2ad1-481c-9c94-83f625471ccf",
      "name": "32fedae4-2ad1-481c-9c94-83f625471ccf",
      "status": "Failed",
      "error": {
        "code": "InternalServerError",
        "message": "[clusterServiceClusterStatus] \u003cno_message\u003e"
      }
    }
    --------------------------------------------------------------------------------
occurred`

	got := extractEvidence(rawSingleLeaf).CanonicalEvidencePhrase
	loweredGot := strings.ToLower(got)

	if strings.Contains(loweredGot, `\u003c`) {
		t.Fatalf("expected \\u003c/\\u003e HTML-escapes to be decoded to real angle brackets, got=%q", got)
	}
	if !strings.Contains(loweredGot, "<no_message>") {
		t.Fatalf("expected ClusterService's <no_message> placeholder to be surfaced in the canonical phrase (so it's evident the upstream service, not extraction, lacks detail), got=%q", got)
	}
	if !strings.Contains(loweredGot, "clusterserviceclusterstatus") {
		t.Fatalf("expected the leaf tag preceding <no_message> to be preserved, got=%q", got)
	}
}

func TestExtractEvidenceKeepsFullMessageWithEmbeddedEscapedQuote(t *testing.T) {
	t.Parallel()

	// Real RP response: the message legitimately contains an escaped quoted
	// substring. decodeEscapedErrorPayload's blanket \" -> " unescape (needed
	// to unwrap nested-JSON-in-string cases like VaultAlreadyExists) used to
	// make extractAzureMessageForCode's `[^"]+` capture stop at the first
	// unescaped quote, truncating the message down to "Invalid value:" and
	// silently dropping the actionable detail.
	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_fips_mode.go:84]: failed to create HCP cluster "fips-enabled-cluster" with FIPS enabled
Unexpected error:
    <*fmt.wrapError | 0xc000ccd860>: 
    failed starting cluster creation "fips-enabled-cluster" in resourcegroup="fips-enabled-m894z8": PUT https://management.azure.com/subscriptions/XXXX/resourceGroups/fips-enabled-m894z8/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/fips-enabled-cluster
    --------------------------------------------------------------------------------
    RESPONSE 400: 400 Bad Request
    ERROR CODE: InvalidRequestContent
    --------------------------------------------------------------------------------
    {
      "error": {
        "code": "InvalidRequestContent",
        "message": "Invalid value: \"aro-hcp.experimental.cluster.max-creation-duration\": unrecognized experimental tag",
        "target": "tags[aro-hcp.experimental.cluster.max-creation-duration]"
      }
    }
    --------------------------------------------------------------------------------
occurred`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "unrecognized experimental tag") {
		t.Fatalf("expected the full RP detail message (including its embedded escaped quote) to survive extraction instead of being truncated to just \"Invalid value:\", got=%q", got)
	}
	if !strings.Contains(got, "detail message") {
		t.Fatalf("expected a detail message segment in the canonical phrase, got=%q", got)
	}
}

func TestExtractEvidenceSurfacesResourceNotFoundDetailMessage(t *testing.T) {
	t.Parallel()

	// "ResourceNotFound" (the code RP actually returns) was missing from the
	// generic-code allowlist that gates detail-message extraction, so any
	// detail (here, the informative "under resource group ''" hint) was
	// silently dropped in favor of a bare "ERROR CODE: ResourceNotFound".
	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/external_auth_list_and_verify.go:86]: failed to create HCP cluster for external auth list test
Unexpected error:
    <*fmt.wrapError | 0xc000669440>: 
    failed to create HCP cluster ea-listsjvd5r: failed waiting for cluster="ea-listsjvd5r" in resourcegroup="ea-list-47q6dt" to finish creating: GET https://rp.example.hcpsvc.osadev.cloud/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/f5e67f73-1a1b-4a43-b367-a1a98b42834d
    --------------------------------------------------------------------------------
    RESPONSE 404: 404 Not Found
    ERROR CODE: ResourceNotFound
    --------------------------------------------------------------------------------
    {
      "error": {
        "code": "ResourceNotFound",
        "message": "The resource 'locations/hcpOperationStatuses/f5e67f73-1a1b-4a43-b367-a1a98b42834d' under resource group '' was not found.",
        "target": "/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/f5e67f73-1a1b-4a43-b367-a1a98b42834d"
      }
    }
    --------------------------------------------------------------------------------
occurred`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "detail message") || !strings.Contains(got, "under resource group") {
		t.Fatalf("expected ResourceNotFound's detail message to be surfaced instead of dropped, got=%q", got)
	}
}

func TestExtractEvidenceAlwaysSurfacesDetailForRedHatOpenShiftProviderUnknownCode(t *testing.T) {
	t.Parallel()

	// "QuotaExceeded" is deliberately NOT in genericCodes, simulating a
	// brand-new RP error code we haven't special-cased yet. Because
	// Microsoft.RedHatOpenShift is the provider under test, its detail
	// message must always be surfaced so future/unseen failure patterns are
	// complete without requiring another allowlist patch.
	raw := `PUT https://management.azure.com/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/hcpOpenShiftClusters/cluster
RESPONSE 429: 429 Too Many Requests
ERROR CODE: QuotaExceeded
{
  "error": {
    "code": "QuotaExceeded",
    "message": "Subscription quota for standardDSv5Family cores has been exceeded, requested 32, available 8"
  }
}`
	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "detail message") || !strings.Contains(got, "quota") {
		t.Fatalf("expected an unrecognized RedHatOpenShift error code to still surface its detail message, got=%q", got)
	}

	// The same unrecognized code for an unrelated provider must keep the
	// existing conservative (no-detail) behavior.
	rawOtherProvider := `PUT https://management.azure.com/subscriptions/XXXX/providers/Microsoft.Storage/storageAccounts/foo
RESPONSE 429: 429 Too Many Requests
ERROR CODE: QuotaExceeded
{
  "error": {
    "code": "QuotaExceeded",
    "message": "Subscription quota for storage accounts has been exceeded"
  }
}`
	gotOtherProvider := extractEvidence(rawOtherProvider).CanonicalEvidencePhrase
	if strings.Contains(gotOtherProvider, "detail message") {
		t.Fatalf("expected non-RedHatOpenShift providers to keep the existing allowlist-gated behavior, got=%q", gotOtherProvider)
	}
}

func TestExtractEvidenceSurfacesInvalidTemplateMessage(t *testing.T) {
	t.Parallel()

	rawA := `PUT https://management.azure.com/subscriptions/XXXX/providers/Microsoft.Resources/deployments/example
RESPONSE 400: 400 Bad Request
ERROR CODE: InvalidTemplate
{
  "error": {
    "code": "InvalidTemplate",
    "message": "Deployment template validation failed: 'The value for the template parameter 'integrationSubnetName' at line '1' and column '2857' is not provided.'"
  }
}`
	rawB := strings.Replace(rawA, "column '2857'", "column '2942'", 1)

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected template source positions to normalize identically:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "detail message") || !strings.Contains(gotA, "integrationSubnetName") {
		t.Fatalf("expected InvalidTemplate detail to be surfaced, got=%q", gotA)
	}
}

func TestExtractEvidenceUsesAssertionHeaderWhenExpectedErrorIsNil(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/cluster_create_private_ingress.go:176]: private ingress should not be reachable from outside the VNet, but connection succeeded
Expected an error to have occurred.  Got:
    <nil>: ...`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	want := "private ingress should not be reachable from outside the VNet, but connection succeeded"
	if got != want {
		t.Fatalf("expected assertion header detail instead of the generic Gomega message, got=%q want=%q", got, want)
	}
}

func TestExtractEvidenceMergesOperationTimeoutDurations(t *testing.T) {
	t.Parallel()

	rawA := `failed waiting for hcpCluster="kms-key-rotate-a" in resourcegroup="rg-a" to finish updating, caused by: timeout '18.000000' minutes exceeded during UpdateHCPCluster20260630 for cluster kms-key-rotate-a in resource group rg-a, error: context deadline exceeded`
	rawB := `failed waiting for hcpCluster="kms-key-rotate-b" in resourcegroup="rg-b" to finish updating, caused by: timeout '30.000000' minutes exceeded during UpdateHCPCluster20260630 for cluster kms-key-rotate-b in resource group rg-b, error: context deadline exceeded`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := "UpdateHCPCluster20260630 timed out after <minutes> minutes; context deadline exceeded"
	if gotA != want || gotB != want {
		t.Fatalf("expected timeout durations to merge on the operation and terminal cause:\n  A=%q\n  B=%q\nwant=%q", gotA, gotB, want)
	}
}

func TestExtractEvidenceKeepsCleanupTimeoutOperation(t *testing.T) {
	t.Parallel()

	raw := `failed to cleanup resource group: failed deleting hcp clusters in resourcegroup="rg-a", caused by: failed waiting for hcpCluster="cluster-a" in resourcegroup="rg-a" to finish deleting, caused by: timeout '25.000000' minutes exceeded during DeleteHCPCluster for cluster cluster-a in resource group rg-a, error: context deadline exceeded`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	want := "DeleteHCPCluster timed out after <minutes> minutes; context deadline exceeded"
	if got != want {
		t.Fatalf("expected cleanup wrapper truncation to preserve the timed-out operation, got=%q want=%q", got, want)
	}
}

func TestExtractEvidenceSurfacesAzureIdentityRootCause(t *testing.T) {
	t.Parallel()

	raw := `GET https://rp.example/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/id
ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "[clusterServiceNodePoolStatus] ClientCertificateCredential authentication failed. \nPOST https://login.microsoftonline.com/tenant/oauth2/v2.0/token\n{\n  \"error\": \"invalid_client\",\n  \"error_description\": \"AADSTS7000213: Invalid certificate chain. Trace ID: volatile Correlation ID: volatile Timestamp: 2026-07-31\"\n}"
  }
}`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "ClientCertificateCredential authentication failed") ||
		!strings.Contains(got, "AADSTS7000213: Invalid certificate chain.") {
		t.Fatalf("expected RP certificate-auth canonical to include the stable AADSTS root cause, got=%q", got)
	}
	if strings.Contains(got, "Trace ID") || strings.Contains(got, "Timestamp") {
		t.Fatalf("expected volatile identity diagnostics to be removed, got=%q", got)
	}
}

func TestExtractEvidenceMergesWrappedUnknownAuthorityFailures(t *testing.T) {
	t.Parallel()

	rawA := `VerifySimpleWebApp failed: [strict TLS verification] route was never reachable: Get "https://app-a.example": tls: failed to verify certificate: x509: certificate signed by unknown authority`
	rawB := `[strict TLS verification] route was never reachable: Get "https://app-b.example": tls: failed to verify certificate: x509: certificate signed by unknown authority`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := "tls: failed to verify certificate: x509: certificate signed by unknown authority"
	if gotA != want || gotB != want {
		t.Fatalf("expected TLS transport wrappers to merge on the certificate failure:\n  A=%q\n  B=%q\nwant=%q", gotA, gotB, want)
	}
}

func TestExtractEvidenceUsesAssertionContextForDetailLessAzureError(t *testing.T) {
	t.Parallel()

	raw := `fail [github.com/Azure/ARO-HCP/test/e2e/admin_api.go:339]: failed to retrieve serial console logs for VM "c9e6l7e5k5c8l7z-worker-vqgww-tm445"
Unexpected error:
    expected status 200 OK, got 500: {
        "error": {
            "code": "InternalServerError",
            "message": "Internal server error."
        }
    }
occurred`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "ERROR CODE: InternalServerError") ||
		!strings.Contains(got, "context failed to retrieve serial console logs") {
		t.Fatalf("expected the failing operation to qualify a detail-less Azure error, got=%q", got)
	}
	if strings.Contains(got, "c9e6l7e5k5c8l7z-worker-vqgww-tm445") {
		t.Fatalf("expected the generated VM name to be normalized, got=%q", got)
	}
}

func TestExtractEvidenceSummarizesClusterServiceDeletionDetails(t *testing.T) {
	t.Parallel()

	rawA := `GET https://rp.example/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/id
ERROR CODE: InternalServerError
{
  "error": {
    "code": "InternalServerError",
    "message": "cluster deletion did not complete before the deadline; [clusterServiceDeletion] ClusterService cluster /api/aro_hcp/v1alpha1/clusters/2rvfr07shlm141ejua6jgajchroj0t6r still exists (deletion dispatched at 2026-08-05T06:39:16Z); [clusterServiceStatus] ClusterService state is \"uninstalling\"; [descendantResources] remaining resources: 1 Microsoft.RedHatOpenShift/hcpOpenShiftClusters/nodePools, 1 microsoft.redhatopenshift/hcpopenshiftclusters/nodepools/serviceProviderNodePools, 1 microsoft.redhatopenshift/hcpopenshiftclusters/serviceProviderClusters"
  }
}`
	rawB := strings.ReplaceAll(rawA, "2rvfr07shlm141ejua6jgajchroj0t6r", "2s08kfkgmp6p466kbsrs48lm52bok333")
	rawB = strings.ReplaceAll(rawB, "2026-08-05T06:39:16Z", "2026-08-05T07:24:38Z")

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected cluster IDs and deletion timestamps to merge:\n  A=%q\n  B=%q", gotA, gotB)
	}
	for _, unwanted := range []string{"2rvfr07shlm141ejua6jgajchroj0t6r", "2s08kfkgmp6p466kbsrs48lm52bok333", "2026-08-05", "/api/aro_hcp/"} {
		if strings.Contains(gotA, unwanted) {
			t.Fatalf("expected transient ClusterService identity to be removed, got=%q", gotA)
		}
	}
	for _, detail := range []string{`state is "uninstalling"`, "nodePools", "serviceProviderNodePools", "serviceProviderClusters"} {
		if !strings.Contains(gotA, detail) {
			t.Fatalf("expected complete deletion detail %q, got=%q", detail, gotA)
		}
	}
	if strings.HasSuffix(gotA, "still") {
		t.Fatalf("expected a complete canonical sentence rather than a mid-clause truncation, got=%q", gotA)
	}
}

func TestExtractEvidenceKeepsClusterServiceDeletionVariantsDistinct(t *testing.T) {
	t.Parallel()

	base := `GET https://rp.example/subscriptions/XXXX/providers/Microsoft.RedHatOpenShift/locations/westus3/hcpOperationStatuses/id
ERROR CODE: InternalServerError
{"error":{"code":"InternalServerError","message":"%s"}}`
	withoutHostedCluster := `cluster deletion did not complete before the deadline; [clusterServiceDeletion] ClusterService cluster /api/aro_hcp/v1alpha1/clusters/2rvfr07shlm141ejua6jgajchroj0t6r still exists (deletion dispatched at 2026-08-05T06:39:16Z); [clusterServiceStatus] ClusterService state is "uninstalling"; [descendantResources] remaining resources: 1 microsoft.redhatopenshift/hcpopenshiftclusters/serviceProviderClusters`
	withHostedCluster := `cluster deletion did not complete before the deadline; [clusterServiceDeletion] ClusterService cluster /api/aro_hcp/v1alpha1/clusters/2s094mnvr386tf3h8e6ohtguvd9b65to still exists (deletion dispatched at 2026-08-05T07:24:38Z); [clusterServiceStatus] ClusterService state is "uninstalling"; [hostedCluster] HostedCluster still exists; [descendantResources] remaining resources: 1 microsoft.redhatopenshift/hcpopenshiftclusters/serviceProviderClusters`

	gotWithout := extractEvidence(fmt.Sprintf(base, withoutHostedCluster)).CanonicalEvidencePhrase
	gotWith := extractEvidence(fmt.Sprintf(base, withHostedCluster)).CanonicalEvidencePhrase
	if gotWithout == gotWith {
		t.Fatalf("expected hosted-cluster presence to remain a semantic merge boundary, got=%q", gotWith)
	}
	if !strings.Contains(gotWith, "[hostedCluster] HostedCluster still exists") {
		t.Fatalf("expected hosted-cluster detail to be retained, got=%q", gotWith)
	}
}

func TestExtractEvidenceNormalizesGeneratedRouteAndNetworkArtifacts(t *testing.T) {
	t.Parallel()

	rawA := `VerifySimpleWebApp failed: DNS for route host agnhost-e2e-sample-app-bmdd7.apps.aro.cilium-cl.dus8.j7009920.hcp.osadev.cloud did not resolve: DNS for agnhost-e2e-sample-app-bmdd7.apps.aro.cilium-cl.dus8.j7009920.hcp.osadev.cloud did not resolve within 10m0s (last error: lookup agnhost-e2e-sample-app-bmdd7.apps.aro.cilium-cl.dus8.j7009920.hcp.osadev.cloud on 172.30.0.10:53: no such host)`
	rawB := `VerifySimpleWebApp failed: DNS for route host agnhost-e2e-sample-app-q26kx.apps.aro.cilium-cl.0290.j8953600.hcp.osadev.cloud did not resolve: DNS for agnhost-e2e-sample-app-q26kx.apps.aro.cilium-cl.0290.j8953600.hcp.osadev.cloud did not resolve within 12m0s (last error: lookup agnhost-e2e-sample-app-q26kx.apps.aro.cilium-cl.0290.j8953600.hcp.osadev.cloud on 172.31.0.10:53: no such host)`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected generated route hosts, DNS servers, and durations to merge:\n  A=%q\n  B=%q", gotA, gotB)
	}
	for _, artifact := range []string{"bmdd7", "q26kx", "j7009920", "j8953600", "172.30.0.10", "172.31.0.10", "10m0s", "12m0s"} {
		if strings.Contains(gotA, artifact) {
			t.Fatalf("expected route/network artifact %q to be normalized, got=%q", artifact, gotA)
		}
	}
}

func TestExtractEvidenceNormalizesKubernetesTestResourceNames(t *testing.T) {
	t.Parallel()

	rawA := `pods "policy-test-allowed" is forbidden: error looking up service account image-policy-test-bc57p/default: serviceaccount "default" not found`
	rawB := `pods "policy-test-other" is forbidden: error looking up service account image-policy-test-vghkn/default: serviceaccount "default" not found`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected generated pod and namespace names to merge:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, `pod "<pod>"`) || !strings.Contains(gotA, "service account <namespace>/default") {
		t.Fatalf("expected Kubernetes resource placeholders, got=%q", gotA)
	}
}

func TestExtractEvidenceSummarizesBreakglassSessionPath(t *testing.T) {
	t.Parallel()

	rawA := `failed to get ready session kubeconfig from /admin/v1/hcp/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourcegroups/admin-api-breakglass-ps66xjjnqrfj/providers/microsoft.redhatopenshift/hcpopenshiftclusters/sre-hcp-cluster/breakglass/breakglass-h7gq5/kubeconfig: timeout waiting for session to become ready (last status: {"HostedControlPlaneAvailable":{"message":"HostedControlPlane exists but is not ready"}})`
	rawB := strings.ReplaceAll(rawA, "admin-api-breakglass-ps66xjjnqrfj", "admin-api-breakglass-other")
	rawB = strings.ReplaceAll(rawB, "breakglass-h7gq5", "breakglass-other")

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := "breakglass session kubeconfig not ready: timeout waiting for session to become ready; HostedControlPlane exists but is not ready"
	if gotA != want || gotB != want {
		t.Fatalf("expected stable breakglass readiness summary:\n  A=%q\n  B=%q\nwant=%q", gotA, gotB, want)
	}
}

func TestExtractEvidenceNormalizesAzureOperationMetadata(t *testing.T) {
	t.Parallel()

	rawA := `ERROR CODE: DeploymentFailed
{"error":{"code":"DeploymentFailed","details":[{"code":"OperationNotAllowed","message":"Operation is not allowed because there's an in-progress create node pool operation (operation ID: 81d7fb37-c8d0-453c-9a89-3b0ab2b05ec0) on agent pool userswft1 started on UTC 2026-07-22T15:54:24Z. Please wait for it to finish before starting a new operation."}]}}`
	rawB := strings.ReplaceAll(rawA, "81d7fb37-c8d0-453c-9a89-3b0ab2b05ec0", "92e8ac48-d9e1-564d-ab90-4c1bc3c16fd1")
	rawB = strings.ReplaceAll(rawB, "userswft1", "usersabcd")
	rawB = strings.ReplaceAll(rawB, "2026-07-22T15:54:24Z", "2026-08-05T07:12:01Z")

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected operation IDs, agent-pool names, and timestamps to merge:\n  A=%q\n  B=%q", gotA, gotB)
	}
	for _, artifact := range []string{"81d7fb37", "92e8ac48", "userswft1", "usersabcd", "2026-"} {
		if strings.Contains(gotA, artifact) {
			t.Fatalf("expected operation artifact %q to be normalized, got=%q", artifact, gotA)
		}
	}
}

func TestExtractEvidenceSummarizesDenyAssignmentWithoutResourceIDs(t *testing.T) {
	t.Parallel()

	raw := `ERROR CODE: InternalServerError
{"error":{"code":"InternalServerError","details":[{"code":"DenyAssignmentAuthorizationFailed","message":"The client 'XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX' with object id '0e96dbb7-17a2-4393-b2d3-d84524e39c32' has permission to perform action 'Microsoft.Compute/virtualMachines/retrieveBootDiagnosticsData/action' on scope '/subscriptions/XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/resourceGroups/generated/providers/Microsoft.Compute/virtualMachines/generated'; however, the access is denied because of the deny assignment with Id '4e1d5354563b53f0a6639624ce82fcc6'."}]}}`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, `Access denied by deny assignment for action "Microsoft.Compute/virtualMachines/retrieveBootDiagnosticsData/action".`) {
		t.Fatalf("expected stable deny-assignment summary, got=%q", got)
	}
	for _, artifact := range []string{"0e96dbb7", "4e1d5354", "resourceGroups/generated"} {
		if strings.Contains(got, artifact) {
			t.Fatalf("expected deny-assignment artifact %q to be removed, got=%q", artifact, got)
		}
	}
}

func TestExtractEvidenceNormalizesHostedClusterComponentLists(t *testing.T) {
	t.Parallel()

	rawA := `ERROR CODE: InternalServerError
{"error":{"code":"InternalServerError","message":"[clusterServiceClusterStatus] <no_message>; [hypershiftHostedCluster] hosted cluster is not available: ComponentsNotAvailable: Waiting for components to be available: packageserver, catalog-operator, olm-operator; hosted cluster degraded: UnavailableReplicas: [packageserver deployment has 3 unavailable replicas, catalog-operator deployment has 1 unavailable replicas]"}}`
	rawB := `ERROR CODE: InternalServerError
{"error":{"code":"InternalServerError","message":"[clusterServiceClusterStatus] <no_message>; [hypershiftHostedCluster] hosted cluster is not available: ComponentsNotAvailable: Waiting for components to be available: olm-operator, catalog-operator, packageserver; hosted cluster degraded: UnavailableReplicas: [catalog-operator deployment has 2 unavailable replicas, packageserver deployment has 1 unavailable replicas]"}}`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	if gotA != gotB {
		t.Fatalf("expected component ordering and replica counts to merge:\n  A=%q\n  B=%q", gotA, gotB)
	}
	if !strings.Contains(gotA, "ComponentsNotAvailable") ||
		!strings.Contains(gotA, "UnavailableReplicas: [catalog-operator, packageserver]") {
		t.Fatalf("expected compact complete hosted-cluster summary, got=%q", gotA)
	}
	if strings.Contains(gotA, "deployment has") {
		t.Fatalf("expected volatile replica counts to be removed, got=%q", gotA)
	}
}

func TestExtractEvidenceUsesLastLogfmtErrorField(t *testing.T) {
	t.Parallel()

	raw := `time=2026-08-04T21:26:06.127Z level=ERROR msg="Failed to roll out the Helm release." err="resource Deployment/clusters-service/clusters-service not ready. status: InProgress, message: Available: 2/3\ncontext deadline exceeded"
time=2026-08-04T21:26:06.127Z level=ERROR msg="Step errored." stamp=1 err="stamp 1: error running Helm release deployment Step, failed to deploy helm release: failed to roll out Helm release: failed post-install: resource Job/multicluster-engine/finalize-mce-config not ready. status: InProgress, message: Job in progress\ncontext deadline exceeded"`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	if !strings.Contains(got, "resource Job/multicluster-engine/finalize-mce-config not ready") ||
		!strings.HasSuffix(got, "context deadline exceeded") {
		t.Fatalf("expected the final step error without logfmt boilerplate or truncation, got=%q", got)
	}
	if strings.Contains(got, "level=ERROR") || strings.Contains(got, "stamp 1") {
		t.Fatalf("expected logfmt wrapper metadata to be removed, got=%q", got)
	}
}

func TestExtractEvidenceSummarizesRepeatedConfigMapPatchTimeouts(t *testing.T) {
	t.Parallel()

	raw := `time=2026-07-23T14:45:42.101Z level=ERROR msg="Step errored." stamp=1 err="stamp 1: error running Helm release deployment Step, failed to deploy helm release: failed to roll out Helm release: the server was unable to return a response in the time allotted, but may still be processing the request (patch configmaps arohcp-monitor-kube-state-metrics-customresourcestate-config)\nthe server was unable to return a response in the time allotted, but may still be processing the request (patch configmaps ama-metrics-settings-configmap)"`

	got := extractEvidence(raw).CanonicalEvidencePhrase
	want := "server timed out while patching configmaps: ama-metrics-settings-configmap, arohcp-monitor-kube-state-metrics-customresourcestate-config"
	if got != want {
		t.Fatalf("expected complete stable configmap timeout summary, got=%q want=%q", got, want)
	}
}

func TestExtractEvidenceNormalizesAlertResourceInstances(t *testing.T) {
	t.Parallel()

	rawA := `Description: Pod ocm-arohcpci01-2s0a6l36dmn4749eat6ubck0rul1qp4j-cilium-cluster/router-7574555b5-qzbmk has been in a non-ready state for longer than 5 minutes.`
	rawB := `Description: Pod ocm-arohcpci01-2s0a5dce7ual13qn1ges3qb3jbgbe1nu-cilium-cluster/router-56bb9d76bf-b7z7q has been in a non-ready state for longer than 5 minutes.`

	gotA := extractEvidence(rawA).CanonicalEvidencePhrase
	gotB := extractEvidence(rawB).CanonicalEvidencePhrase
	want := `Description: Pod <namespace>/router-<pod> has been in a non-ready state for longer than 5 minutes.`
	if gotA != want || gotB != want {
		t.Fatalf("expected hosted namespace and pod replicas to merge:\n  A=%q\n  B=%q\nwant=%q", gotA, gotB, want)
	}
}

func TestExtractEvidenceNormalizesAlertNodesAndKlusterletNamespaces(t *testing.T) {
	t.Parallel()

	nodeA := `Description: aks-userswft1-20760849-vmss000004 has been unready for more than 30 minutes.`
	nodeB := `Description: aks-userswft2-99188438-vmss000001 has been unready for more than 30 minutes.`
	if gotA, gotB := extractEvidence(nodeA).CanonicalEvidencePhrase, extractEvidence(nodeB).CanonicalEvidencePhrase; gotA != gotB || gotA != "Description: node <node> has been unready for more than 30 minutes." {
		t.Fatalf("expected alert node identities to merge, got A=%q B=%q", gotA, gotB)
	}

	leaseA := `Description: Leader election lease governance-policy-framework in namespace klusterlet-2rvbf26ip74iaa4ddjchlnnkh26anmhi on cluster customer-a has not been renewed for more than 37 minutes. The leadership election might be broken or the component stopped running.`
	leaseB := `Description: Leader election lease governance-policy-framework in namespace klusterlet-2roge5s0ptig9l3e5sfu6hkausj66nj9 on cluster customer-b has not been renewed for more than 37 minutes. The leadership election might be broken or the component stopped running.`
	gotA := extractEvidence(leaseA).CanonicalEvidencePhrase
	gotB := extractEvidence(leaseB).CanonicalEvidencePhrase
	if gotA != gotB || !strings.Contains(gotA, "namespace <namespace> on cluster <cluster>") {
		t.Fatalf("expected klusterlet namespace and cluster identities to merge, got A=%q B=%q", gotA, gotB)
	}
}

func TestExtractEvidenceDoesNotTruncateAlertDescription(t *testing.T) {
	t.Parallel()

	raw := `Description: More than 72% of cluster create operations are in failed state, indicating a fast error budget burn that would exhaust the SLO budget. A regional install failure of this magnitude typically points at a shared infrastructure dependency rather than an individual test failure.`
	got := extractEvidence(raw).CanonicalEvidencePhrase
	if got != raw {
		t.Fatalf("expected the complete alert description without arbitrary truncation, got=%q", got)
	}
}

func TestExtractEvidenceNormalizesAlertLabelDeploymentAndNodePoolAssignment(t *testing.T) {
	t.Parallel()

	labels := `Labels: alertname="KubeDeploymentRolloutStuck", deployment="klusterlet-2rvavta7vgp33jl81jf2v6d5suo9dls1-work-agent", instance="10.128.64.244:8080"`
	gotLabels := extractEvidence(labels).CanonicalEvidencePhrase
	if strings.Contains(gotLabels, "2rvavta7vgp33jl81jf2v6d5suo9dls1") ||
		!strings.Contains(gotLabels, `deployment="klusterlet-<cluster>-work-agent"`) {
		t.Fatalf("expected generated klusterlet deployment label to be normalized, got=%q", gotLabels)
	}

	nodePool := `list nodes (nodePool=npdg-4-21): Get "https://api.example": net/http: TLS handshake timeout`
	gotNodePool := extractEvidence(nodePool).CanonicalEvidencePhrase
	if strings.Contains(gotNodePool, "npdg-4-21") || !strings.Contains(gotNodePool, "nodePool=<nodepool>") {
		t.Fatalf("expected nodePool assignment to be normalized, got=%q", gotNodePool)
	}
}
