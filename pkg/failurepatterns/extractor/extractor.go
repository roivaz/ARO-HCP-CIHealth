package extractor

import (
	"regexp"
	"sort"
	"strings"
)

var (
	reProviderPath              = regexp.MustCompile(`/providers/(Microsoft\.[A-Za-z]+(?:\.[A-Za-z]+)?)/`)
	reProviderText              = regexp.MustCompile(`(Microsoft\.[A-Za-z]+(?:\.[A-Za-z]+)?)`)
	reCleanFmtWrap              = regexp.MustCompile(`<\*[^>]+\|\s*0x[0-9a-fA-F]+>\s*:?`)
	reCleanHexAddress           = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reCleanGoFileLine           = regexp.MustCompile(`\b\w[\w./-]*\.go:\d+\b`)
	reCleanUnexpectedPrefix     = regexp.MustCompile(`(?i)^\s*unexpected error:\s*`)
	reCleanWrapperPrefix        = regexp.MustCompile(`(?i)^\s*(msg:|err:|caused by:)\s*`)
	reCleanURL                  = regexp.MustCompile(`https?://[^\s"'<>]+`)
	reCleanSubscription         = regexp.MustCompile(`/subscriptions/[0-9a-fA-F-]+`)
	reCleanRedactedSubscription = regexp.MustCompile(`(?i)/subscriptions/X{20,}`)
	// Azure resource-ID path segment, e.g. .../resourcegroups/oidc-wi-2jj8pc/...
	// The generated resource-group name is instance-specific; normalize so the
	// same class of error merges across runs.
	reCleanResourceGroupsPathSegment = regexp.MustCompile(`(?i)/resourcegroups/[a-z0-9-]+/`)
	// "The vault name '<name>' is already in use." — the Key Vault name embeds a
	// random suffix (and may contain hyphens), so it will not match the generic
	// 20+ char alnum opaque-ID regex below. Normalize explicitly so repeated
	// vault-name-collision failures merge into a single pattern.
	reCleanVaultNameAlreadyInUse  = regexp.MustCompile(`(?i)the vault name '[^']+' is already in use\.?`)
	reCleanUUID                   = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F-]{27,}\b`)
	reCleanHexLong                = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reCleanLookupHostOnServer     = regexp.MustCompile(`(?i)\blookup\s+[a-z0-9.-]+\s+on\s+\d{1,3}(?:\.\d{1,3}){3}:\d+\b`)
	reCleanResourceGroupQuoted    = regexp.MustCompile(`(?i)resourcegroup="[^"]+"`)
	reCleanResourceGroupBare      = regexp.MustCompile(`(?i)\bresource group [a-z0-9-]+\b`)
	reCleanResourceGroupSingle    = regexp.MustCompile(`(?i)\bresource group '[^']+'`)
	reCleanClusterQuoted          = regexp.MustCompile(`(?i)cluster="[^"]+"`)
	reCleanClusterPhraseQuoted    = regexp.MustCompile(`(?i)\bcluster "[^"]+"`)
	reCleanClusterCreationQuoted  = regexp.MustCompile(`(?i)\bcluster creation ["'][^"']+["']`)
	reCleanForClusterPhrase       = regexp.MustCompile(`(?i)\bfor cluster [a-z0-9-]+\b`)
	reCleanInClusterPhrase        = regexp.MustCompile(`(?i)\bin cluster [a-z0-9-]+\b`)
	reCleanOnClusterPhrase        = regexp.MustCompile(`(?i)\bon cluster [a-z0-9-]+\b`)
	reCleanHCPClusterPhrase       = regexp.MustCompile(`(?i)\bhcp cluster [a-z0-9-]+\b`)
	reCleanExternalAuthQuoted     = regexp.MustCompile(`(?i)external auth "[^"]+"`)
	reCleanExternalAuthBare       = regexp.MustCompile(`(?i)\bexternal auth [a-z0-9-]+\b`)
	reCleanVMQuoted               = regexp.MustCompile(`(?i)\bVM "[^"]+"`)
	reCleanPodQuoted              = regexp.MustCompile(`(?i)\bpods? "[^"]+"`)
	reCleanNamespaceQuoted        = regexp.MustCompile(`(?i)\bnamespaces? "[^"]+"`)
	reCleanServiceAccountPath     = regexp.MustCompile(`(?i)\bservice account [^/\s]+/([a-z0-9-]+)`)
	reCleanNodePoolQuoted         = regexp.MustCompile(`(?i)nodepool="[^"]+"`)
	reCleanNodePoolAssignment     = regexp.MustCompile(`(?i)nodePool=[a-z0-9-]+`)
	reCleanNodePoolPhrase         = regexp.MustCompile(`(?i)\bnode pool [a-z0-9-]+\b`)
	reCleanNodePoolPhraseQuoted   = regexp.MustCompile(`(?i)\bnode pool "[^"]+"`)
	reCleanAgentPool              = regexp.MustCompile(`(?i)\bagent pool [a-z0-9-]+\b`)
	reBoolAssertionContext        = regexp.MustCompile(`(?is)Timed out after [0-9.]+s\.\s*\n(?P<context>[^\n]+)\s*\nExpected\s*\n\s*<bool>:\s*false\s*\n\s*to be true`)
	reAssertionRegexHint          = regexp.MustCompile(`Regexp:\s*"([^"]+)"`)
	reAssertionErrorSignal        = regexp.MustCompile(`(?i)(error|failed|timeout|forbidden|denied|conflict|deadline|not found|invalid|http2:)`)
	reSafeErrorLineSignal         = regexp.MustCompile(`(?i)(error|failed|timeout|not found|forbidden|denied|deadline|conflict)`)
	reEventuallyWrapperLine       = regexp.MustCompile(`(?i)^the function passed to eventually failed at .+ with:?$`)
	reTimedOutAfterLine           = regexp.MustCompile(`(?i)^timed out after [0-9.]+s\.`)
	reCodeField                   = regexp.MustCompile(`"code"\s*:\s*"([A-Za-z0-9_]+)"`)
	reCauseBySplit                = regexp.MustCompile(`(?i)caused by:`)
	reErrorCode                   = regexp.MustCompile(`(?i)ERROR CODE:\s*([A-Za-z0-9_]+)`)
	rePickErrorSignal             = regexp.MustCompile(`(?i)(error|failed|timeout|forbidden|denied|conflict|deadline|not found)`)
	reHTTPResponseStatusLine      = regexp.MustCompile(`(?i)^response [45][0-9]{2}:\s*.+$`)
	reRouteHostNeverFound         = regexp.MustCompile(`(?i)route host was never found:[^\n]+`)
	reClusterOperatorsUnavailable = regexp.MustCompile(`(?i)cluster operators not available:[^\n]+`)
	reRateLimiterDeadline         = regexp.MustCompile(`(?i)client rate limiter wait returned an error: context deadline exceeded`)
	// Ginkgo assertion header, e.g. `fail [file.go:191]: failed to poll admin
	// credential 1 to completion`. Normally excluded as wrapper noise, but used
	// as a last-resort fallback so a generic "context deadline exceeded" block
	// doesn't discard the only test-specific detail available (the message
	// following the file:line marker).
	reFailAssertionHeader = regexp.MustCompile(`(?i)^fail \[[^\]]*\]:\s*(.+)$`)
	// Step-progress polling noise: repeated "Waiting for X to finish (state:
	// Y, elapsed Ns)..." lines and DNS/service-readiness retry-loop attempt
	// lines. Both vary only by an elapsed-seconds/attempt counter and carry no
	// distinguishing failure signal, so they must never be picked as the
	// canonical evidence phrase.
	reWaitingToFinishProgressLine = regexp.MustCompile(`(?i)^waiting for [^)]+\(state:\s*[^,]+,\s*elapsed \d+s\)\.\.\.$`)
	reRetryAttemptProgressLine    = regexp.MustCompile(`(?i)\battempt \d+ failed \(elapsed \d+s/\d+s;[^)]*\);\s*retrying in \d+s\.\.\.$`)
	// Terminal "giving up after N attempt(s) / Xs (limit Ys)" summary line
	// that follows a retry loop; used as a last-resort stable canonical when
	// no other distinguishing error line is found.
	reGivingUpAfterAttempts           = regexp.MustCompile(`(?i)giving up after \d+ attempt\(s\) / \d+s \(limit \d+s\)`)
	reUnexpectedOnly                  = regexp.MustCompile(`(?i)unexpected error:?`)
	reFailurePatternPlaceholder       = regexp.MustCompile(`<uuid>|<hex>|<url>`)
	reDeserializationLiteral          = regexp.MustCompile(`(?i)Deserializa(?:ti|i)on Error:[^\n]+`)
	reDeserializationNoOutput         = regexp.MustCompile(`(?i)Deserializa(?:ti|i)on Error:\s*no output from command`)
	reDeserializationToken            = regexp.MustCompile(`(?i)deserializa(?:ti|i)on error`)
	reCommandErrorLine                = regexp.MustCompile(`(?im)^Command Error:\s*[^\n]+$`)
	reQuotaRequiredAvailable          = regexp.MustCompile(`(?i)\brequired\s+['"]?\d+['"]?\s*,\s*available\s+['"]?\d+['"]?\b`)
	reTimeoutMinutesExceeded          = regexp.MustCompile(`(?i)timeout\s+'\d+(?:\.\d+)?'\s+minutes exceeded`)
	reOperationTimeout                = regexp.MustCompile(`(?i)timeout\s+'(?:\d+(?:\.\d+)?|<minutes>)'\s+minutes exceeded during ([A-Za-z0-9_]+)`)
	reTemplateLineColumn              = regexp.MustCompile(`(?i)\bat line '\d+' and column '\d+'`)
	reExpectedErrorButNil             = regexp.MustCompile(`(?i)expected an error to have occurred\.\s*got:\s*\n\s*<nil>`)
	reAzureErrorDescription           = regexp.MustCompile(`(?is)"error_description"\s*:\s*"([^"]+)"`)
	reClusterServiceState             = regexp.MustCompile(`(?i)\bstate is "([^"]+)"`)
	reClusterServiceClusterRef        = regexp.MustCompile(`(?i)/api/aro_hcp/v1alpha1/clusters/[a-z0-9]+`)
	reDeletionDispatchedAt            = regexp.MustCompile(`(?i)\s*\(deletion dispatched at [^)]+\)`)
	reLeadingResourceCount            = regexp.MustCompile(`^\d+\s+`)
	reInvalidTemplateParameter        = regexp.MustCompile(`(?i)deployment template validation failed:\s*'the value for the template parameter '([^']+)'.*?is not provided\.`)
	reDenyAssignmentAction            = regexp.MustCompile(`(?i)perform action '([^']+)'`)
	reNetworkAssociationError         = regexp.MustCompile(`(?i)error message:\s*(.+)$`)
	reUnavailableDeployment           = regexp.MustCompile(`(?i)^([a-z0-9-]+) deployment has \d+ unavailable replicas?$`)
	reStampPrefix                     = regexp.MustCompile(`(?i)^stamp \d+:\s*`)
	reServerPatchTimeout              = regexp.MustCompile(`(?i)the server was unable to return a response in the time allotted, but may still be processing the request \(patch configmaps ([^)]+)\)`)
	reAlertResourceRef                = regexp.MustCompile(`(?i)\b(Pod|Deployment|StatefulSet|DaemonSet)\s+([a-z0-9-]+)/([a-z0-9-]+)`)
	reAlertContainerPod               = regexp.MustCompile(`(?i)\bin pod ([a-z0-9-]+)/([a-z0-9-]+)`)
	reAlertPodInNamespace             = regexp.MustCompile(`(?i)\bpod/([a-z0-9-]+) in namespace ([a-z0-9-]+)`)
	reAlertNodeDescription            = regexp.MustCompile(`(?i)^(Description:\s+)[a-z0-9-]+(\s+(?:has been unready|is unreachable)\b)`)
	reAlertHostedCluster              = regexp.MustCompile(`(?i)\bon hosted cluster [a-z0-9-]+ \(management cluster [a-z0-9-]+\)`)
	reAlertNamespace                  = regexp.MustCompile(`(?i)\bin namespace (?:ocm-arohcpci01-[a-z0-9-]+|klusterlet-[a-z0-9-]+|[a-z0-9]{24,})\b`)
	reAlertPodCount                   = regexp.MustCompile(`(?i)^(Description:\s+)\d+ Pods\b`)
	reAlertKustoCluster               = regexp.MustCompile(`(?i)(Kusto cluster )[a-z0-9-]+`)
	reGetPodsName                     = regexp.MustCompile(`(?i)\bget pods [a-z0-9-]+`)
	reNotReadyNodes                   = regexp.MustCompile(`(?i)(not ready nodes:\s*)\[[^\]]+\]`)
	rePodReplicaSuffix                = regexp.MustCompile(`(?i)^(.+)-[a-f0-9]{8,10}-[a-z0-9]{5}$`)
	rePodShortSuffix                  = regexp.MustCompile(`(?i)^(.+)-[a-z0-9]{5}$`)
	rePodOrdinalSuffix                = regexp.MustCompile(`(?i)^(.+)-\d+$`)
	reKlusterletDeployment            = regexp.MustCompile(`(?i)^klusterlet-[a-z0-9]{20,}-(.+)$`)
	reCleanKlusterletDeploymentQuoted = regexp.MustCompile(`(?i)(deployment=")klusterlet-[a-z0-9]{20,}-(.+?)(")`)
	reWrapperStepErroredContainer     = regexp.MustCompile(`(?i)step errored`)
	reModelDiffSummary                = regexp.MustCompile(`(?i)operation result model did not match expected model[^\n]*`)
	reX509CertificateMismatch         = regexp.MustCompile(`(?i)tls: failed to verify certificate: x509: [^\n"]+`)
	reTimedOutAfterDuration           = regexp.MustCompile(`(?i)^timed out after [0-9]+(?:\.[0-9]+)?s\.$`)
	reTimedOutAssertionWrapper        = regexp.MustCompile(`(?i)^fail \[[^\]]*\]:\s*(timed out after [0-9]+(?:\.[0-9]+)?s\.)$`)
	rePlaceholderAssertionValue       = regexp.MustCompile(`^<[^>\n]+>:\s*\.{3}\s*$`)
	reStructuredFieldLine             = regexp.MustCompile(`^[+-]?\s*[A-Za-z_][A-Za-z0-9_]+:\s+.*(?:,\s*|\{\s*)$`)
	reLogfmtReleaseStatusDesc         = regexp.MustCompile(`(?i)level=info[^\n]*msg="determined release status\."[^\n]*description="((?:\\.|[^"])*)"`)
	reCleanupWorkflowTarget           = regexp.MustCompile(`(?i)(ordered cleanup workflow failed for )([a-z0-9-]+)(:)`)
	reCleanupWorkflowMethodURL        = regexp.MustCompile(`(?i):\s*(?:GET|POST|PUT|PATCH|DELETE)\s+<url>`)
	reCleanupWorkflowResourceName     = regexp.MustCompile(`(?i)(failed deleting )([a-z0-9-]+)( \([^)]+\):)`)

	// Dial-TCP address: normalize raw IPs left behind after URL masking.
	reCleanDialTCPAddress = regexp.MustCompile(`\bdial tcp \d{1,3}(?:\.\d{1,3}){3}:\d+\b`)
	// Istio/envoy "dialing <ip>:<port>" — same IP/port noise, different verb.
	reCleanDialingAddress = regexp.MustCompile(`\bdialing \d{1,3}(?:\.\d{1,3}){3}:\d+\b`)
	reCleanIP             = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`)
	reCleanBracketedIPv6  = regexp.MustCompile(`\[[0-9a-fA-F]*:[0-9a-fA-F:]+\](?::\d+)?`)
	reCleanISOTimestamp   = regexp.MustCompile(`\b20[0-9]{2}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z\b`)
	reCleanResolveWithin  = regexp.MustCompile(`(?i)\bdid not resolve within [0-9]+(?:h[0-9]+m)?(?:m[0-9]+s|s)\b`)
	reCleanRouteHost      = regexp.MustCompile(`(?i)\bagnhost-e2e-sample-app-[a-z0-9]+\.apps\.[a-z0-9.-]+\b`)
	reCleanBreakglassPath = regexp.MustCompile(`(?i)/hcpopenshiftclusters/[^/]+/breakglass/[^/]+/kubeconfig`)
	reCleanRedactedQuoted = regexp.MustCompile(`"X{20,}"`)
	reCleanFailEmpty      = regexp.MustCompile(`(?i)^\s*fail\s*\[\s*\]:\s*`)
	reCleanSelector       = regexp.MustCompile(`(?i)\bselector "[^"]+"`)
	reExpectedReadyNodes  = regexp.MustCompile(`(?i)\bexpected \d+ ready \(and schedulable\) nodes\b[^,]*,\s*found \d+\b`)
	// Logfmt-style timestamp (e.g. time=2026-04-17T11:04:19.211Z).
	reCleanLogfmtTimestamp = regexp.MustCompile(`\btime=[0-9]{4}-[0-9]{2}-[0-9]{2}T[A-Z0-9:.]+\s*`)
	// JSON "time" field with ISO-8601 value (e.g. prow entrypoint logs).
	reCleanJSONTimeField = regexp.MustCompile(`"time"\s*:\s*"[0-9]{4}-[0-9]{2}-[0-9]{2}T[^"]*",?\s*`)
	// Prow entrypoint single-line JSON: extract just the msg value.
	reProwEntrypointMsg = regexp.MustCompile(`"component"\s*:\s*"entrypoint"[^}]{0,400}"msg"\s*:\s*"([^"]+)"`)
	// All CreateHCPCluster*FromParam / *AndWait helper variants.
	reCreateHCPClusterTimeout = regexp.MustCompile(`(?i)createhcpcluster\w*(?:fromparam|andwait)`)
	// Gomega "Expected success, but got an error:" followed by optional type
	// wrapper line, then the real error message.
	reGomegaSuccessFailure = regexp.MustCompile(`(?i)Expected success, but got an error:\s*\n(?:[ \t]*<[^>\n]*>[ \t]*:?[ \t]*\n)?[ \t]*([^\n.]+)`)
	// HCP API / reserved hostnames (e.g. api.<cluster>.<stamp>.<region>.aroapp-hcp.io).
	// These appear in x509 certificate-mismatch error text and are cluster/stamp-
	// specific; normalize so the same class of cert error merges across clusters.
	reCleanHCPApiHost = regexp.MustCompile(`(?i)\b(?:api\.[a-z0-9][a-z0-9.-]*|reserved(?:\.[a-z0-9][a-z0-9.-]*)?)\.aroapp-hcp(?:\.azure-test\.net|\.io)\b`)
	// OCP version strings like openshift-v4.22.0-candidate (the version number
	// is instance-specific; normalize so the same class of error merges).
	reCleanOCPVersion = regexp.MustCompile(`\bopenshift-v[0-9]+\.[0-9]+\.[0-9]+-[a-z]+\b`)
	// Single-quoted opaque alphanumeric IDs (≥20 chars) such as OCM cluster
	// internal IDs that appear in Azure RP error messages.
	reCleanQuotedOpaqueID = regexp.MustCompile(`'[a-z0-9]{20,}'`)
	// Single-quoted Azure provider/type/name path, e.g.
	// 'Microsoft.ContainerService/managedClusters/prow-j123456-mgmt-1'. Keep the
	// provider/type anchor but scrub the generated resource name.
	reCleanQuotedAzureResourcePath = regexp.MustCompile(`'Microsoft\.[A-Za-z]+(?:\.[A-Za-z]+)?(?:/[A-Za-z0-9._-]+){2,}'`)
	// logfmt err= field value extracted from a step-error log line.
	// The pattern matches level=error … msg="Step errored." … err="<value>" to
	// capture the actionable error message without logfmt boilerplate fields.
	reLogfmtStepErroredErr = regexp.MustCompile(`(?i)level=error[^"]*msg="step errored\."[^"]*err="([^"]+)"`)

	// Gomega type-annotation line: <*errors.errorString | 0x...>: or <wait.errInterrupted>:
	// Used by bestGomegaInnerError to locate the actual error message that
	// follows the type wrapper in HaveOccurred() assertion output.
	reGomegaTypeAnnotation = regexp.MustCompile(`^\s*<[^>]+>\s*:?\s*$`)

	// Kubernetes machine/node name: 12+ char alphanumeric prefix followed by
	// 3+ dash-separated segments and a 4-6 char hash suffix. These are
	// instance-specific and must be scrubbed to merge the same failure class.
	reCleanK8sNodeName = regexp.MustCompile(`\b[a-z0-9]{12,}(?:-[a-z0-9]+){3,}-[a-z0-9]{4,6}\b`)

	// OCP upgrade-graph channel references (e.g. stable-5.2, candidate-4.20).
	// The version is instance-specific; normalize so all version-parametrized
	// variants of the same channel-lookup failure merge.
	reCleanOCPChannel = regexp.MustCompile(`\b(?:stable|candidate|fast|eus)-[0-9]+\.[0-9]+\b`)

	// Kubernetes klog / structured-log prefix: E<MMDD> <HH:MM:SS>.<us> <goroutine>
	// The file:line portion is already stripped by reCleanGoFileLine; this
	// strips the remaining severity+timestamp+goroutine token so the real
	// message (e.g. "Unhandled Error" err=…) becomes the canonical phrase.
	reCleanK8sLogPrefix = regexp.MustCompile(`[EWI][0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+ +[0-9]+\s*\]?`)

	// Bare nodepool name in the form "nodepool <name>" (single token, no quotes)
	// that appears in UpdateNodePoolAndWait / timeout messages. The quoted form
	// nodepool="<name>" is handled by reCleanNodePoolQuoted above; this pattern
	// covers the unquoted counterpart so the same failure class merges.
	reCleanNodePoolBare = regexp.MustCompile(`(?i)\bnodepool [a-z0-9][a-z0-9-]+\b`)

	// "make[N]: Entering/Leaving directory '...'" lines emitted by GNU Make
	// when shell steps run sub-makes. These are build preamble noise that
	// precede the real error in multi-line err= log fields; strip them so the
	// canonical phrase reflects the actual failure rather than the banner.
	reCleanMakeDirectory = regexp.MustCompile(`(?i)make\[\d+\]: (?:Entering|Leaving) directory\s+'[^']*'\s*\.?\s*`)
)

var normalizePickPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Deserializa(?:ti|i)on Error:[^\n]+`),
	regexp.MustCompile(`(?i)Command Error:[^\n]+`),
	regexp.MustCompile(`(?i)route host was never found:[^\n]+`),
	regexp.MustCompile(`(?i)cluster operators not available:[^\n]+`),
	regexp.MustCompile(`(?i)client rate limiter wait returned an error: context deadline exceeded`),
	regexp.MustCompile(`(?i)missing expected log sources[^\n]+`),
	regexp.MustCompile(`(?i)failed to gather logs[^\n]+`),
	regexp.MustCompile(`(?i)failed to get service aro-hcp-exporter/aro-hcp-exporter: services "aro-hcp-exporter" not found`),
	regexp.MustCompile(`(?i)failed to search for managed resource groups:[^\n]+`),
	regexp.MustCompile(`(?i)failed to create SRE breakglass session:[^\n]+`),
	// ERROR CODE must come before the generic response-status line so that a
	// richer error code (e.g. NotFound with a detail message) is preferred
	// over the bare HTTP status text.
	regexp.MustCompile(`(?i)ERROR CODE:\s*[A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)response 404:[^\n]{0,240}`),
	regexp.MustCompile(`(?i)timeout '\d+\.\d+' minutes exceeded during CreateNodePoolFromParam[^\n]*`),
	regexp.MustCompile(`(?i)failed waiting for nodepool[^\n]+(?:updating|to finish creating)[^\n]*`),
	regexp.MustCompile(`(?i)UpdateNodePoolAndWait[^\n]+minutes exceeded[^\n]*`),
	regexp.MustCompile(`(?i)timeout '\d+\.\d+' minutes exceeded during CreateHCPClusterFromParam[^\n]*`),
	regexp.MustCompile(`(?i)error running Image Mirror Step, failed to execute shell command:[^\n]+`),
	regexp.MustCompile(`(?i)error running Helm release deployment Step, failed to deploy helm release:[^\n]+`),
	regexp.MustCompile(`(?i)error running Shell Step, failed to execute shell command:[^\n]+`),
	regexp.MustCompile(`(?i)failed to run ARM step:[^\n]+`),
	regexp.MustCompile(`(?i)Cluster provisioning failed`),
	regexp.MustCompile(`(?i)Interrupted by User`),
}

var safeSearchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Deserializa(?:ti|i)on Error:[^\n]+`),
	regexp.MustCompile(`(?i)Command Error:[^\n]+`),
	regexp.MustCompile(`(?i)route host was never found:[^\n]+`),
	regexp.MustCompile(`(?i)cluster operators not available:[^\n]+`),
	regexp.MustCompile(`(?i)client rate limiter wait returned an error: context deadline exceeded`),
	regexp.MustCompile(`(?i)ERROR CODE:\s*[A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)timeout '\d+\.\d+' minutes exceeded during [A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)failed waiting for nodepool[^\n]+(?:updating|to finish creating)[^\n]*`),
	regexp.MustCompile(`(?i)failed to get service aro-hcp-exporter/aro-hcp-exporter: services "aro-hcp-exporter" not found`),
	regexp.MustCompile(`(?i)failed to search for managed resource groups:[^\n]+`),
	regexp.MustCompile(`(?i)failed to create SRE breakglass session:[^\n]+`),
	regexp.MustCompile(`(?i)error running Image Mirror Step, failed to execute shell command:[^\n]+`),
	regexp.MustCompile(`(?i)error running Helm release deployment Step, failed to deploy helm release:[^\n]+`),
	regexp.MustCompile(`(?i)error running Shell Step, failed to execute shell command:[^\n]+`),
	regexp.MustCompile(`(?i)failed to run ARM step:[^\n]+`),
	regexp.MustCompile(`(?i)response 404:[^\n]+`),
	regexp.MustCompile(`(?i)missing expected log sources[^\n]+`),
	regexp.MustCompile(`(?i)failed to gather logs[^\n]+`),
	regexp.MustCompile(`(?i)context deadline exceeded`),
	regexp.MustCompile(`(?i)Interrupted by User`),
	regexp.MustCompile(`(?i)Cluster provisioning failed`),
}

