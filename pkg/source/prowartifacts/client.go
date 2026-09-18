package prowartifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
)

const (
	defaultHTTPTimeout       = 90 * time.Second
	maxArtifactResponseBytes = 32 << 20
	prowJobArtifactPath      = "prowjob.json"
)

type ArtifactOutcome string

const (
	ArtifactOutcomeFound     ArtifactOutcome = "found"
	ArtifactOutcomeMissing   ArtifactOutcome = "missing"
	ArtifactOutcomeForbidden ArtifactOutcome = "forbidden"
	ArtifactOutcomeInvalid   ArtifactOutcome = "invalid"
)

type ArtifactResult struct {
	Outcome ArtifactOutcome
	Body    []byte
	URL     string
}

type Failure struct {
	ArtifactURL string
	TestName    string
	TestSuite   string
	FailureText string
}

type FailureListResult struct {
	Outcome  ArtifactOutcome
	Failures []Failure
}

type RegionResult struct {
	Outcome     ArtifactOutcome
	Region      string
	ArtifactURL string
}

type TimingResult struct {
	Outcome     ArtifactOutcome
	StartedAt   string
	CompletedAt string
	ArtifactURL string
}

type Client interface {
	FetchArtifact(ctx context.Context, runURL string, relativePath string) (ArtifactResult, error)
	ListFailures(ctx context.Context, environment string, runURL string) (FailureListResult, error)
	GetRunTiming(ctx context.Context, runURL string) (TimingResult, error)
	GetRunRegion(ctx context.Context, runURL string, relativePath string) (RegionResult, error)
}

type ClientOptions struct {
	ArtifactsBaseURL       string
	JUnitPathsByEnvMapping map[string][]string
	DefaultJUnitPaths      []string
}

type HTTPClient struct {
	artifactsBaseURL       string
	httpClient             *http.Client
	junitPathsByEnvMapping map[string][]string
	defaultJUnitPaths      []string
}

func NewHTTPClient(options ClientOptions) *HTTPClient {
	baseURL := strings.TrimSpace(options.ArtifactsBaseURL)
	if baseURL == "" {
		baseURL = sourceoptions.DefaultRuntimeDefaults().ProwArtifactsBaseURL
	}
	junitPathsByEnvMapping := options.JUnitPathsByEnvMapping
	if len(junitPathsByEnvMapping) == 0 {
		junitPathsByEnvMapping = sourceoptions.DeterministicJUnitPathsByEnvironment()
	}
	defaultJUnitPaths := normalizeDeterministicPaths(options.DefaultJUnitPaths)
	if len(defaultJUnitPaths) == 0 {
		defaultJUnitPaths = normalizeDeterministicPaths(sourceoptions.DefaultJUnitPaths())
	}
	return &HTTPClient{
		artifactsBaseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		junitPathsByEnvMapping: copyJUnitPathMap(junitPathsByEnvMapping),
		defaultJUnitPaths:      append([]string(nil), defaultJUnitPaths...),
	}
}

