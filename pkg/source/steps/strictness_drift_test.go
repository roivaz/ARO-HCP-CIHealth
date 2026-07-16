package steps

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// driftTestEnvVar gates the network-dependent registry drift guard. It is opt-in
// so the default `go test`/`make check` run stays offline and deterministic.
const driftTestEnvVar = "CIHEALTH_REGISTRY_DRIFT_TEST"

const registryRawBase = "https://raw.githubusercontent.com/openshift/release/main/ci-operator/step-registry/aro-hcp"
const registryContentsBase = "https://api.github.com/repos/openshift/release/contents/ci-operator/step-registry/aro-hcp"

// TestVendoredStrictnessMatchesRegistry validates the vendored workflow
// composition and best_effort flags against the live openshift/release step
// registry. Enable with CIHEALTH_REGISTRY_DRIFT_TEST=1 (requires network).
func TestVendoredStrictnessMatchesRegistry(t *testing.T) {
	if strings.TrimSpace(os.Getenv(driftTestEnvVar)) == "" {
		t.Skipf("set %s=1 to run the registry drift guard (requires network)", driftTestEnvVar)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	bestEffortByRef := loadRefBestEffort(t, client)

	workflowFiles := map[Workflow]string{
		WorkflowLocalE2E:      "local-e2e/aro-hcp-local-e2e-workflow.yaml",
		WorkflowPersistentE2E: "persistent-e2e/aro-hcp-persistent-e2e-workflow.yaml",
		WorkflowE2E:           "e2e/aro-hcp-e2e-workflow.yaml",
	}

	for workflow, path := range workflowFiles {
		definition, ok := workflowDefinitions[workflow]
		if !ok {
			t.Errorf("no vendored definition for workflow %q", workflow)
			continue
		}

		composition := fetchWorkflowComposition(t, client, path)

		if got, want := composition, definition.allSteps(); !equalStringSlices(got, want) {
			t.Errorf("workflow %q composition drift:\n live=%v\n vendored=%v", workflow, got, want)
		}

		liveBestEffort := map[string]bool{}
		for _, ref := range composition {
			if bestEffortByRef[ref] {
				liveBestEffort[ref] = true
			}
		}
		if !equalBoolSets(liveBestEffort, definition.bestEffort) {
			t.Errorf("workflow %q best_effort drift:\n live=%v\n vendored=%v", workflow, sortedKeys(liveBestEffort), sortedKeys(definition.bestEffort))
		}
	}
}

// loadRefBestEffort recursively walks the aro-hcp registry and returns each step
// ref's best_effort flag keyed by its `as` name.
func loadRefBestEffort(t *testing.T, client *http.Client) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	var walk func(url string)
	walk = func(url string) {
		entries := fetchContents(t, client, url)
		for _, entry := range entries {
			switch entry.Type {
			case "dir":
				walk(entry.URL)
			case "file":
				if !strings.HasSuffix(entry.Name, "-ref.yaml") {
					continue
				}
				name, bestEffort := parseRef(t, decodeContent(t, entry))
				if name != "" {
					out[name] = bestEffort
				}
			}
		}
	}
	walk(registryContentsBase)
	return out
}

type contentEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	DownloadURL string `json:"download_url"`
}

func fetchContents(t *testing.T, client *http.Client, url string) []contentEntry {
	t.Helper()
	body := httpGet(t, client, url)
	var entries []contentEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("decode contents %q: %v", url, err)
	}
	return entries
}

func decodeContent(t *testing.T, entry contentEntry) []byte {
	t.Helper()
	if entry.Encoding == "base64" && entry.Content != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(entry.Content, "\n", ""))
		if err != nil {
			t.Fatalf("decode base64 content for %q: %v", entry.Name, err)
		}
		return decoded
	}
	if entry.DownloadURL != "" {
		return httpGet(t, &http.Client{Timeout: 30 * time.Second}, entry.DownloadURL)
	}
	t.Fatalf("no content available for %q", entry.Name)
	return nil
}

func parseRef(t *testing.T, payload []byte) (string, bool) {
	t.Helper()
	var doc struct {
		Ref struct {
			As         string `yaml:"as"`
			BestEffort bool   `yaml:"best_effort"`
		} `yaml:"ref"`
	}
	if err := yaml.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("parse ref yaml: %v", err)
	}
	return strings.TrimSpace(doc.Ref.As), doc.Ref.BestEffort
}

func fetchWorkflowComposition(t *testing.T, client *http.Client, path string) []string {
	t.Helper()
	payload := httpGet(t, client, registryRawBase+"/"+path)
	var doc struct {
		Workflow struct {
			Steps struct {
				Pre []struct {
					Ref string `yaml:"ref"`
				} `yaml:"pre"`
				Test []struct {
					Ref string `yaml:"ref"`
				} `yaml:"test"`
				Post []struct {
					Ref string `yaml:"ref"`
				} `yaml:"post"`
			} `yaml:"steps"`
		} `yaml:"workflow"`
	}
	if err := yaml.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("parse workflow yaml %q: %v", path, err)
	}
	out := []string{}
	for _, s := range doc.Workflow.Steps.Pre {
		out = append(out, strings.TrimSpace(s.Ref))
	}
	for _, s := range doc.Workflow.Steps.Test {
		out = append(out, strings.TrimSpace(s.Ref))
	}
	for _, s := range doc.Workflow.Steps.Post {
		out = append(out, strings.TrimSpace(s.Ref))
	}
	return out
}

func httpGet(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request %q: %v", url, err)
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %q: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %q: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %q returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalBoolSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