type FailurePattern struct {
	CanonicalEvidencePhrase string
	SearchQueryPhrase       string
	ProviderAnchor          string
	GenericPhrase           bool
}

type ExtractOptions struct {
	TestName string
}

type azureCodeHit struct {
	Code  string
	Index int
}

func FailurePatternKey(pattern FailurePattern) string {
	base := strings.ToLower(collapseWS(pattern.CanonicalEvidencePhrase))
	base = reFailurePatternPlaceholder.ReplaceAllString(base, "")
	base = collapseWS(base)
	return base
}

func Extract(text string) FailurePattern {
	return ExtractWithOptions(text, ExtractOptions{})
}

func ExtractWithOptions(text string, opts ExtractOptions) FailurePattern {
	raw := text
	lowered := strings.ToLower(raw)
	provider := ProviderAnchor(raw)
	assertionContext := extractAssertionContext(raw)
	if assertionContext != "" {
		canonical := normalizeExtractedCanonical(cleanCanonical(prepareCanonicalText(assertionContext)))
		searchPhrase := ""
		if strings.Contains(raw, assertionContext) {
			searchPhrase = ChooseSearchPhrase(raw, []string{assertionContext, canonical})
		}
		return FailurePattern{
			CanonicalEvidencePhrase: contextualizeCanonicalWithTestName(canonical, opts.TestName),
			SearchQueryPhrase:       searchPhrase,
			ProviderAnchor:          provider,
			GenericPhrase:           false,
		}
	}

	logfmtErr := extractLogfmtStepError(raw)
	releaseStatusDescription := extractLogfmtReleaseStatusDescription(raw)
	picked := ""
	for _, pattern := range normalizePickPatterns {
		if match := pattern.FindString(raw); match != "" {
			picked = match
			break
		}
	}

	if logfmtErr != "" && (picked == "" || isStructuredStepWrapperPick(picked)) {
		picked = logfmtErr
	}
	if releaseStatusDescription != "" && (picked == "" || isReleaseStatusWrapperPick(picked)) {
		picked = releaseStatusDescription
	}
	if candidateGraphFailure := bestCandidateGraphFailure(raw); candidateGraphFailure != "" {
		picked = candidateGraphFailure
	}
	if imageMirrorFailure := bestImageMirrorInnerFailure(raw); imageMirrorFailure != "" {
		picked = imageMirrorFailure
	}

	if picked == "" {
		if match := reProwEntrypointMsg.FindStringSubmatch(raw); len(match) > 1 {
			picked = strings.TrimSpace(match[1])
		}
	}

	if picked == "" {
		if logfmtErr != "" {
			picked = logfmtErr
		}
	}

	if picked == "" {
		picked = bestSignalErrorLine(raw)
	}

	if picked == "" {
		picked = bestHTTPResponseStatusLine(raw)
	}

	if picked == "" {
		parts := reCauseBySplit.Split(raw, -1)
		if len(parts) > 1 {
			picked = truncatePickedText(parts[len(parts)-1], 260)
		} else {
			lines := splitNonEmptyLines(raw)
			errorLines := make([]string, 0, len(lines))
			for _, line := range lines {
				if rePickErrorSignal.MatchString(line) && !isAssertionTail(line) && !isWrapperNoiseLine(line) && !isStructFieldNoiseLine(line) && !isStatusBannerLine(line) {
					errorLines = append(errorLines, line)
				}
			}
			fallback := "failure occurred"
			if len(lines) > 0 {
				for i := len(lines) - 1; i >= 0; i-- {
					if !isAssertionTail(lines[i]) && !isStructFieldNoiseLine(lines[i]) && !isStatusBannerLine(lines[i]) {
						fallback = lines[i]
						break
					}
				}
			}
			if len(errorLines) > 0 {
				picked = truncatePickedText(errorLines[len(errorLines)-1], 260)
			} else {
				picked = truncatePickedText(fallback, 260)
			}
		}
	}
	if certMismatch := bestX509CertificateMismatchDetail(raw); certMismatch != "" && shouldPreferX509CertificateMismatchDetail(picked) {
		picked = certMismatch
	}

	if strings.EqualFold(strings.TrimSpace(picked), "cluster provisioning failed") {
		if codePick := regexp.MustCompile(`(?i)ERROR CODE:\s*[A-Za-z0-9_]+`).FindString(raw); codePick != "" {
			picked = codePick
		}
	}
	picked = refineDeserializationNoOutputPicked(raw, picked)
	picked = refineCommandErrorExitStatusOnly(raw, picked)

	code := ""
	leafCode := ""
	leafMessage := ""
	if match := reErrorCode.FindStringSubmatch(picked); len(match) > 1 {
		code = strings.TrimSpace(match[1])
	}
	if code == "" {
		code = RootAzureErrorCode(raw)
	}
	canonical := normalizeExtractedCanonical(cleanCanonical(prepareCanonicalText(picked)))

	if code != "" && (isGenericCode(code) || isAlwaysDetailedProvider(provider)) {
		leafCode, leafMessage = extractLeafAzureDetail(raw, code)
		parts := []string{"ERROR CODE: " + code}
		if leafCode != "" {
			parts = append(parts, "detail code "+leafCode)
		}
		if leafMessage != "" {
			parts = append(parts, "detail message "+leafMessage)
		}
		if leafCode == "" && leafMessage == "" {
			if context := extractAssertionHeaderContext(raw); context != "" {
				parts = append(parts, "context "+cleanCanonical(context))
			}
		}
		if provider != "" {
			parts = append(parts, "provider "+provider)
		}
		canonical = strings.Join(parts, "; ")
	}

	if strings.Contains(lowered, "context deadline exceeded") && reCreateHCPClusterTimeout.MatchString(lowered) {
		canonical = "timeout during CreateHCPClusterAndWait; context deadline exceeded"
	}
	if strings.Contains(lowered, "getadminrestconfigforhcpcluster") && strings.Contains(lowered, "timeout") {
		canonical = "timeout during GetAdminRESTConfigForHCPCluster while waiting for hcpcluster creds"
	}
	if strings.Contains(lowered, "interrupted by user") {
		canonical = "Interrupted by User"
	}
	if containsDeserializationErrorToken(picked) || containsDeserializationErrorToken(canonical) {
		match := reDeserializationLiteral.FindString(raw)
		if match == "" {
			canonical = "Deserializaion Error"
		} else {
			canonical = cleanCanonical(match)
		}
	}
	if strings.EqualFold(strings.TrimSpace(canonical), "context deadline exceeded") {
		if refined := bestContextDeadlineDetail(raw); refined != "" {
			canonical = cleanCanonical(refined)
		}
	}

	normalizedCanonical := strings.ToLower(canonical)
	if _, found := wrapperOnly[normalizedCanonical]; found || reUnexpectedOnly.MatchString(canonical) {
		parts := reCauseBySplit.Split(raw, -1)
		if len(parts) > 1 {
			canonical = cleanCanonical(truncatePickedText(parts[len(parts)-1], 260))
		}
	}
	if isLowInformationCanonical(canonical) {
		if refined := bestSignalErrorLine(raw); refined != "" {
			canonical = cleanCanonical(refined)
		} else if refined := bestGomegaInnerError(raw); refined != "" {
			canonical = cleanCanonical(refined)
		}
	}

	if code != "" && !isGenericCode(code) && !isAlwaysDetailedProvider(provider) && provider != "" &&
		strings.HasPrefix(strings.ToLower(canonical), "error code:") &&
		!strings.Contains(strings.ToLower(canonical), "; provider ") {
		canonical += "; provider " + provider
	}

	searchPhrase := ChooseSearchPhrase(raw, []string{picked, canonical})
	_, genericCanonical := map[string]struct{}{
		"interrupted by user":         {},
		"cluster provisioning failed": {},
		"context deadline exceeded":   {},
		"timeout during createhcpclusterandwait; context deadline exceeded": {},
	}[strings.ToLower(canonical)]
	genericPhrase := genericCanonical
	if code != "" && isGenericCode(code) {
		genericPhrase = leafCode == "" && leafMessage == "" && provider == ""
	}
	canonical = contextualizeCanonicalWithTestName(canonical, opts.TestName)

	return FailurePattern{
		CanonicalEvidencePhrase: canonical,
		SearchQueryPhrase:       searchPhrase,
		ProviderAnchor:          provider,
		GenericPhrase:           genericPhrase,
	}
}