func (c *HTTPClient) ListFailures(ctx context.Context, environment string, runURL string) (FailureListResult, error) {
	junitPaths := c.junitPathsForEnvironment(environment)
	failures := make([]Failure, 0, 8)
	seen := map[string]struct{}{}
	fetchErrors := make([]error, 0, len(junitPaths))
	sawFound := false
	sawForbidden := false
	sawInvalid := false

	for _, junitPath := range junitPaths {
		result, err := c.FetchArtifact(ctx, runURL, junitPath)
		if err != nil {
			fetchErrors = append(fetchErrors, fmt.Errorf("fetch junit %q: %w", junitPath, err))
			continue
		}
		switch result.Outcome {
		case ArtifactOutcomeForbidden:
			sawForbidden = true
			continue
		case ArtifactOutcomeMissing:
			continue
		case ArtifactOutcomeInvalid:
			sawInvalid = true
			continue
		case ArtifactOutcomeFound:
			sawFound = true
		default:
			return FailureListResult{}, fmt.Errorf("fetch junit %q returned unknown artifact outcome %q", junitPath, result.Outcome)
		}

		rows, err := parseJUnitFailures(result.Body, result.URL)
		if err != nil {
			// A 200 with unparsable/empty/non-junit XML is a terminal content
			// mismatch for this deterministic path. Treat as missing instead of a
			// retryable transport error.
			sawInvalid = true
			continue
		}

		for _, row := range rows {
			testName := strings.TrimSpace(row.TestName)
			failureText := strings.TrimSpace(row.FailureText)
			if testName == "" || failureText == "" {
				continue
			}
			key := strings.TrimSpace(row.ArtifactURL) + "\x00" + strings.TrimSpace(row.TestSuite) + "\x00" + testName + "\x00" + failureText
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			failures = append(failures, row)
		}
	}

	if len(fetchErrors) > 0 {
		return FailureListResult{}, errors.Join(fetchErrors...)
	}
	if len(failures) > 0 {
		sort.Slice(failures, func(i, j int) bool {
			if failures[i].ArtifactURL != failures[j].ArtifactURL {
				return failures[i].ArtifactURL < failures[j].ArtifactURL
			}
			if failures[i].TestSuite != failures[j].TestSuite {
				return failures[i].TestSuite < failures[j].TestSuite
			}
			if failures[i].TestName != failures[j].TestName {
				return failures[i].TestName < failures[j].TestName
			}
			return failures[i].FailureText < failures[j].FailureText
		})
		return FailureListResult{
			Outcome:  ArtifactOutcomeFound,
			Failures: failures,
		}, nil
	}
	if sawForbidden {
		return FailureListResult{Outcome: ArtifactOutcomeForbidden}, nil
	}
	if sawFound {
		return FailureListResult{Outcome: ArtifactOutcomeFound, Failures: []Failure{}}, nil
	}
	if sawInvalid {
		return FailureListResult{Outcome: ArtifactOutcomeInvalid, Failures: []Failure{}}, nil
	}
	return FailureListResult{Outcome: ArtifactOutcomeMissing, Failures: []Failure{}}, nil
}

func (c *HTTPClient) junitPathsForEnvironment(environment string) []string {
	normalizedEnv := strings.ToLower(strings.TrimSpace(environment))
	if normalizedEnv != "" {
		if paths := normalizeDeterministicPaths(c.junitPathsByEnvMapping[normalizedEnv]); len(paths) > 0 {
			return paths
		}
	}
	return append([]string(nil), c.defaultJUnitPaths...)
}

func (c *HTTPClient) artifactURL(prefix string, relPath string) string {
	joined := path.Join(strings.Trim(prefix, "/"), strings.Trim(relPath, "/"))
	return c.artifactsBaseURL + "/" + joined
}

func (c *HTTPClient) FetchArtifact(ctx context.Context, runURL string, relativePath string) (ArtifactResult, error) {
	prefix, err := ArtifactPrefixFromRunURL(runURL)
	if err != nil {
		return ArtifactResult{}, err
	}
	return c.fetchArtifactURL(ctx, c.artifactURL(prefix, relativePath))
}

func (c *HTTPClient) GetRunRegion(ctx context.Context, runURL string, relativePath string) (RegionResult, error) {
	result, err := c.FetchArtifact(ctx, runURL, relativePath)
	if err != nil {
		return RegionResult{}, err
	}
	regionResult := RegionResult{
		Outcome:     result.Outcome,
		ArtifactURL: result.URL,
	}
	if result.Outcome != ArtifactOutcomeFound {
		return regionResult, nil
	}

	region, err := parseRuntimeRegion(result.Body)
	if err != nil {
		regionResult.Outcome = ArtifactOutcomeInvalid
		return regionResult, nil
	}
	regionResult.Region = region
	return regionResult, nil
}

func (c *HTTPClient) GetRunTiming(ctx context.Context, runURL string) (TimingResult, error) {
	result, err := c.FetchArtifact(ctx, runURL, prowJobArtifactPath)
	if err != nil {
		return TimingResult{}, err
	}
	timingResult := TimingResult{
		Outcome:     result.Outcome,
		ArtifactURL: result.URL,
	}
	if result.Outcome != ArtifactOutcomeFound {
		return timingResult, nil
	}

	startedAt, completedAt, err := parseProwJobTiming(result.Body)
	if err != nil {
		timingResult.Outcome = ArtifactOutcomeInvalid
		return timingResult, nil
	}
	timingResult.StartedAt = startedAt
	timingResult.CompletedAt = completedAt
	return timingResult, nil
}

