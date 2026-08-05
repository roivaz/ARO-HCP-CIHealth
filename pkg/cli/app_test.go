package cli

import "testing"

func TestSiteRunURLFromListenAddress(t *testing.T) {
	testCases := []struct {
		name     string
		listen   string
		expected string
	}{
		{
			name:     "defaults when empty",
			listen:   "",
			expected: "http://127.0.0.1:8080",
		},
		{
			name:     "loopback host and port",
			listen:   "127.0.0.1:9000",
			expected: "http://127.0.0.1:9000",
		},
		{
			name:     "wildcard host normalizes to localhost",
			listen:   "0.0.0.0:8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "empty host normalizes to localhost",
			listen:   ":8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "invalid hostport falls back to raw",
			listen:   "localhost",
			expected: "http://localhost",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := siteRunURLFromListenAddress(tc.listen)
			if got != tc.expected {
				t.Fatalf("unexpected URL: got %q want %q", got, tc.expected)
			}
		})
	}
}

func TestNewAppCommandDoesNotExposeExportSiteSubcommand(t *testing.T) {
	t.Parallel()

	cmd, err := NewAppCommand()
	if err != nil {
		t.Fatalf("create app command: %v", err)
	}

	if got, want := cmd.Name(), "app"; got != want {
		t.Fatalf("unexpected command name: got=%q want=%q", got, want)
	}

	exportCmd, _, err := cmd.Find([]string{"export-site"})
	if err == nil && exportCmd != nil && exportCmd.Name() == "export-site" {
		t.Fatalf("did not expect export-site subcommand to be present")
	}
}

func TestSplitAndTrim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "  ", nil},
		{"single", "dev", []string{"dev"}},
		{"multiple", "dev,int,stg", []string{"dev", "int", "stg"}},
		{"with spaces", " dev , int , stg ", []string{"dev", "int", "stg"}},
		{"trailing comma", "dev,", []string{"dev"}},
		{"only commas", ",,,", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := splitAndTrim(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNewAppCommandDefaultsPreparedWindowCacheFlags(t *testing.T) {
	t.Parallel()

	cmd, err := NewAppCommand()
	if err != nil {
		t.Fatalf("create app command: %v", err)
	}

	cacheEnabledFlag := cmd.Flags().Lookup("app.failure-patterns-cache")
	if cacheEnabledFlag == nil {
		t.Fatalf("expected app.failure-patterns-cache flag")
	}
	if got, want := cacheEnabledFlag.DefValue, "true"; got != want {
		t.Fatalf("unexpected cache enabled default: got=%q want=%q", got, want)
	}

	cacheWindowFlag := cmd.Flags().Lookup("app.failure-patterns-cache-window")
	if cacheWindowFlag == nil {
		t.Fatalf("expected app.failure-patterns-cache-window flag")
	}
	if got, want := cacheWindowFlag.DefValue, "840h0m0s"; got != want {
		t.Fatalf("unexpected cache window default: got=%q want=%q", got, want)
	}

	cacheRefreshFlag := cmd.Flags().Lookup("app.failure-patterns-cache-refresh")
	if cacheRefreshFlag == nil {
		t.Fatalf("expected app.failure-patterns-cache-refresh flag")
	}
	if got, want := cacheRefreshFlag.DefValue, "10m0s"; got != want {
		t.Fatalf("unexpected cache refresh default: got=%q want=%q", got, want)
	}

	cacheTTLFlag := cmd.Flags().Lookup("app.failure-patterns-cache-ttl")
	if cacheTTLFlag == nil {
		t.Fatalf("expected app.failure-patterns-cache-ttl flag")
	}
	if got, want := cacheTTLFlag.DefValue, "12m0s"; got != want {
		t.Fatalf("unexpected cache ttl default: got=%q want=%q", got, want)
	}
}