func ProviderAnchor(text string) string {
	pathMatches := reProviderPath.FindAllStringSubmatch(text, -1)
	for i := len(pathMatches) - 1; i >= 0; i-- {
		if len(pathMatches[i]) < 2 {
			continue
		}
		candidate := strings.TrimSpace(pathMatches[i][1])
		if candidate == "" || isIgnoredProvider(candidate) {
			continue
		}
		return candidate
	}

	textMatches := reProviderText.FindAllStringSubmatch(text, -1)
	for i := len(textMatches) - 1; i >= 0; i-- {
		if len(textMatches[i]) < 2 {
			continue
		}
		candidate := strings.TrimSpace(textMatches[i][1])
		if candidate == "" || isIgnoredProvider(candidate) {
			continue
		}
		return candidate
	}
	return ""
}

func isIgnoredProvider(value string) bool {
	switch value {
	case "Microsoft.Resources", "Microsoft.Azure.ARO":
		return true
	default:
		return strings.HasPrefix(value, "Microsoft.Azure.ARO.HCP")
	}
}

func cleanCanonical(value string) string {
	return cleanCanonicalWithLimit(value, 260)
}

func cleanCanonicalWithLimit(value string, limit int) string {
	text := value
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, `\r`, " ")
	text = strings.ReplaceAll(text, `\n`, " ")
	text = strings.ReplaceAll(text, `\t`, " ")
	text = strings.ReplaceAll(text, `\"`, `"`)
	text = reCleanFmtWrap.ReplaceAllString(text, " ")
	text = reCleanHexAddress.ReplaceAllString(text, " ")
	text = reCleanGoFileLine.ReplaceAllString(text, " ")
	text = reCleanUnexpectedPrefix.ReplaceAllString(text, "")
	text = reCleanWrapperPrefix.ReplaceAllString(text, "")
	text = reCleanURL.ReplaceAllString(text, "<url>")
	text = reCleanRedactedSubscription.ReplaceAllString(text, "/subscriptions/<subscription>")
	text = reCleanSubscription.ReplaceAllString(text, "/subscriptions/<subscription>")
	text = reCleanResourceGroupsPathSegment.ReplaceAllString(text, "/resourcegroups/<resource-group>/")
	text = reCleanVaultNameAlreadyInUse.ReplaceAllString(text, "the vault name '<vault-name>' is already in use.")
	text = reCleanUUID.ReplaceAllString(text, "<uuid>")
	text = reCleanHexLong.ReplaceAllString(text, "<hex>")
	text = reCleanLookupHostOnServer.ReplaceAllString(text, "lookup <host> on <dns-server>")
	text = reCleanResourceGroupQuoted.ReplaceAllString(text, `resourcegroup="<resource-group>"`)
	text = reCleanResourceGroupBare.ReplaceAllString(text, "resource group <resource-group>")
	text = reCleanResourceGroupSingle.ReplaceAllString(text, "resource group '<resource-group>'")
	text = reCleanClusterQuoted.ReplaceAllString(text, `cluster="<cluster>"`)
	text = reCleanClusterPhraseQuoted.ReplaceAllString(text, `cluster "<cluster>"`)
	text = reCleanClusterCreationQuoted.ReplaceAllString(text, `cluster creation "<cluster>"`)
	text = reCleanForClusterPhrase.ReplaceAllString(text, "for cluster <cluster>")
	text = reCleanInClusterPhrase.ReplaceAllString(text, "in cluster <cluster>")
	text = reCleanOnClusterPhrase.ReplaceAllString(text, "on cluster <cluster>")
	text = reCleanHCPClusterPhrase.ReplaceAllString(text, "HCP cluster <cluster>")
	text = reCleanExternalAuthQuoted.ReplaceAllString(text, `external auth "<external-auth>"`)
	text = reCleanExternalAuthBare.ReplaceAllString(text, "external auth <external-auth>")
	text = reCleanVMQuoted.ReplaceAllString(text, `VM "<vm>"`)
	text = reCleanPodQuoted.ReplaceAllString(text, `pod "<pod>"`)
	text = reCleanNamespaceQuoted.ReplaceAllString(text, `namespace "<namespace>"`)
	text = reCleanServiceAccountPath.ReplaceAllString(text, `service account <namespace>/$1`)
	text = reCleanNodePoolQuoted.ReplaceAllString(text, `nodepool="<nodepool>"`)
	text = reCleanNodePoolAssignment.ReplaceAllString(text, "nodePool=<nodepool>")
	text = reCleanNodePoolPhraseQuoted.ReplaceAllString(text, `node pool "<nodepool>"`)
	text = reCleanNodePoolPhrase.ReplaceAllString(text, "node pool <nodepool>")
	text = reCleanNodePoolBare.ReplaceAllString(text, "nodepool <nodepool>")
	text = reCleanAgentPool.ReplaceAllString(text, "agent pool <nodepool>")
	text = reCleanK8sNodeName.ReplaceAllString(text, "<node>")
	text = reCleanOCPChannel.ReplaceAllString(text, "<ocp-channel>")
	text = reCleanDialTCPAddress.ReplaceAllString(text, "dial tcp <ip>:<port>")
	text = reCleanDialingAddress.ReplaceAllString(text, "dialing <ip>:<port>")
	text = reCleanRouteHost.ReplaceAllString(text, "<route-host>")
	text = reCleanBreakglassPath.ReplaceAllString(text, "/hcpopenshiftclusters/<cluster>/breakglass/<session>/kubeconfig")
	text = reCleanRedactedQuoted.ReplaceAllString(text, `"<redacted-id>"`)
	text = reCleanBracketedIPv6.ReplaceAllString(text, "<ip>")
	text = strings.ReplaceAll(text, "::1", "<ip>")
	text = reCleanIP.ReplaceAllString(text, "<ip>")
	text = reCleanLogfmtTimestamp.ReplaceAllString(text, "")
	text = reCleanJSONTimeField.ReplaceAllString(text, "")
	text = reCleanISOTimestamp.ReplaceAllString(text, "<timestamp>")
	text = reCleanHCPApiHost.ReplaceAllString(text, "<hcp-api-host>")
	text = reCleanOCPVersion.ReplaceAllString(text, "openshift-v<version>")
	text = normalizeQuotedAzureResourcePath(text)
	text = reCleanQuotedOpaqueID.ReplaceAllString(text, "'<id>'")
	text = reCleanK8sLogPrefix.ReplaceAllString(text, "")
	text = reCleanMakeDirectory.ReplaceAllString(text, "")
	text = reTimeoutMinutesExceeded.ReplaceAllString(text, "timeout '<minutes>' minutes exceeded")
	text = reTemplateLineColumn.ReplaceAllString(text, "at line '<line>' and column '<column>'")
	text = reCleanResolveWithin.ReplaceAllString(text, "did not resolve within <duration>")
	text = reCleanSelector.ReplaceAllString(text, `selector "<selector>"`)
	text = reExpectedReadyNodes.ReplaceAllString(text, "expected <count> ready (and schedulable) nodes, found <count>")
	text = reCleanFailEmpty.ReplaceAllString(text, "")
	text = reCleanKlusterletDeploymentQuoted.ReplaceAllString(text, `${1}klusterlet-<cluster>-${2}${3}`)
	text = collapseWS(text)
	text = normalizeAlertDescription(text)
	text = trimUnmatchedTerminalQuote(text)
	if strings.HasPrefix(strings.ToLower(text), "description:") {
		return text
	}
	if limit > 0 && len(text) > limit {
		text = truncateCanonical(text, limit)
	}
	return text
}

func normalizeAlertDescription(value string) string {
	text := value
	text = reAlertResourceRef.ReplaceAllStringFunc(text, func(match string) string {
		parts := reAlertResourceRef.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		kind := parts[1]
		name := parts[3]
		switch strings.ToLower(kind) {
		case "pod":
			name = normalizePodName(name)
		case "deployment":
			name = normalizeDeploymentName(name)
		}
		return kind + " <namespace>/" + name
	})
	text = reAlertContainerPod.ReplaceAllStringFunc(text, func(match string) string {
		parts := reAlertContainerPod.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "in pod <namespace>/" + normalizePodName(parts[2])
	})
	text = reAlertPodInNamespace.ReplaceAllStringFunc(text, func(match string) string {
		parts := reAlertPodInNamespace.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "pod/" + normalizePodName(parts[1]) + " in namespace <namespace>"
	})
	text = reAlertNodeDescription.ReplaceAllString(text, `${1}node <node>${2}`)
	text = reAlertHostedCluster.ReplaceAllString(text, "on hosted cluster <cluster> (management cluster <management-cluster>)")
	text = reAlertNamespace.ReplaceAllString(text, "in namespace <namespace>")
	text = reAlertPodCount.ReplaceAllString(text, `${1}<count> pods`)
	text = reAlertKustoCluster.ReplaceAllString(text, `${1}<kusto-cluster>`)
	text = reGetPodsName.ReplaceAllString(text, "get pod <pod>")
	text = reNotReadyNodes.ReplaceAllString(text, `${1}[<node>]`)
	return text
}

func normalizePodName(value string) string {
	if match := rePodReplicaSuffix.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + "-<pod>"
	}
	if match := rePodShortSuffix.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + "-<pod>"
	}
	if match := rePodOrdinalSuffix.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + "-<pod>"
	}
	if len(value) >= 20 {
		return "<pod>"
	}
	return value
}

func normalizeDeploymentName(value string) string {
	if match := reKlusterletDeployment.FindStringSubmatch(value); len(match) > 1 {
		return "klusterlet-<cluster>-" + match[1]
	}
	return value
}

func trimUnmatchedTerminalQuote(value string) string {
	text := strings.TrimSpace(value)
	if strings.Count(text, `"`)%2 == 0 {
		return text
	}
	if strings.HasSuffix(text, `"`) {
		return strings.TrimSpace(strings.TrimSuffix(text, `"`))
	}
	if strings.HasPrefix(text, `"`) {
		return strings.TrimSpace(strings.TrimPrefix(text, `"`))
	}
	return text
}

func truncatePickedText(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(trimmed), "description:") {
		return trimmed
	}
	return truncateText(trimmed, max)
}