func (c *HTTPClient) fetchArtifactURL(ctx context.Context, artifactURL string) (ArtifactResult, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ArtifactResult{}, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
		if err != nil {
			return ArtifactResult{}, fmt.Errorf("build artifact request: %w", err)
		}
		req.Header.Set("Accept", "application/json,application/xml,text/xml,text/plain,*/*")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ArtifactResult{}, ctx.Err()
			}
			lastErr = fmt.Errorf("fetch artifact %q: %w", artifactURL, err)
			if attempt < maxAttempts {
				if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
					return ArtifactResult{}, err
				}
				continue
			}
			return ArtifactResult{}, lastErr
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxArtifactResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read artifact response %q: %w", artifactURL, readErr)
			if attempt < maxAttempts {
				if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
					return ArtifactResult{}, err
				}
				continue
			}
			return ArtifactResult{}, lastErr
		}

		if resp.StatusCode == http.StatusNotFound {
			return ArtifactResult{Outcome: ArtifactOutcomeMissing, URL: artifactURL}, nil
		}
		if resp.StatusCode == http.StatusForbidden {
			return ArtifactResult{Outcome: ArtifactOutcomeForbidden, URL: artifactURL}, nil
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("fetch artifact %q returned status %d: %s", artifactURL, resp.StatusCode, strings.TrimSpace(string(limitBytes(body, 2048))))
			if attempt < maxAttempts && isRetryableStatusCode(resp.StatusCode) {
				if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
					return ArtifactResult{}, err
				}
				continue
			}
			return ArtifactResult{}, lastErr
		}
		if len(body) > maxArtifactResponseBytes {
			return ArtifactResult{}, fmt.Errorf("artifact %q exceeds maximum response size of %d bytes", artifactURL, maxArtifactResponseBytes)
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/html") || looksLikeHTML(body) {
			// Some artifact paths legitimately resolve to HTML directory/index pages.
			// Treat those as "artifact not found" instead of retryable failures.
			return ArtifactResult{Outcome: ArtifactOutcomeMissing, URL: artifactURL}, nil
		}

		return ArtifactResult{
			Outcome: ArtifactOutcomeFound,
			Body:    body,
			URL:     artifactURL,
		}, nil
	}

	if lastErr != nil {
		return ArtifactResult{}, lastErr
	}
	return ArtifactResult{}, fmt.Errorf("fetch artifact %q failed without explicit error", artifactURL)
}

func isRetryableStatusCode(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusRequestTimeout ||
		statusCode >= http.StatusInternalServerError
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var ansiEscapeSequence = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func parseProwJobTiming(contents []byte) (string, string, error) {
	var prowJob struct {
		Status struct {
			StartTime      string `json:"startTime"`
			CompletionTime string `json:"completionTime"`
		} `json:"status"`
	}
	if err := json.Unmarshal(contents, &prowJob); err != nil {
		return "", "", fmt.Errorf("decode prowjob metadata: %w", err)
	}

	startedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(prowJob.Status.StartTime))
	if err != nil {
		return "", "", fmt.Errorf("parse prowjob startTime: %w", err)
	}
	startedAt = startedAt.UTC()

	rawCompletedAt := strings.TrimSpace(prowJob.Status.CompletionTime)
	if rawCompletedAt == "" {
		return startedAt.Format(time.RFC3339Nano), "", nil
	}
	completedAt, err := time.Parse(time.RFC3339Nano, rawCompletedAt)
	if err != nil {
		return "", "", fmt.Errorf("parse prowjob completionTime: %w", err)
	}
	completedAt = completedAt.UTC()
	if completedAt.Before(startedAt) {
		return "", "", fmt.Errorf("prowjob completionTime precedes startTime")
	}
	return startedAt.Format(time.RFC3339Nano), completedAt.Format(time.RFC3339Nano), nil
}

func parseRuntimeRegion(contents []byte) (string, error) {
	cleaned := ansiEscapeSequence.ReplaceAll(contents, nil)
	const marker = "Acquired slot and wrote shared artifacts"
	markerIndex := bytes.Index(cleaned, []byte(marker))
	if markerIndex < 0 {
		return "", fmt.Errorf("slot acquisition marker not found")
	}
	jsonStart := bytes.IndexByte(cleaned[markerIndex+len(marker):], '{')
	if jsonStart < 0 {
		return "", fmt.Errorf("slot acquisition metadata JSON not found")
	}
	jsonStart += markerIndex + len(marker)

	var metadata struct {
		RuntimeRegion string `json:"runtimeRegion"`
	}
	if err := json.NewDecoder(bytes.NewReader(cleaned[jsonStart:])).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode slot acquisition metadata: %w", err)
	}
	region := strings.ToLower(strings.TrimSpace(metadata.RuntimeRegion))
	if region == "" {
		return "", fmt.Errorf("slot acquisition metadata missing runtimeRegion")
	}
	return region, nil
}