func normalizeExtractedCanonical(value string) string {
	normalized := normalizeTimedOutAfterDuration(value)
	normalized = stripTransportURLPrefixForTLSMismatch(normalized)
	normalized = stripReleaseFailureWrapper(normalized)
	normalized = stripUnhandledErrorWrapper(normalized)
	normalized = normalizeCleanupWorkflowDeletion(normalized)
	normalized = stripDefaultVMSizeWrapper(normalized)
	return normalized
}

func prepareCanonicalText(value string) string {
	normalized := collapseWS(value)
	normalized = normalizeOperationTimeout(normalized)
	normalized = stripReleaseFailureWrapper(normalized)
	normalized = normalizeServerPatchTimeouts(normalized)
	normalized = stripDefaultVMSizeWrapper(normalized)
	normalized = normalizeBreakglassKubeconfigFailure(normalized)
	return normalized
}

func normalizeTimedOutAfterDuration(value string) string {
	trimmed := strings.TrimSpace(value)
	if !reTimedOutAfterDuration.MatchString(trimmed) {
		return trimmed
	}
	return "Timed out after <duration>s."
}

func normalizeOperationTimeout(value string) string {
	trimmed := strings.TrimSpace(value)
	match := reOperationTimeout.FindStringSubmatch(trimmed)
	if len(match) < 2 {
		return trimmed
	}
	result := match[1] + " timed out after <minutes> minutes"
	if strings.Contains(strings.ToLower(trimmed), "context deadline exceeded") {
		result += "; context deadline exceeded"
	}
	return result
}

func contextualizeCanonicalWithTestName(canonical string, testName string) string {
	current := strings.TrimSpace(canonical)
	name := strings.TrimSpace(testName)
	if current == "" || name == "" {
		return current
	}
	if !isGenericAssertionCanonical(current) && !isGenericTimedOutCanonical(current) {
		return current
	}
	return truncateText(collapseWS(name+": "+current), 220)
}

func isGenericAssertionCanonical(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "assertion failed: expected values to equal")
}

func isGenericTimedOutCanonical(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "Timed out after <duration>s.")
}

func stripTransportURLPrefixForTLSMismatch(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	const marker = `tls: failed to verify certificate:`
	if strings.HasPrefix(lowered, `get "<url>`) {
		if idx := strings.Index(lowered, marker); idx >= 0 {
			return strings.TrimSpace(trimmed[idx:])
		}
	}
	return trimmed
}

func stripReleaseFailureWrapper(value string) string {
	trimmed := reStampPrefix.ReplaceAllString(strings.TrimSpace(value), "")
	lowered := strings.ToLower(trimmed)
	const helmPrefix = "error running helm release deployment step, failed to deploy helm release:"
	if strings.HasPrefix(lowered, helmPrefix) {
		return strings.TrimSpace(trimmed[len(helmPrefix):])
	}
	if strings.HasPrefix(lowered, `release "`) {
		if idx := strings.Index(lowered, `" failed: `); idx >= 0 {
			return strings.TrimSpace(trimmed[idx+len(`" failed: `):])
		}
	}
	return trimmed
}

func normalizeCleanupWorkflowDeletion(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	if !strings.Contains(lowered, "ordered cleanup workflow failed for") || !strings.Contains(lowered, "failed deleting") {
		return trimmed
	}

	normalized := reCleanupWorkflowTarget.ReplaceAllString(trimmed, `${1}<cleanup-target>${3}`)
	normalized = reCleanupWorkflowResourceName.ReplaceAllString(normalized, `${1}<cleanup-resource>${3}`)
	normalized = reCleanupWorkflowMethodURL.ReplaceAllString(normalized, ": <url>")
	return collapseWS(normalized)
}

func stripDefaultVMSizeWrapper(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, "failed to resolve default vm size for node pool ") {
		return trimmed
	}
	if idx := strings.Index(lowered, `selector "`); idx >= 0 {
		return strings.TrimSpace(trimmed[idx:])
	}
	return trimmed
}

func normalizeBreakglassKubeconfigFailure(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	if !strings.Contains(lowered, "failed to get ready session kubeconfig from ") ||
		!strings.Contains(lowered, "timeout waiting for session to become ready") {
		return trimmed
	}
	summary := "breakglass session kubeconfig not ready: timeout waiting for session to become ready"
	if strings.Contains(lowered, "hostedcontrolplane exists but is not ready") {
		summary += "; HostedControlPlane exists but is not ready"
	}
	return summary
}

func normalizeServerPatchTimeouts(value string) string {
	matches := reServerPatchTimeout.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return value
	}
	configMaps := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			configMaps = append(configMaps, strings.TrimSpace(match[1]))
		}
	}
	sort.Strings(configMaps)
	configMaps = compactStrings(configMaps)
	return "server timed out while patching configmaps: " + strings.Join(configMaps, ", ")
}

func stripUnhandledErrorWrapper(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	if !strings.Contains(lowered, "unhandled error") {
		return trimmed
	}
	if idx := strings.Index(lowered, `err="`); idx >= 0 {
		return trimUnmatchedTerminalQuote(strings.TrimSpace(trimmed[idx+len(`err="`):]))
	}
	return trimmed
}

func normalizeQuotedAzureResourcePath(value string) string {
	return reCleanQuotedAzureResourcePath.ReplaceAllStringFunc(value, func(match string) string {
		inner := strings.Trim(match, `'`)
		parts := strings.Split(inner, "/")
		if len(parts) < 3 {
			return match
		}
		parts[len(parts)-1] = "<resource>"
		return "'" + strings.Join(parts, "/") + "'"
	})
}

func truncateCanonical(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	if max <= 0 || len(trimmed) <= max {
		return strings.TrimRight(trimmed, " ,;:-")
	}
	cut := strings.TrimSpace(trimmed[:max])
	if lastSpace := strings.LastIndex(cut, " "); lastSpace >= max/2 {
		cut = cut[:lastSpace]
	}
	return strings.TrimRight(cut, " ,;:-")
}

func extractAssertionContext(text string) string {
	if boolContext := extractBoolAssertionContext(text); boolContext != "" {
		return boolContext
	}
	if eventuallyContext := extractEventuallyFailureContext(text); eventuallyContext != "" {
		return eventuallyContext
	}
	if successContext := extractGomegaSuccessFailureContext(text); successContext != "" {
		return successContext
	}
	if modelDiffSummary := extractModelDiffSummaryContext(text); modelDiffSummary != "" {
		return modelDiffSummary
	}
	if placeholderEquality := extractPlaceholderOnlyEqualityAssertionContext(text); placeholderEquality != "" {
		return placeholderEquality
	}
	if reExpectedErrorButNil.MatchString(text) {
		for _, line := range strings.Split(text, "\n") {
			if detail := assertionHeaderDetail(collapseWS(line)); detail != "" {
				return detail
			}
		}
	}

	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if !isAssertionTail(line) {
			continue
		}

		tail := strings.ToLower(collapseWS(line))
		if strings.HasPrefix(tail, "to match error") {
			stop := minInt(len(lines), index+12)
			for _, candidateLine := range lines[index+1 : stop] {
				match := reAssertionRegexHint.FindStringSubmatch(candidateLine)
				if len(match) > 1 {
					regexHint := collapseWS(match[1])
					if regexHint != "" {
						return regexHint
					}
				}
			}
		}

		best := ""
		start := maxInt(0, index-30)
		for i := index - 1; i >= start; i-- {
			candidate := collapseWS(lines[i])
			if isNoiseAssertionContextLine(candidate) {
				continue
			}
			candidate = unwrapTimedOutAssertionWrapper(candidate)
			if reAssertionErrorSignal.MatchString(candidate) {
				return candidate
			}
			if best == "" && regexp.MustCompile(`[A-Za-z]`).MatchString(candidate) {
				best = candidate
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

func unwrapTimedOutAssertionWrapper(value string) string {
	trimmed := strings.TrimSpace(value)
	match := reTimedOutAssertionWrapper.FindStringSubmatch(trimmed)
	if len(match) <= 1 {
		return trimmed
	}
	return strings.TrimSpace(match[1])
}

func extractModelDiffSummaryContext(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		candidate := collapseWS(line)
		if candidate == "" {
			continue
		}
		if reModelDiffSummary.MatchString(candidate) {
			return candidate
		}
	}
	return ""
}

func extractPlaceholderOnlyEqualityAssertionContext(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(strings.ToLower(collapseWS(line)), "to equal") {
			continue
		}
		sawPlaceholderValue := false
		sawMeaningfulContext := false
		start := maxInt(0, index-4)
		stop := minInt(len(lines), index+5)
		for i := start; i < stop; i++ {
			if i == index {
				continue
			}
			candidate := strings.TrimSpace(lines[i])
			if candidate == "" {
				continue
			}
			normalized := strings.ToLower(collapseWS(candidate))
			if normalized == "expected" || isAssertionTail(candidate) || isWrapperNoiseLine(candidate) {
				continue
			}
			if isPlaceholderAssertionValueLine(candidate) {
				sawPlaceholderValue = true
				continue
			}
			if isStructBoundaryLine(candidate) || isStructFieldNoiseLine(candidate) {
				continue
			}
			sawMeaningfulContext = true
			break
		}
		if sawPlaceholderValue && !sawMeaningfulContext {
			return "assertion failed: expected values to equal"
		}
	}
	return ""
}

func extractBoolAssertionContext(text string) string {
	match := reBoolAssertionContext.FindStringSubmatch(text)
	if len(match) == 0 {
		return ""
	}
	for i, name := range reBoolAssertionContext.SubexpNames() {
		if name == "context" && i < len(match) {
			return collapseWS(match[i])
		}
	}
	return ""
}

func extractEventuallyFailureContext(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if !isExpectedBlockStartLine(line) {
			continue
		}
		start := maxInt(0, index-12)
		for i := index - 1; i >= start; i-- {
			candidate := collapseWS(lines[i])
			if candidate == "" {
				continue
			}
			if isNoiseAssertionContextLine(candidate) || isEventuallyWrapperLine(candidate) || isTimedOutAfterLine(candidate) {
				continue
			}
			return unwrapTimedOutAssertionWrapper(candidate)
		}
	}
	return ""
}

func extractGomegaSuccessFailureContext(text string) string {
	matchIdx := reGomegaSuccessFailure.FindStringSubmatchIndex(text)
	if len(matchIdx) < 4 {
		return ""
	}
	candidate := strings.TrimSpace(text[matchIdx[2]:matchIdx[3]])
	if candidate == "" || candidate == "..." {
		return ""
	}
	if strings.HasSuffix(candidate, ":") {
		afterMatch := text[matchIdx[1]:]
		for _, line := range strings.Split(afterMatch, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || trimmed == "..." || strings.HasSuffix(trimmed, ":") {
				continue
			}
			return trimmed
		}
	}
	return candidate
}

func bestGomegaInnerError(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !reGomegaTypeAnnotation.MatchString(trimmed) {
			continue
		}
		for j := i + 1; j < len(lines) && j < i+5; j++ {
			candidate := strings.TrimSpace(lines[j])
			if candidate == "" || candidate == "..." {
				continue
			}
			if isStructBoundaryLine(candidate) {
				break
			}
			if isStructFieldNoiseLine(candidate) || isWrapperNoiseLine(candidate) {
				continue
			}
			if strings.ContainsAny(candidate, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				return candidate
			}
		}
	}
	return ""
}

func isExpectedBlockStartLine(line string) bool {
	return strings.EqualFold(collapseWS(line), "expected")
}

func isEventuallyWrapperLine(line string) bool {
	return reEventuallyWrapperLine.MatchString(collapseWS(line))
}

func isTimedOutAfterLine(line string) bool {
	return reTimedOutAfterLine.MatchString(collapseWS(line))
}

func isAssertionTail(line string) bool {
	normalized := strings.ToLower(collapseWS(line))
	for _, prefix := range assertionTailPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func AssertionTail(line string) bool {
	return isAssertionTail(line)
}

func isNoiseAssertionContextLine(line string) bool {
	normalized := collapseWS(line)
	lowered := strings.ToLower(normalized)
	if normalized == "" {
		return true
	}
	if isAssertionTail(normalized) {
		return true
	}
	if isEventuallyWrapperLine(normalized) || isTimedOutAfterLine(normalized) {
		return true
	}
	switch lowered {
	case "expected", "{", "}", "},", "]", "],", "(", ")":
		return true
	}
	if strings.HasPrefix(lowered, "expected") ||
		strings.HasPrefix(lowered, "learn more here:") ||
		strings.HasPrefix(lowered, "gomega truncated") ||
		strings.HasPrefix(lowered, "consider having") {
		return true
	}
	if strings.HasPrefix(lowered, "fail [") && strings.HasSuffix(lowered, ": expected") {
		return true
	}
	if isStructFieldNoiseLine(normalized) {
		return true
	}
	return strings.HasPrefix(normalized, "<") || strings.HasPrefix(normalized, "{") || strings.HasPrefix(normalized, "}")
}

func ChooseSearchPhrase(text string, candidates []string) string {
	for _, candidate := range candidates {
		token := strings.TrimSpace(candidate)
		if token == "" {
			continue
		}
		if containsPlaceholderToken(token) {
			continue
		}
		if strings.Contains(text, token) {
			return token
		}
	}
	return safeSearchFromText(text)
}

func extractLogfmtStepError(raw string) string {
	const errField = ` err="`
	start := strings.LastIndex(raw, errField)
	if start < 0 {
		match := reLogfmtStepErroredErr.FindStringSubmatch(raw)
		if len(match) <= 1 {
			return ""
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			return ""
		}
		if refined := bestInnerStepErrorLine(unescapeLogfmtNewlines(value)); refined != "" {
			return refined
		}
		return value
	}
	start += len(errField)
	end := findUnescapedQuote(raw, start)
	value := strings.TrimSpace(raw[start:end])
	if value == "" {
		return ""
	}
	if refined := bestInnerStepErrorLine(unescapeLogfmtNewlines(value)); refined != "" {
		return refined
	}
	return value
}

func findUnescapedQuote(value string, start int) int {
	escaped := false
	for index := start; index < len(value); index++ {
		switch value[index] {
		case '\\':
			escaped = !escaped
		case '"':
			if !escaped {
				return index
			}
			escaped = false
		default:
			escaped = false
		}
	}
	return len(value)
}

func extractLogfmtReleaseStatusDescription(raw string) string {
	match := reLogfmtReleaseStatusDesc.FindStringSubmatch(raw)
	if len(match) <= 1 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func isReleaseStatusWrapperPick(value string) bool {
	return strings.Contains(strings.ToLower(collapseWS(value)), `msg="determined release status."`)
}

func safeSearchFromText(text string) string {
	assertionContext := extractAssertionContext(text)
	if assertionContext != "" && strings.Contains(text, assertionContext) {
		return assertionContext
	}

	for _, pattern := range safeSearchPatterns {
		if match := pattern.FindString(text); match != "" {
			token := strings.TrimSpace(match)
			if token == "" || containsPlaceholderToken(token) {
				continue
			}
			if strings.Contains(text, token) {
				return token
			}
		}
	}

	for _, line := range strings.Split(text, "\n") {
		token := strings.TrimSpace(line)
		if token == "" || isAssertionTail(token) || containsPlaceholderToken(token) || isWrapperNoiseLine(token) || isStructFieldNoiseLine(token) || isStatusBannerLine(token) {
			continue
		}
		if reSafeErrorLineSignal.MatchString(token) {
			return truncateText(token, 220)
		}
	}

	if strings.Contains(strings.ToLower(text), "context deadline exceeded") {
		return "context deadline exceeded"
	}
	return "failure"
}

func extractLeafAzureDetail(text string, rootCode string) (string, string) {
	decoded := decodeEscapedErrorPayload(text)
	hits := collectAzureCodeHits(decoded)
	if len(hits) == 0 {
		return "", ""
	}

	root := strings.ToLower(strings.TrimSpace(rootCode))
	fallbackCode := ""
	genericFallbackCode := ""
	for i := len(hits) - 1; i >= 0; i-- {
		code := strings.TrimSpace(hits[i].Code)
		lowered := strings.ToLower(code)
		if lowered == "" || lowered == root {
			continue
		}
		if lowered == "resourcedeploymentfailure" || lowered == "deploymentfailed" {
			continue
		}
		if isLikelyTruncatedAzureCode(code, hits) {
			continue
		}

		message := summarizeAzureDetailMessage(extractAzureMessageForCode(decoded, code))
		message = appendAzureIdentityErrorDescription(message, decoded)
		if _, generic := genericCodes[lowered]; generic {
			if message != "" {
				return code, message
			}
			if genericFallbackCode == "" {
				genericFallbackCode = code
			}
			continue
		}
		if message != "" {
			return code, message
		}
		if fallbackCode == "" {
			fallbackCode = code
		}
	}
	if fallbackCode != "" {
		return fallbackCode, ""
	}
	if genericFallbackCode != "" {
		return genericFallbackCode, ""
	}
	rootMessage := summarizeAzureDetailMessage(extractAzureMessageForCode(decoded, rootCode))
	rootMessage = appendAzureIdentityErrorDescription(rootMessage, decoded)
	if rootMessage != "" {
		return "", rootMessage
	}
	return "", ""
}

func RootAzureErrorCode(text string) string {
	if match := reErrorCode.FindStringSubmatch(text); len(match) > 1 {
		code := strings.TrimSpace(match[1])
		if code != "" {
			return code
		}
	}
	hits := collectAzureCodeHits(decodeEscapedErrorPayload(text))
	for _, hit := range hits {
		code := strings.TrimSpace(hit.Code)
		if code == "" {
			continue
		}
		return code
	}
	return ""
}

func collectAzureCodeHits(text string) []azureCodeHit {
	out := make([]azureCodeHit, 0)
	errorCodeMatches := reErrorCode.FindAllStringSubmatchIndex(text, -1)
	for _, match := range errorCodeMatches {
		if len(match) < 4 {
			continue
		}
		code := strings.TrimSpace(text[match[2]:match[3]])
		if code == "" {
			continue
		}
		out = append(out, azureCodeHit{
			Code:  code,
			Index: match[0],
		})
	}

	codeFieldMatches := reCodeField.FindAllStringSubmatchIndex(text, -1)
	for _, match := range codeFieldMatches {
		if len(match) < 4 {
			continue
		}
		code := strings.TrimSpace(text[match[2]:match[3]])
		if code == "" {
			continue
		}
		out = append(out, azureCodeHit{
			Code:  code,
			Index: match[0],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func decodeEscapedErrorPayload(text string) string {
	out := text
	for range 2 {
		out = strings.ReplaceAll(out, `\\r\\n`, "\n")
		out = strings.ReplaceAll(out, `\\n`, "\n")
		out = strings.ReplaceAll(out, `\\t`, " ")
		out = strings.ReplaceAll(out, `\\\"`, `"`)
		out = strings.ReplaceAll(out, `\r\n`, "\n")
		out = strings.ReplaceAll(out, `\n`, "\n")
		out = strings.ReplaceAll(out, `\t`, " ")
		out = strings.ReplaceAll(out, `\"`, `"`)
		// Go's json.Marshal HTML-escapes '<', '>' and '&' as \u003c, \u003e
		// and \u0026 by default; several ClusterService error payloads carry
		// this literal escaping (e.g. "\u003cno_message\u003e"). Decode it so
		// downstream detail extraction sees the real placeholder text.
		out = strings.ReplaceAll(out, `\u003c`, "<")
		out = strings.ReplaceAll(out, `\u003e`, ">")
		out = strings.ReplaceAll(out, `\u0026`, "&")
	}
	return out
}

func isLikelyTruncatedAzureCode(code string, hits []azureCodeHit) bool {
	candidate := strings.ToLower(strings.TrimSpace(code))
	if candidate == "" {
		return true
	}
	for _, hit := range hits {
		other := strings.ToLower(strings.TrimSpace(hit.Code))
		if other == "" || len(other) <= len(candidate) {
			continue
		}
		if strings.HasPrefix(other, candidate) {
			return true
		}
	}
	return false
}

func extractAzureMessageForCode(text string, code string) string {
	targetCode := strings.TrimSpace(code)
	if targetCode == "" {
		return ""
	}
	// The message capture is intentionally lazy and stops only at a quote
	// immediately followed by a JSON delimiter (,}]). A plain `[^"]+` stops
	// at the FIRST bare quote, which truncates messages that legitimately
	// contain an escaped quote (e.g. `Invalid value: \"tag\": unrecognized
	// experimental tag`) once decodeEscapedErrorPayload has unescaped it to
	// a literal `"` for nested-JSON-in-string parsing elsewhere.
	pattern := `(?is)(?:ERROR CODE:\s*` + regexp.QuoteMeta(targetCode) + `|"code"\s*:\s*"` + regexp.QuoteMeta(targetCode) + `").{0,900}"message"\s*:\s*"(.+?)"\s*[,}\]]`
	reCodeMessage := regexp.MustCompile(pattern)
	matches := reCodeMessage.FindAllStringSubmatch(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) < 2 {
			continue
		}
		message := strings.TrimSpace(matches[i][1])
		if message == "" {
			continue
		}
		return message
	}
	return ""
}

func summarizeAzureDetailMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(trimmed), "clientcertificatecredential authentication failed") {
		if idx := strings.Index(strings.ToLower(trimmed), "clientcertificatecredential authentication failed"); idx >= 0 {
			prefix := trimmed[:idx+len("ClientCertificateCredential authentication failed")]
			return cleanCanonicalWithLimit(strings.TrimRight(prefix, " .")+".", 0)
		}
	}

	loweredRaw := strings.ToLower(trimmed)
	if strings.Contains(loweredRaw, `"code":`) ||
		strings.Contains(loweredRaw, `\"code\"`) ||
		strings.Contains(loweredRaw, `"status":`) ||
		strings.Contains(loweredRaw, `\"status\"`) {
		return ""
	}
	if strings.Count(trimmed, "{")+strings.Count(trimmed, "}") >= 2 {
		return ""
	}
	if match := reInvalidTemplateParameter.FindStringSubmatch(trimmed); len(match) > 1 {
		return `Deployment template validation failed: template parameter "` + strings.TrimSpace(match[1]) + `" is not provided.`
	}
	if strings.Contains(strings.ToLower(trimmed), "access is denied because of the deny assignment") {
		if match := reDenyAssignmentAction.FindStringSubmatch(trimmed); len(match) > 1 {
			return `Access denied by deny assignment for action "` + strings.TrimSpace(match[1]) + `".`
		}
		return "Access denied by deny assignment."
	}
	if strings.Contains(strings.ToLower(trimmed), "error while processing request for association ") {
		summary := "Error while processing network security perimeter association."
		if match := reNetworkAssociationError.FindStringSubmatch(trimmed); len(match) > 1 {
			summary = "Error while processing network security perimeter association: " + strings.TrimSpace(match[1])
		}
		return cleanCanonicalWithLimit(summary, 0)
	}
	if deletionSummary := summarizeClusterServiceDeletionMessage(trimmed); deletionSummary != "" {
		return deletionSummary
	}
	if hostedClusterSummary := summarizeHostedClusterMessage(trimmed); hostedClusterSummary != "" {
		return hostedClusterSummary
	}

	normalized := cleanCanonicalWithLimit(trimmed, 0)
	normalized = reQuotaRequiredAvailable.ReplaceAllString(normalized, "required <count>, available <count>")
	if idx := strings.Index(strings.ToLower(normalized), "allocation failed."); idx >= 0 {
		normalized = strings.TrimSpace(normalized[idx:])
	}
	lowered := strings.ToLower(normalized)
	for _, generic := range []string{
		"at least one resource deployment operation failed",
		"the resource write operation failed to complete successfully",
		"operation failed due to an internal server error",
		"internal server error",
	} {
		if strings.Contains(lowered, generic) {
			return ""
		}
	}

	if idx := strings.Index(lowered, " for more details,"); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
		lowered = strings.ToLower(normalized)
	}
	if idx := strings.Index(lowered, " refer to "); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	for _, marker := range []string{" please see ", " please wait for it to finish", " you may also use "} {
		if idx := strings.Index(strings.ToLower(normalized), marker); idx >= 0 {
			normalized = strings.TrimSpace(normalized[:idx])
		}
	}

	// "<no_message>" is ClusterService's own placeholder for "no failure
	// detail was provided". It's short (often just "[tag] <no_message>"),
	// but it is meaningful: dropping it here would make it indistinguishable
	// from an extractor bug that silently ate real detail. Always surface it
	// so it's evident the upstream service, not this extractor, is at fault.
	if strings.Contains(strings.ToLower(normalized), "<no_message>") {
		return normalized
	}

	if len(strings.Fields(normalized)) < 3 && !strings.EqualFold(strings.TrimSpace(normalized), "Allocation failed.") {
		return ""
	}
	return normalized
}

func summarizeHostedClusterMessage(message string) string {
	if !strings.Contains(strings.ToLower(message), "[hypershifthostedcluster]") {
		return ""
	}
	normalized := cleanCanonicalWithLimit(message, 0)
	hasUnavailableReplicas := strings.Contains(strings.ToLower(normalized), "unavailablereplicas:")
	parts := strings.Split(normalized, ";")
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if hasUnavailableReplicas && strings.Contains(strings.ToLower(part), "componentsnotavailable: waiting for components to be available:") {
			if marker := strings.Index(strings.ToLower(part), "componentsnotavailable:"); marker >= 0 {
				part = strings.TrimSpace(part[:marker+len("ComponentsNotAvailable")])
			}
		} else {
			part = normalizeCommaSeparatedDetail(part, "waiting for components to be available:")
		}
		part = normalizeUnavailableReplicas(part)
		parts[index] = part
	}
	return strings.Join(parts, "; ")
}

func normalizeCommaSeparatedDetail(value string, marker string) string {
	lowered := strings.ToLower(value)
	index := strings.Index(lowered, marker)
	if index < 0 {
		return value
	}
	start := index + len(marker)
	items := sortedUniqueCommaList(value[start:])
	if len(items) == 0 {
		return value
	}
	return strings.TrimSpace(value[:start]) + " " + strings.Join(items, ", ")
}

func normalizeUnavailableReplicas(value string) string {
	const marker = "unavailablereplicas:"
	lowered := strings.ToLower(value)
	index := strings.Index(lowered, marker)
	if index < 0 {
		return value
	}
	start := index + len(marker)
	payload := strings.Trim(strings.TrimSpace(value[start:]), "[]")
	items := sortedUniqueCommaList(payload)
	if len(items) == 0 {
		return value
	}
	for index, item := range items {
		if match := reUnavailableDeployment.FindStringSubmatch(item); len(match) > 1 {
			items[index] = strings.TrimSpace(match[1])
		}
	}
	sort.Strings(items)
	items = compactStrings(items)
	if len(items) > 8 {
		return strings.TrimSpace(value[:start]) + " multiple components"
	}
	return strings.TrimSpace(value[:start]) + " [" + strings.Join(items, ", ") + "]"
}

func sortedUniqueCommaList(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	sort.Strings(items)
	return compactStrings(items)
}

func summarizeClusterServiceDeletionMessage(message string) string {
	normalized := collapseWS(message)
	lowered := strings.ToLower(normalized)
	if !strings.Contains(lowered, "cluster deletion did not complete before the deadline") ||
		!strings.Contains(lowered, "[clusterservicedeletion] clusterservice cluster ") {
		return ""
	}

	summary := []string{"cluster deletion did not complete before the deadline"}
	descendants := make([]string, 0)
	for _, rawPart := range strings.Split(normalized, ";") {
		part := strings.TrimSpace(rawPart)
		partLower := strings.ToLower(part)
		switch {
		case strings.Contains(partLower, "[clusterservicedeletion]") &&
			strings.Contains(partLower, "clusterservice cluster") &&
			strings.Contains(partLower, "still exists"):
			summary = append(summary, "[clusterServiceDeletion] ClusterService cluster still exists")
		case strings.Contains(partLower, "[clusterservicestatus]"):
			if match := reClusterServiceState.FindStringSubmatch(part); len(match) > 1 {
				summary = append(summary, `[clusterServiceStatus] state is "`+strings.ToLower(strings.TrimSpace(match[1]))+`"`)
			}
		case strings.Contains(partLower, "[hostedcluster]") && strings.Contains(partLower, "still exists"):
			summary = append(summary, "[hostedCluster] HostedCluster still exists")
		case strings.Contains(partLower, "[descendantresources]"):
			if idx := strings.Index(partLower, "remaining resources:"); idx >= 0 {
				for _, resource := range strings.Split(part[idx+len("remaining resources:"):], ",") {
					resource = reLeadingResourceCount.ReplaceAllString(strings.TrimSpace(resource), "")
					if slash := strings.LastIndex(resource, "/"); slash >= 0 {
						resource = resource[slash+1:]
					}
					if resource != "" {
						descendants = append(descendants, canonicalResourceKind(resource))
					}
				}
			}
		case strings.Contains(partLower, "cluster deletion did not complete before the deadline"):
			continue
		default:
			part = reClusterServiceClusterRef.ReplaceAllString(part, "<cluster-id>")
			part = reDeletionDispatchedAt.ReplaceAllString(part, "")
			if part != "" {
				summary = append(summary, part)
			}
		}
	}
	if len(descendants) > 0 {
		sort.Strings(descendants)
		descendants = compactStrings(descendants)
		summary = append(summary, "[descendantResources] remaining resources: "+strings.Join(descendants, ", "))
	}
	return strings.Join(summary, "; ")
}

func canonicalResourceKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "nodepools":
		return "nodePools"
	case "serviceprovidernodepools":
		return "serviceProviderNodePools"
	case "serviceproviderclusters":
		return "serviceProviderClusters"
	default:
		return strings.TrimSpace(value)
	}
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func appendAzureIdentityErrorDescription(message string, text string) string {
	if !strings.Contains(strings.ToLower(message), "clientcertificatecredential authentication failed") {
		return message
	}
	match := reAzureErrorDescription.FindStringSubmatch(text)
	if len(match) < 2 {
		return message
	}
	description := strings.TrimSpace(match[1])
	for _, marker := range []string{" Trace ID:", " Correlation ID:", " Timestamp:"} {
		if idx := strings.Index(description, marker); idx >= 0 {
			description = strings.TrimSpace(description[:idx])
		}
	}
	description = strings.TrimRight(description, " .") + "."
	if description == "." || strings.Contains(strings.ToLower(message), strings.ToLower(description)) {
		return message
	}
	return strings.TrimRight(message, " ;") + "; " + description
}

func splitNonEmptyLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func containsPlaceholderToken(value string) bool {
	return strings.Contains(value, "<") && strings.Contains(value, ">")
}

func ContainsPlaceholderToken(value string) bool {
	return containsPlaceholderToken(value)
}

func refineDeserializationNoOutputPicked(raw string, picked string) string {
	if !containsDeserializationNoOutputSignal(raw) && !containsDeserializationNoOutputSignal(picked) {
		return picked
	}
	if deserializationLine := lastDeserializationNoOutputLine(raw); deserializationLine != "" {
		return deserializationLine
	}
	if commandLine := lastCommandErrorLine(raw); commandLine != "" && !isBareCommandErrorExitStatus(commandLine) {
		return commandLine
	}
	return picked
}

func refineCommandErrorExitStatusOnly(raw string, picked string) string {
	normalized := strings.ToLower(collapseWS(strings.TrimSpace(picked)))
	if normalized != "command error: exit status 1" &&
		normalized != "command error: exit status 2" &&
		normalized != "command error: exit status 3" {
		return picked
	}
	if refined := bestSignalErrorLine(raw); refined != "" {
		return refined
	}
	return picked
}

func containsDeserializationNoOutputSignal(value string) bool {
	return reDeserializationNoOutput.MatchString(strings.TrimSpace(value))
}

func containsDeserializationErrorToken(value string) bool {
	return reDeserializationToken.MatchString(strings.TrimSpace(value))
}