func CanonicalRunURL(deckBaseURL string, runURL string) (string, error) {
	prefix, err := ArtifactPrefixFromRunURL(runURL)
	if err != nil {
		return "", err
	}

	trimmedBaseURL := strings.TrimSpace(deckBaseURL)
	if trimmedBaseURL == "" {
		return "", fmt.Errorf("deck base URL is required")
	}

	parsedBaseURL, err := url.Parse(trimmedBaseURL)
	if err != nil {
		return "", fmt.Errorf("parse deck base URL: %w", err)
	}
	parsedBaseURL.RawQuery = ""
	parsedBaseURL.Fragment = ""
	basePath := strings.TrimRight(parsedBaseURL.Path, "/")
	if strings.HasSuffix(strings.ToLower(basePath), ".js") {
		if slash := strings.LastIndex(basePath, "/"); slash >= 0 {
			basePath = basePath[:slash]
		} else {
			basePath = ""
		}
	}
	parsedBaseURL.Path = basePath + "/view/gs/" + strings.Trim(prefix, "/")
	return parsedBaseURL.String(), nil
}

func ArtifactPrefixFromRunURL(runURL string) (string, error) {
	trimmed := strings.TrimSpace(runURL)
	if trimmed == "" {
		return "", fmt.Errorf("run URL is required")
	}
	if strings.HasPrefix(trimmed, "gs://") {
		return strings.Trim(strings.TrimPrefix(trimmed, "gs://"), "/"), nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse run URL: %w", err)
	}

	if strings.EqualFold(parsed.Scheme, "gs") {
		prefix := strings.Trim(parsed.Host+parsed.Path, "/")
		if prefix == "" {
			return "", fmt.Errorf("invalid gs run URL %q", runURL)
		}
		return prefix, nil
	}

	if prefix, ok := prefixFromHTTPPath(parsed.Path); ok {
		return prefix, nil
	}

	if strings.EqualFold(parsed.Host, "storage.googleapis.com") {
		prefix := strings.Trim(parsed.Path, "/")
		if prefix == "" {
			return "", fmt.Errorf("storage run URL missing object path %q", runURL)
		}
		return prefix, nil
	}

	return "", fmt.Errorf("unsupported run URL format %q", runURL)
}

// IsBatchRunURL reports whether a run URL belongs to a Tide batch job rather
// than a single-PR presubmit check. Batch jobs are stored under the synthetic
// pull identity "batch" (e.g. .../pr-logs/pull/batch/<job>/<build>), whereas PR
// checks are stored under "<org>_<repo>" (e.g. .../pr-logs/pull/Azure_ARO-HCP/
// <pr>/<job>/<build>). The classification is derived from the run URL alone.
func IsBatchRunURL(runURL string) bool {
	prefix, err := ArtifactPrefixFromRunURL(runURL)
	if err != nil {
		return false
	}
	segments := strings.Split(strings.Trim(prefix, "/"), "/")
	for i, segment := range segments {
		if segment != "pull" {
			continue
		}
		if i+1 < len(segments) && segments[i+1] == "batch" {
			return true
		}
	}
	return false
}

func prefixFromHTTPPath(rawPath string) (string, bool) {
	p := strings.TrimSpace(rawPath)
	if strings.HasPrefix(p, "/view/gs/") {
		return strings.TrimPrefix(strings.Trim(p, "/"), "view/gs/"), true
	}
	if strings.HasPrefix(p, "/view/gcs/") {
		return strings.TrimPrefix(strings.Trim(p, "/"), "view/gcs/"), true
	}
	if strings.HasPrefix(p, "/gcs/") {
		return strings.TrimPrefix(strings.Trim(p, "/"), "gcs/"), true
	}
	return "", false
}

func parseJUnitFailures(contents []byte, artifactURL string) ([]Failure, error) {
	rootName, err := detectRootElement(contents)
	if err != nil {
		return nil, err
	}

	failures := make([]Failure, 0, 8)

	switch strings.ToLower(rootName) {
	case "testsuite":
		var suite junitTestSuite
		if err := xml.Unmarshal(contents, &suite); err != nil {
			return nil, err
		}
		collectFailuresFromSuite(artifactURL, suite, "", &failures)
	case "testsuites":
		var root junitTestSuites
		if err := xml.Unmarshal(contents, &root); err != nil {
			return nil, err
		}
		for _, testcase := range root.TestCases {
			collectFailureFromCase(artifactURL, "", testcase, &failures)
		}
		for _, suite := range root.TestSuites {
			collectFailuresFromSuite(artifactURL, suite, "", &failures)
		}
	default:
		return nil, fmt.Errorf("unsupported junit root element %q", rootName)
	}

	return failures, nil
}

func detectRootElement(payload []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return "", fmt.Errorf("empty XML payload")
			}
			return "", err
		}
		if start, ok := token.(xml.StartElement); ok {
			return start.Name.Local, nil
		}
	}
}

func collectFailuresFromSuite(artifactURL string, suite junitTestSuite, parentSuiteName string, out *[]Failure) {
	suiteName := strings.TrimSpace(suite.Name)
	if suiteName == "" {
		suiteName = parentSuiteName
	}

	for _, testcase := range suite.TestCases {
		collectFailureFromCase(artifactURL, suiteName, testcase, out)
	}
	for _, child := range suite.TestSuites {
		collectFailuresFromSuite(artifactURL, child, suiteName, out)
	}
}

func collectFailureFromCase(artifactURL, fallbackSuite string, testcase junitTestCase, out *[]Failure) {
	failureText, ok := firstFailureText(testcase)
	if !ok {
		return
	}

	testName := strings.TrimSpace(testcase.Name)
	if testName == "" {
		return
	}

	testSuite := strings.TrimSpace(testcase.ClassName)
	if testSuite == "" {
		testSuite = fallbackSuite
	}

	*out = append(*out, Failure{
		ArtifactURL: strings.TrimSpace(artifactURL),
		TestName:    testName,
		TestSuite:   strings.TrimSpace(testSuite),
		FailureText: failureText,
	})
}

func firstFailureText(testcase junitTestCase) (string, bool) {
	if text, ok := firstFailureTextFromNodes(testcase.Failures); ok {
		return text, true
	}
	if text, ok := firstFailureTextFromNodes(testcase.Errors); ok {
		return text, true
	}
	return "", false
}

func firstFailureTextFromNodes(nodes []junitFailureNode) (string, bool) {
	for _, node := range nodes {
		message := strings.TrimSpace(node.Message)
		content := strings.TrimSpace(node.Content)
		switch {
		case message != "" && content != "":
			return message + "\n" + content, true
		case content != "":
			return content, true
		case message != "":
			return message, true
		}
	}
	return "", false
}

type junitTestSuites struct {
	TestSuites []junitTestSuite `xml:"testsuite"`
	TestCases  []junitTestCase  `xml:"testcase"`
}

type junitTestSuite struct {
	Name       string           `xml:"name,attr"`
	TestSuites []junitTestSuite `xml:"testsuite"`
	TestCases  []junitTestCase  `xml:"testcase"`
}

type junitTestCase struct {
	Name      string             `xml:"name,attr"`
	ClassName string             `xml:"classname,attr"`
	Failures  []junitFailureNode `xml:"failure"`
	Errors    []junitFailureNode `xml:"error"`
}

type junitFailureNode struct {
	Message string `xml:"message,attr"`
	Content string `xml:",chardata"`
}

func looksLikeHTML(body []byte) bool {
	const sniffBytes = 512
	if len(body) > sniffBytes {
		body = body[:sniffBytes]
	}
	trimmed := strings.ToLower(strings.TrimSpace(string(body)))
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

func normalizeDeterministicPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, value := range paths {
		normalized := strings.Trim(strings.TrimSpace(value), "/")
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func copyJUnitPathMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, values := range in {
		out[k] = append([]string(nil), values...)
	}
	return out
}

func limitBytes(in []byte, limit int) []byte {
	if len(in) <= limit {
		return in
	}
	return in[:limit]
}