func lastDeserializationNoOutputLine(value string) string {
	matches := reDeserializationNoOutput.FindAllString(value, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		line := strings.TrimSpace(matches[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func lastCommandErrorLine(value string) string {
	matches := reCommandErrorLine.FindAllString(value, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		line := strings.TrimSpace(matches[i])
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "Command Error: no output from command") {
			continue
		}
		return line
	}
	return ""
}

func isBareCommandErrorExitStatus(value string) bool {
	normalized := strings.ToLower(collapseWS(strings.TrimSpace(value)))
	switch normalized {
	case "command error: exit status 1", "command error: exit status 2", "command error: exit status 3":
		return true
	default:
		return false
	}
}

func bestSignalErrorLine(text string) string {
	lines := splitNonEmptyLines(text)
	if len(lines) == 0 {
		return ""
	}

	preStruct := make([]string, 0, len(lines))
	postStruct := make([]string, 0, len(lines))
	inStructBlock := false
	for _, line := range lines {
		token := strings.TrimSpace(line)
		if token == "" {
			continue
		}
		if isStructBoundaryLine(token) {
			inStructBlock = true
			continue
		}
		if !rePickErrorSignal.MatchString(token) || isAssertionTail(token) {
			continue
		}
		if isWrapperNoiseLine(token) || isStructFieldNoiseLine(token) || isStatusBannerLine(token) {
			continue
		}
		if inStructBlock {
			postStruct = append(postStruct, token)
		} else {
			preStruct = append(preStruct, token)
		}
	}

	if len(preStruct) > 0 {
		return preStruct[len(preStruct)-1]
	}
	if len(postStruct) > 0 {
		return postStruct[len(postStruct)-1]
	}
	return ""
}

func bestHTTPResponseStatusLine(text string) string {
	lines := splitNonEmptyLines(text)
	for _, line := range lines {
		token := collapseWS(line)
		if reHTTPResponseStatusLine.MatchString(token) {
			return token
		}
	}
	return ""
}

func bestCandidateGraphFailure(text string) string {
	lines := splitNonEmptyLines(text)
	best := ""
	for _, line := range lines {
		token := collapseWS(line)
		lowered := strings.ToLower(token)
		if !strings.Contains(lowered, "query candidate graph for") {
			continue
		}
		switch {
		case strings.Contains(lowered, "returned 503 service unavailable"):
			return token
		case strings.Contains(lowered, "no such host"):
			return token
		case strings.Contains(lowered, "client.timeout exceeded while awaiting headers"):
			return token
		case strings.Contains(lowered, "context deadline exceeded"):
			best = token
		}
	}
	return best
}

func bestImageMirrorInnerFailure(text string) string {
	if !strings.Contains(strings.ToLower(text), "error running image mirror step, failed to execute shell command:") {
		return ""
	}
	return bestInnerStepErrorLine(text)
}

func bestInnerStepErrorLine(text string) string {
	lines := splitNonEmptyLines(text)
	best := ""
	givingUp := ""
	for _, line := range lines {
		token := collapseWS(line)
		lowered := strings.ToLower(token)
		if token == "" || isAssertionTail(token) || isStructFieldNoiseLine(token) || isStatusBannerLine(token) {
			continue
		}
		if isStructuredStepWrapperPick(token) || isStepSetupNoiseLine(lowered) {
			continue
		}
		// Repetitive progress-polling lines (state-transition waits and
		// retry-loop attempts) carry no distinguishing signal beyond elapsed
		// seconds/attempt counts that vary run-to-run; skip them so they
		// never become the picked canonical text.
		if isProgressPollingNoiseLine(lowered) {
			continue
		}
		if givingUp == "" {
			if match := reGivingUpAfterAttempts.FindString(token); match != "" {
				givingUp = normalizeGivingUpAfterAttempts(token)
			}
		}
		if strings.HasPrefix(lowered, "error:") {
			best = token
			continue
		}
		// A bare "context deadline exceeded" line carries no distinguishing
		// detail on its own (see reCleanQuotedOpaqueID / wrapperOnly generic
		// handling elsewhere); prefer letting a genuinely distinguishing line
		// (or the unmodified raw value fallback) win instead of collapsing to
		// this wrapper phrase.
		if lowered == "context deadline exceeded" {
			continue
		}
		if rePickErrorSignal.MatchString(token) {
			best = token
		}
	}
	if best != "" {
		return best
	}
	return givingUp
}

// unescapeLogfmtNewlines converts literal "\r\n"/"\n"/"\r" escape sequences
// (as they appear inside a logfmt/JSON quoted string field) into real newline
// characters so downstream line-based noise filtering can operate per log
// line instead of treating the whole multi-line value as one unsplit token.
func unescapeLogfmtNewlines(value string) string {
	value = strings.ReplaceAll(value, `\r\n`, "\n")
	value = strings.ReplaceAll(value, `\n`, "\n")
	value = strings.ReplaceAll(value, `\r`, "\n")
	return value
}

// isProgressPollingNoiseLine matches repetitive step-progress log lines whose
// only variance is an elapsed-time or attempt counter, e.g.:
//
//	Waiting for swift-vnet-aks-net to finish (state: Running, elapsed 180s)...
//	[swift-vnet] dns readiness (login.microsoftonline.com): attempt 12 failed (elapsed 116s/480s; ...); retrying in 5s...
func isProgressPollingNoiseLine(lowered string) bool {
	return reWaitingToFinishProgressLine.MatchString(lowered) || reRetryAttemptProgressLine.MatchString(lowered)
}

func normalizeGivingUpAfterAttempts(line string) string {
	return reGivingUpAfterAttempts.ReplaceAllString(line, "giving up after <n> attempt(s) / <duration>s (limit <duration>s)")
}

func isStructBoundaryLine(line string) bool {
	switch strings.TrimSpace(line) {
	case "{", "}", "},", "[", "]", "],":
		return true
	default:
		return false
	}
}

func isWrapperNoiseLine(line string) bool {
	normalized := strings.ToLower(collapseWS(line))
	if normalized == "" {
		return true
	}
	if isEventuallyWrapperLine(normalized) || isTimedOutAfterLine(normalized) {
		return true
	}
	return strings.HasPrefix(normalized, "fail [") ||
		strings.HasPrefix(normalized, "unexpected error") ||
		strings.HasPrefix(normalized, "<*fmt.wraperror") ||
		strings.HasPrefix(normalized, "<*errors.errorstring") ||
		strings.HasPrefix(normalized, "<*")
}

func WrapperNoiseLine(line string) bool {
	return isWrapperNoiseLine(line)
}

func isStructFieldNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	normalized := strings.ToLower(collapseWS(trimmed))
	switch normalized {
	case "", "{", "}", "},", "[]", "{}", "{},", "null":
		return true
	}
	if strings.HasPrefix(normalized, "msg:") || strings.HasPrefix(normalized, "err:") || strings.HasPrefix(normalized, "cause:") {
		return true
	}
	if strings.HasPrefix(normalized, "istimeout:") ||
		strings.HasPrefix(normalized, "istemporary:") ||
		strings.HasPrefix(normalized, "isnotfound:") ||
		strings.HasPrefix(normalized, "server:") {
		return true
	}
	if strings.HasPrefix(normalized, "errorcode:\"\"") || strings.HasPrefix(normalized, "errorcode: \"\"") || strings.HasPrefix(normalized, "errorcode:''") || strings.HasPrefix(normalized, "errorcode: ''") {
		return true
	}
	if strings.Contains(normalized, "<context.") && strings.Contains(normalized, "{") {
		return true
	}
	if isPlaceholderAssertionValueLine(trimmed) || reStructuredFieldLine.MatchString(trimmed) {
		return true
	}
	return strings.HasPrefix(trimmed, "<*") && strings.Contains(trimmed, "{")
}

func StructFieldNoiseLine(line string) bool {
	return isStructFieldNoiseLine(line)
}

func isStatusBannerLine(line string) bool {
	normalized := strings.ToLower(collapseWS(line))
	return strings.HasPrefix(normalized, "response ") ||
		strings.HasPrefix(normalized, "error code unavailable") ||
		strings.HasPrefix(normalized, "response contained no body")
}

func StatusBannerLine(line string) bool {
	return isStatusBannerLine(line)
}

func isStructuredStepWrapperPick(value string) bool {
	normalized := strings.ToLower(collapseWS(value))
	return strings.Contains(normalized, "error running image mirror step, failed to execute shell command:") ||
		strings.Contains(normalized, "error running helm release deployment step, failed to deploy helm release:") ||
		strings.Contains(normalized, "error running shell step, failed to execute shell command:") ||
		strings.Contains(normalized, "failed to run arm step:")
}

func isPlaceholderAssertionValueLine(line string) bool {
	return rePlaceholderAssertionValue.MatchString(strings.TrimSpace(line))
}

func bestX509CertificateMismatchDetail(text string) string {
	match := reX509CertificateMismatch.FindString(text)
	if match == "" {
		return ""
	}
	return collapseWS(match)
}

func shouldPreferX509CertificateMismatchDetail(picked string) bool {
	normalized := strings.ToLower(collapseWS(picked))
	if normalized == "" {
		return true
	}
	return strings.HasPrefix(normalized, "verifybasicaccess failed:") ||
		strings.HasPrefix(normalized, "verifyallapiservicesavailable failed:") ||
		strings.Contains(normalized, "tls: failed to verify certificate: x509:")
}

func isStepSetupNoiseLine(lowered string) bool {
	return strings.HasPrefix(lowered, "checking use_oc_login_registries:") ||
		strings.HasPrefix(lowered, "setting up registry authentication") ||
		strings.HasPrefix(lowered, "info: using registry public hostname") ||
		strings.HasPrefix(lowered, "saved credentials for registry") ||
		strings.HasPrefix(lowered, "logging into target acr") ||
		strings.HasPrefix(lowered, "login succeeded") ||
		strings.HasPrefix(lowered, "mirroring image ") ||
		strings.HasPrefix(lowered, "the image will still be available under")
}

func isLowInformationCanonical(value string) bool {
	canonical := strings.TrimSpace(value)
	normalized := strings.ToLower(collapseWS(canonical))
	if normalized == "" {
		return true
	}
	if _, found := wrapperOnly[normalized]; found {
		return true
	}
	if reUnexpectedOnly.MatchString(canonical) {
		return true
	}
	if isStructBoundaryLine(canonical) || isStructFieldNoiseLine(canonical) {
		return true
	}
	if strings.Contains(normalized, "<context.") && strings.Contains(normalized, "{") {
		return true
	}
	return strings.Contains(normalized, "errorcode:\"\"") || strings.Contains(normalized, "errorcode: \"\"") || strings.Contains(normalized, "errorcode:''") || strings.Contains(normalized, "errorcode: ''")
}

func bestContextDeadlineDetail(text string) string {
	lines := splitNonEmptyLines(text)
	best := ""
	assertionFallback := ""
	for _, line := range lines {
		token := collapseWS(line)
		lowered := strings.ToLower(token)
		if token == "" {
			continue
		}
		// Ginkgo assertion headers ("fail [file.go:191]: <message>") are
		// normally treated as wrapper noise, but when the rest of the block is
		// generic ("Unexpected error:" / type annotation / bare "context
		// deadline exceeded" / "occurred") this header's message is the only
		// test-specific detail available. Capture it as a last-resort fallback
		// so distinct failures (e.g. different assertions that all end in a
		// deadline error) don't collapse into the same generic canonical text.
		if assertionFallback == "" {
			if detail := assertionHeaderDetail(token); detail != "" {
				assertionFallback = detail
			}
		}
		if isWrapperNoiseLine(token) || isStructFieldNoiseLine(token) || isStatusBannerLine(token) || isAssertionTail(token) {
			continue
		}
		if reRateLimiterDeadline.MatchString(token) {
			return token
		}
		if reClusterOperatorsUnavailable.MatchString(token) {
			best = token
			continue
		}
		if strings.Contains(lowered, "context deadline exceeded") && lowered != "context deadline exceeded" {
			best = token
		}
	}
	if best != "" {
		return best
	}
	if route := reRouteHostNeverFound.FindString(text); route != "" {
		return route
	}
	if assertionFallback != "" {
		return assertionFallback
	}
	return ""
}

// assertionHeaderDetail extracts the message following a Ginkgo
// "fail [file.go:line]:" marker, e.g. "failed to wait for first cluster
// \"basic-hcp-cluster\" to complete creation (timeout '20.000000' minutes)".
// Returns "" if there is no header, or the message itself is just the
// generic "context deadline exceeded" phrase.
func assertionHeaderDetail(line string) string {
	match := reFailAssertionHeader.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	detail := strings.TrimSpace(match[1])
	if detail == "" || strings.EqualFold(detail, "context deadline exceeded") {
		return ""
	}
	return detail
}

func extractAssertionHeaderContext(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if detail := assertionHeaderDetail(collapseWS(line)); detail != "" {
			return detail
		}
	}
	return ""
}

func truncateText(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:max])
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
