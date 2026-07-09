package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
	readmodelreview "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/review"
	readmodelrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/runlog"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
	postgresstore "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/initdb"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/migrations"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/testsupport/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHandleAPIDailyFailurePatternsRouteRemoved(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/failure-patterns/daily", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusNotFound; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
}

func TestHandleReviewRoutesRemoved(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/review", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if got, want := recorder.Code, http.StatusNotFound; got != want {
		t.Fatalf("unexpected /review status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/review/actions/links", strings.NewReader("week=2026-03-16"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if got, want := recorder.Code, http.StatusNotFound; got != want {
		t.Fatalf("unexpected /review/actions/links status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
}

func TestHandleHealthEndpoints(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected /healthz status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected /readyz status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenPostgresClosed(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	fixture.pool.Close()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("unexpected /readyz status code after pool close: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
}

func TestHandleAPIFailurePatternsReturnsJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
			JobName:     "periodic-ci-nodepool",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, reviewAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	if err := store.UpsertMetricsDaily(ctx, []storecontracts.MetricDailyRecord{
		{Environment: "dev", Date: "2026-03-16", Metric: "run_count", Value: 5},
	}); err != nil {
		t.Fatalf("seed metrics daily: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/failure-patterns/window?start_date=2026-03-16&end_date=2026-03-16&env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("unexpected content type: %q", got)
	}

	var payload readmodelpatterns.FailurePatternsData
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.Contains(recorder.Body.String(), "\"resolved_week\"") {
		t.Fatalf("did not expect resolved_week in failure-pattern payload: %s", recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "\"failure_pattern_id\"") || !strings.Contains(body, "\"matched_occurrences\"") || !strings.Contains(body, "\"runs_affected\"") {
		t.Fatalf("expected renamed failure-pattern keys in body, got %q", body)
	} else if strings.Contains(body, "\"cluster_id\"") || strings.Contains(body, "\"matched_failure_count\"") || strings.Contains(body, "\"jobs_affected\"") {
		t.Fatalf("did not expect stale failure-pattern keys in body, got %q", body)
	}
	if got, want := payload.Meta.Timezone, "UTC"; got != want {
		t.Fatalf("unexpected failure-pattern payload timezone: got=%q want=%q", got, want)
	}
	if got, want := payload.Meta.StartAt, "2026-03-16T00:00:00Z"; got != want {
		t.Fatalf("unexpected failure-pattern payload start_at: got=%q want=%q", got, want)
	}
	if got, want := payload.Meta.EndAt, "2026-03-17T00:00:00Z"; got != want {
		t.Fatalf("unexpected failure-pattern payload end_at: got=%q want=%q", got, want)
	}
	if got, want := len(payload.Environments), 1; got != want {
		t.Fatalf("unexpected environment count: got=%d want=%d", got, want)
	}
	if got, want := payload.Environments[0].Summary.TotalRuns, 5; got != want {
		t.Fatalf("unexpected total runs: got=%d want=%d", got, want)
	}
	if got, want := len(payload.Environments[0].Rows), 2; got != want {
		t.Fatalf("unexpected row count: got=%d want=%d", got, want)
	}
	var linkedRow *readmodelpatterns.FailurePatternsRow
	for index := range payload.Environments[0].Rows {
		row := &payload.Environments[0].Rows[index]
		if len(row.FullErrorSamples) == 1 && row.FullErrorSamples[0] == reviewAPILongRawFailureText() {
			linkedRow = row
			break
		}
	}
	if linkedRow == nil {
		t.Fatalf("expected linked failure-pattern row in payload")
	}
	if got, want := len(linkedRow.FullErrorSamples), 1; got != want {
		t.Fatalf("unexpected full error sample count: got=%d want=%d", got, want)
	}
	if got, want := linkedRow.FullErrorSamples[0], reviewAPILongRawFailureText(); got != want {
		t.Fatalf("expected full raw failure sample without truncation: got=%q want=%q", got, want)
	}
}

func TestHandleAPIFailurePatternsReturnsJSONError(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/failure-patterns/window?start_date=2026-03-16", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got := payload["error"]; !strings.Contains(got, "start_date and end_date must both be set") {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestHandleAPIFailurePatternsRejectsPartialExactWindow(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/failure-patterns/window?start_at=2026-03-16T08:00:00Z", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got := payload["error"]; !strings.Contains(got, "start_at and end_at must both be set") {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestHandleFailurePatternsPageWindowRendersHTML(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	targetStore := fixture.openWeekStore(t, "2026-03-16")
	if err := targetStore.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
			JobName:     "periodic-ci-nodepool",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := targetStore.UpsertRawFailures(ctx, reviewAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/failure-patterns?start_date=2026-03-16&end_date=2026-03-16&env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("unexpected content type: %q", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "Resolved semantic week (UTC)") {
		t.Fatalf("did not expect resolved week note in body, got %q", body)
	}
	if strings.Contains(body, "Runs affected, run impact, and seen-in are recomputed across the selected window") {
		t.Fatalf("did not expect failure-pattern guidance in body, got %q", body)
	}
	if !strings.Contains(body, "CreateNodePool timeout after 45 minutes") {
		t.Fatalf("expected failure-pattern row phrase in body, got %q", body)
	}
	if !strings.Contains(body, `id="failure-patterns-search"`) {
		t.Fatalf("expected failure-patterns search box in body, got %q", body)
	}
	if !strings.Contains(body, `getElementById("failure-patterns-search")`) {
		t.Fatalf("expected failure-patterns search wiring in body, got %q", body)
	}
	if !strings.Contains(body, "Single Day: Mar 16") {
		t.Fatalf("expected single-day time selector label in body, got %q", body)
	}
	if !strings.Contains(body, `type="datetime-local" name="start_at" value="2026-03-16T00:00"`) {
		t.Fatalf("expected start_at control in body, got %q", body)
	}
	if !strings.Contains(body, `type="datetime-local" name="end_at" value="2026-03-17T00:00"`) {
		t.Fatalf("expected end_at control in body, got %q", body)
	}
	if !strings.Contains(body, `name="env"`) || !strings.Contains(body, `option value="dev" selected="selected">Env: DEV</option>`) {
		t.Fatalf("expected env control in body, got %q", body)
	}
	if strings.Contains(body, `type="hidden" name="week"`) {
		t.Fatalf("did not expect hidden week input in body, got %q", body)
	}
	if !strings.Contains(body, "View JSON API") {
		t.Fatalf("expected JSON API link in body, got %q", body)
	}
	if !strings.Contains(body, "Reset") {
		t.Fatalf("expected reset link in body, got %q", body)
	}
	if strings.Contains(body, `class="cards"`) {
		t.Fatalf("did not expect failure-pattern summary cards in body, got %q", body)
	}
	for _, snippet := range []string{
		`class="report-route-link" href="/">Overview</a>`,
		`class="report-route-link active" href="/failure-patterns">Failure Patterns</a>`,
		`class="report-route-link" href="/run-log">Run Log</a>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected default route navigation link %q in body, got %q", snippet, body)
		}
	}
}

func TestHandleFailurePatternsPageExactWindowRendersHTML(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	targetStore := fixture.openWeekStore(t, "2026-03-16")
	if err := targetStore.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:30:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
			JobName:     "periodic-ci-nodepool",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := targetStore.UpsertRawFailures(ctx, reviewAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/failure-patterns?start_at=2026-03-16T08:00:00Z&end_at=2026-03-16T10:00:00Z&env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Custom: Mar 16 08:00 - Mar 16 10:00 UTC") {
		t.Fatalf("expected exact custom label in body, got %q", body)
	}
	if !strings.Contains(body, `name="start_at" value="2026-03-16T08:00"`) {
		t.Fatalf("expected exact start_at control in body, got %q", body)
	}
	if !strings.Contains(body, `name="end_at" value="2026-03-16T10:00"`) {
		t.Fatalf("expected exact end_at control in body, got %q", body)
	}
	if !strings.Contains(body, `start_at=2026-03-16T08%3A00%3A00Z`) || !strings.Contains(body, `end_at=2026-03-16T10%3A00%3A00Z`) {
		t.Fatalf("expected exact-window JSON API href in body, got %q", body)
	}
}

func TestHandleFailurePatternsPageDefaultsToRollingWindow(t *testing.T) {
	fixture := newHandlerFixture(t)

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/failure-patterns", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	body := recorder.Body.String()
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -6).Format("2006-01-02")
	if !strings.Contains(body, `name="start_at" value="`+startDate+`T00:00"`) {
		t.Fatalf("expected default start_at in body, got %q", body)
	}
	nextDay := now.AddDate(0, 0, 1).Format("2006-01-02")
	if !strings.Contains(body, `name="end_at" value="`+nextDay+`T00:00"`) {
		t.Fatalf("expected default end_at in body, got %q", body)
	}
	if !strings.Contains(body, "Last 7 Days") {
		t.Fatalf("expected Last 7 Days selector label in body, got %q", body)
	}
	if !strings.Contains(body, "Apply") {
		t.Fatalf("expected apply button in body, got %q", body)
	}
}

func TestHandleFailurePatternsPageWeekQueryUsesFullWeekWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/2",
			JobName:     "periodic-ci-nodepool",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, reviewAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/failure-patterns?week=2026-03-16", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `name="start_at" value="2026-03-16T00:00"`) {
		t.Fatalf("expected default start_at in body, got %q", body)
	}
	if !strings.Contains(body, `name="end_at" value="2026-03-23T00:00"`) {
		t.Fatalf("expected default end_at in body, got %q", body)
	}
	if !strings.Contains(body, "Weekly: Mar 16 - Mar 22") {
		t.Fatalf("expected weekly time selector label in body, got %q", body)
	}
	if !strings.Contains(body, "Apply") {
		t.Fatalf("expected apply button in body, got %q", body)
	}
}

func TestHandleAPIRunsDayReturnsJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, jobHistoryAPIRuns()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, jobHistoryAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}
	fixture.seedDeprecatedPhase3Links(t,
		deprecatedPhase3LinkRecord{
			IssueID:     "QE-999",
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/job-history-1",
			RowID:       "job-history-row-1",
			UpdatedAt:   "2026-03-16T12:00:00Z",
		},
		deprecatedPhase3LinkRecord{
			IssueID:     "QE-999",
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/job-history-1",
			RowID:       "job-history-row-2",
			UpdatedAt:   "2026-03-16T12:00:00Z",
		},
	)

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/run-log/day?date=2026-03-16&env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("unexpected content type: %q", got)
	}

	var payload readmodelrunlog.RunLogDayData
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.Contains(recorder.Body.String(), "\"resolved_week\"") {
		t.Fatalf("did not expect resolved_week in runs payload: %s", recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "\"failure_pattern_match\"") || !strings.Contains(body, "\"failure_pattern_summary\"") || !strings.Contains(body, "\"failed_at\"") {
		t.Fatalf("expected renamed run-log keys in body, got %q", body)
	} else if strings.Contains(body, "\"semantic_attachment\"") || strings.Contains(body, "\"semantic_rollups\"") || strings.Contains(body, "\"lanes\"") {
		t.Fatalf("did not expect stale run-log keys in body, got %q", body)
	}
	if got, want := payload.Meta.Timezone, "UTC"; got != want {
		t.Fatalf("unexpected runs payload timezone: got=%q want=%q", got, want)
	}
	if got, want := len(payload.Environments), 1; got != want {
		t.Fatalf("unexpected environment count: got=%d want=%d", got, want)
	}
	environment := payload.Environments[0]
	if got, want := environment.Summary.TotalRuns, 3; got != want {
		t.Fatalf("unexpected total runs: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.FailedRunsWithoutRawRows, 1; got != want {
		t.Fatalf("unexpected failed runs without raw rows: got=%d want=%d", got, want)
	}
	if got, want := environment.Summary.RunsUnmatchedSignatures, 0; got != want {
		t.Fatalf("unexpected unmatched signature runs: got=%d want=%d", got, want)
	}

	var multipleRun *readmodelrunlog.JobHistoryRunRow
	var unmatchedRun *readmodelrunlog.JobHistoryRunRow
	var noRawRun *readmodelrunlog.JobHistoryRunRow
	for index := range environment.Runs {
		row := &environment.Runs[index]
		switch row.Run.RunURL {
		case "https://prow.example.com/view/job-history-1":
			multipleRun = row
		case "https://prow.example.com/view/job-history-2":
			unmatchedRun = row
		case "https://prow.example.com/view/job-history-3":
			noRawRun = row
		}
	}
	if multipleRun == nil || unmatchedRun == nil || noRawRun == nil {
		t.Fatalf("expected matched, unmatched, and no-raw runs in payload")
	}
	if got, want := multipleRun.SemanticRollups.AttachmentSummary, "multiple_clustered"; got != want {
		t.Fatalf("unexpected multiple run summary: got=%q want=%q", got, want)
	}
	if got, want := multipleRun.FailedTestCount, 2; got != want {
		t.Fatalf("unexpected multiple run failed test count: got=%d want=%d", got, want)
	}
	if got, want := multipleRun.BadPRScore, 3; got != want {
		t.Fatalf("unexpected multiple run bad PR score: got=%d want=%d", got, want)
	}
	if got := len(multipleRun.BadPRReasons); got == 0 {
		t.Fatalf("expected multiple run bad PR reasons in payload")
	}
	if got, want := len(multipleRun.Lanes), 1; got != want {
		t.Fatalf("unexpected multiple run lane count: got=%d want=%d", got, want)
	}
	if got, want := multipleRun.Lanes[0], "unknown"; got != want {
		t.Fatalf("unexpected multiple run lane: got=%q want=%q", got, want)
	}
	if got, want := unmatchedRun.SemanticRollups.AttachmentSummary, "single_clustered"; got != want {
		t.Fatalf("unexpected unmatched run summary: got=%q want=%q", got, want)
	}
	if got, want := unmatchedRun.FailedTestCount, 1; got != want {
		t.Fatalf("unexpected unmatched run failed test count: got=%d want=%d", got, want)
	}
	if got, want := noRawRun.SemanticRollups.AttachmentSummary, "failed_without_raw_rows"; got != want {
		t.Fatalf("unexpected no-raw run summary: got=%q want=%q", got, want)
	}
}

func TestHandleAPIRunsDayReturnsValidationError(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/run-log/day?env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got := payload["error"]; !strings.Contains(got, "invalid date") {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestHandleRunsPageRendersHTML(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, jobHistoryAPIRuns()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, jobHistoryAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/run-log?date=2026-03-16&env=dev", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("unexpected content type: %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "CIHealth Run Log") {
		t.Fatalf("expected runs title in body, got %q", body)
	}
	if !strings.Contains(body, "Open Failure patterns for this day") {
		t.Fatalf("expected failure-pattern CTA in body, got %q", body)
	}
	if !strings.Contains(body, `id="run-log-search"`) {
		t.Fatalf("expected run-log search box in body, got %q", body)
	}
	if !strings.Contains(body, `getElementById("run-log-search")`) {
		t.Fatalf("expected run-log search script in body, got %q", body)
	}
	if !strings.Contains(body, "View JSON API") {
		t.Fatalf("expected JSON API link in body, got %q", body)
	}
	if !strings.Contains(body, "Single Day: Mar 16") {
		t.Fatalf("expected single-day time selector label in chrome, got %q", body)
	}
	if strings.Contains(body, "Generated (UTC)") {
		t.Fatalf("did not expect UTC generated label in body, got %q", body)
	}
	if !strings.Contains(body, "<th class=\"tz-header\">Time (UTC)</th>") {
		t.Fatalf("expected UTC time header in body, got %q", body)
	}
	if !strings.Contains(body, "<th>Failed at</th>") {
		t.Fatalf("expected Failed at column in body, got %q", body)
	}
	if !strings.Contains(body, "<th>Failed tests</th>") {
		t.Fatalf("expected failed tests column in body, got %q", body)
	}
	if !strings.Contains(body, "<th>Details</th>") {
		t.Fatalf("expected details column in body, got %q", body)
	}
	if !strings.Contains(body, `<select class="report-env-select" name="failed_at" onchange=`) {
		t.Fatalf("expected auto-submitting failed_at selector in run-log chrome, got %q", body)
	}
	if strings.Contains(body, "Semantic status") {
		t.Fatalf("did not expect semantic status column in body, got %q", body)
	}
	if strings.Contains(body, "Runs are listed once and enriched with semantic attachments") {
		t.Fatalf("did not expect internal implementation details text in body, got %q", body)
	}
	if strings.Contains(body, "Runs with semantic attachment") {
		t.Fatalf("did not expect semantic attachment card in body, got %q", body)
	}
	if strings.Contains(body, "Environments: <strong>") {
		t.Fatalf("did not expect environment scope summary in body, got %q", body)
	}
	if strings.Contains(body, "Semantic matches and bad-PR signals use the latest contributing stored semantic snapshot") {
		t.Fatalf("did not expect semantic explanatory text in body, got %q", body)
	}
	if strings.Contains(body, `class="cards"`) {
		t.Fatalf("did not expect run-log summary cards in body, got %q", body)
	}
	if strings.Contains(body, "Multiple failures (2)") {
		t.Fatalf("did not expect the duplicated multiple-failure summary line when category expanders render, got %q", body)
	}
	if !strings.Contains(body, "unknown (2)") {
		t.Fatalf("expected per-category expander for the multi-failure run, got %q", body)
	}
	if !strings.Contains(body, "Installer failed to reach bootstrap machine") {
		t.Fatalf("expected unmatched failure text in body, got %q", body)
	}
	if !strings.Contains(body, "Failure details unavailable") {
		t.Fatalf("expected failed-without-raw-rows summary in body, got %q", body)
	}
	if !strings.Contains(body, "Show raw failure") {
		t.Fatalf("expected raw failure toggle in body, got %q", body)
	}
	if !strings.Contains(body, ">unknown<") {
		t.Fatalf("expected lane value in body, got %q", body)
	}
	if !strings.Contains(body, `class="job-link" href="https://prow.example.com/view/job-history-1"`) {
		t.Fatalf("expected job name to be the run link, got %q", body)
	}
	if !strings.Contains(body, `class="signal-icon signal-regression"`) {
		t.Fatalf("expected regression signal icon in PR column, got %q", body)
	}
	if !strings.Contains(body, "#123 (open)") {
		t.Fatalf("expected open PR state label in body, got %q", body)
	}
	if !strings.Contains(body, "/failure-patterns?") || !strings.Contains(body, "start_date=2026-03-16") || !strings.Contains(body, "end_date=2026-03-16") {
		t.Fatalf("expected day-scoped failure-pattern link in body, got %q", body)
	}
	for _, snippet := range []string{
		`class="report-route-link" href="/">Overview</a>`,
		`class="report-route-link" href="/failure-patterns">Failure Patterns</a>`,
		`class="report-route-link active" href="/run-log">Run Log</a>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected default route navigation link %q in body, got %q", snippet, body)
		}
	}
}

func TestHandleRunsPageFailedAtFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, jobHistoryAPIRuns()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, jobHistoryAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	// The fixture failures classify as the "other" source bucket. Filtering by
	// e2e must drop every run, while filtering by other keeps them.
	e2eReq := httptest.NewRequest(http.MethodGet, "/run-log?date=2026-03-16&env=dev&failed_at=e2e", nil)
	e2eRecorder := httptest.NewRecorder()
	handler.ServeHTTP(e2eRecorder, e2eReq)
	if got, want := e2eRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, e2eRecorder.Body.String())
	}
	e2eBody := e2eRecorder.Body.String()
	if strings.Contains(e2eBody, "Installer failed to reach bootstrap machine") {
		t.Fatalf("expected failed_at=e2e to filter out other-bucket runs, got %q", e2eBody)
	}
	if !strings.Contains(e2eBody, `<option value="e2e" selected="selected">Failed at: E2E</option>`) {
		t.Fatalf("expected failed_at=e2e option to render as selected, got %q", e2eBody)
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/run-log?date=2026-03-16&env=dev&failed_at=other", nil)
	otherRecorder := httptest.NewRecorder()
	handler.ServeHTTP(otherRecorder, otherReq)
	if got, want := otherRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, otherRecorder.Body.String())
	}
	otherBody := otherRecorder.Body.String()
	if !strings.Contains(otherBody, "Installer failed to reach bootstrap machine") {
		t.Fatalf("expected failed_at=other to retain other-bucket runs, got %q", otherBody)
	}
	if !strings.Contains(otherBody, "failed_at=other") {
		t.Fatalf("expected failed_at=other to be preserved in navigation hrefs, got %q", otherBody)
	}
}

func TestHandleRunsPageDefaultsWhenDateIsOmitted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, jobHistoryAPIRuns()); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, jobHistoryAPIRawFailures()); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/run-log", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	body := recorder.Body.String()
	expectedLabel := "Single Day: " + time.Now().UTC().Format("Jan 2")
	if !strings.Contains(body, expectedLabel) {
		t.Fatalf("expected default run-log day label in body, got %q", body)
	}
	if !strings.Contains(body, `class="report-route-link active" href="/run-log">Run Log</a>`) {
		t.Fatalf("expected default run-log navigation link in body, got %q", body)
	}
}

func TestHandleAPIReviewSignalsWindowReturnsJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "2026-03-16")
	if err := store.UpsertRuns(ctx, append(reviewSignalsAPIRunsCurrent(), reviewSignalsAPIRunsPrevious()...)); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, append(reviewSignalsAPIRawFailuresCurrent(), reviewSignalsAPIRawFailuresPrevious()...)); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/review/signals/window?start_date=2026-03-10&end_date=2026-03-16", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("unexpected content type: %q", got)
	}

	var payload readmodelreview.WindowData
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := payload.Meta.StartDate, "2026-03-10"; got != want {
		t.Fatalf("unexpected start date: got=%q want=%q", got, want)
	}
	if got, want := payload.Meta.EndDate, "2026-03-16"; got != want {
		t.Fatalf("unexpected end date: got=%q want=%q", got, want)
	}
	if got, want := payload.Meta.Timezone, "UTC"; got != want {
		t.Fatalf("unexpected review-signals timezone: got=%q want=%q", got, want)
	}
	if got, want := payload.TotalSignals, 1; got != want {
		t.Fatalf("unexpected total signal count: got=%d want=%d", got, want)
	}
	if got, want := payload.SignalsByReason["new_pattern"], 1; got != want {
		t.Fatalf("unexpected new_pattern signal count: got=%d want=%d", got, want)
	}

	rowsByReason := map[string]readmodelreview.ReviewSignalRow{}
	for _, row := range payload.Rows {
		rowsByReason[row.Reason] = row
	}

	newPattern, ok := rowsByReason["new_pattern"]
	if !ok {
		t.Fatalf("missing new_pattern row: %+v", payload.Rows)
	}
	if got, want := newPattern.ProposedFailurePattern, "Installer failed to reach bootstrap machine"; got != want {
		t.Fatalf("unexpected new-pattern phrase: got=%q want=%q", got, want)
	}
}

func TestHandleAPIReviewSignalsWindowDefaultsToRollingSevenDays(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newHandlerFixture(t)
	store := fixture.openWeekStore(t, "")
	currentDate := time.Now().UTC().Format("2006-01-02")
	currentOccurredAt := currentDate + "T08:00:00Z"
	if err := store.UpsertRuns(ctx, []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/review-default-1",
			JobName:     "periodic-ci-install",
			Failed:      true,
			OccurredAt:  currentOccurredAt,
		},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if err := store.UpsertRawFailures(ctx, []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "review-default-row-1",
			RunURL:         "https://prow.example.com/view/review-default-1",
			TestName:       "should install",
			TestSuite:      "suite-b",
			SignatureID:    "sig-review-default-1",
			OccurredAt:     currentOccurredAt,
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}); err != nil {
		t.Fatalf("seed raw failures: %v", err)
	}

	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/review/signals/window", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}

	var payload readmodelreview.WindowData
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expectedStartDate := time.Now().UTC().AddDate(0, 0, -6).Format("2006-01-02")
	expectedEndDate := time.Now().UTC().Format("2006-01-02")
	if got, want := payload.Meta.StartDate, expectedStartDate; got != want {
		t.Fatalf("unexpected default rolling start date: got=%q want=%q", got, want)
	}
	if got, want := payload.Meta.EndDate, expectedEndDate; got != want {
		t.Fatalf("unexpected default rolling end date: got=%q want=%q", got, want)
	}
	if got, want := payload.TotalSignals, 1; got != want {
		t.Fatalf("unexpected total signal count: got=%d want=%d", got, want)
	}
}

func TestHandleAPIReviewSignalsWeekRouteRemoved(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/review/signals/week?week=2026-03-16", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusNotFound; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
}

func TestHandleAPIReviewSignalsWindowRejectsWeekQuery(t *testing.T) {
	t.Parallel()

	fixture := newHandlerFixture(t)
	handler, err := NewHandler(HandlerOptions{
		PostgresPool: fixture.pool,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/review/signals/window?week=2026-03-16", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("unexpected status code: got=%d want=%d body=%s", got, want, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "week is not supported") {
		t.Fatalf("expected unsupported week message, got %q", recorder.Body.String())
	}
}

type handlerFixture struct {
	pool *pgxpool.Pool
}

type deprecatedPhase3LinkRecord struct {
	IssueID     string `json:"issue_id"`
	Environment string `json:"environment"`
	RunURL      string `json:"run_url"`
	RowID       string `json:"row_id"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func newHandlerFixture(t *testing.T) *handlerFixture {
	t.Helper()

	server, err := pgtest.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Stop()
	})

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		server.User,
		server.Password,
		server.Host,
		server.Port,
		server.Database,
	)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := initdb.Initialize(context.Background(), pool); err != nil {
		t.Fatalf("initialize postgres schema: %v", err)
	}
	if err := migrations.Run(context.Background(), pool); err != nil {
		t.Fatalf("run postgres migrations: %v", err)
	}

	return &handlerFixture{pool: pool}
}

func (f *handlerFixture) seedDeprecatedPhase3Links(t *testing.T, rows ...deprecatedPhase3LinkRecord) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS cfa_phase3_links (
  environment TEXT NOT NULL,
  run_url TEXT NOT NULL,
  row_id TEXT NOT NULL,
  issue_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL,
  PRIMARY KEY (environment, run_url, row_id)
)`)
	if err != nil {
		t.Fatalf("ensure deprecated phase3 link table: %v", err)
	}
	for _, row := range rows {
		payload, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal deprecated phase3 link payload: %v", err)
		}
		_, err = f.pool.Exec(ctx, `
INSERT INTO cfa_phase3_links (environment, run_url, row_id, issue_id, updated_at, payload)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (environment, run_url, row_id)
DO UPDATE SET issue_id = EXCLUDED.issue_id, updated_at = EXCLUDED.updated_at, payload = EXCLUDED.payload
`, row.Environment, row.RunURL, row.RowID, row.IssueID, row.UpdatedAt, payload)
		if err != nil {
			t.Fatalf("insert deprecated phase3 link: %v", err)
		}
	}
}

func (f *handlerFixture) openWeekStore(t *testing.T, week string) storeWithClose {
	t.Helper()

	store, err := postgresstore.New(f.pool, postgresstore.Options{})
	if err != nil {
		t.Fatalf("create postgres store for %s: %v", week, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type storeWithClose interface {
	UpsertRuns(context.Context, []storecontracts.RunRecord) error
	UpsertRawFailures(context.Context, []storecontracts.RawFailureRecord) error
	UpsertMetricsDaily(context.Context, []storecontracts.MetricDailyRecord) error
	Close() error
}

func reviewAPIRawFailures() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "row-1",
			RunURL:         "https://prow.example.com/view/1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-linked",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        reviewAPILongRawFailureText(),
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
		{
			Environment:    "dev",
			RowID:          "row-2",
			RunURL:         "https://prow.example.com/view/2",
			TestName:       "should create nodepool",
			TestSuite:      "suite-b",
			SignatureID:    "sig-unlinked",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "CreateNodePool timeout after 45 minutes",
			NormalizedText: "createnodepool timeout after 45 minutes",
		},
	}
}

func reviewAPILongRawFailureText() string {
	return strings.Join([]string{
		`time=2026-03-16T08:00:00Z level=INFO msg="Running step." serviceGroup=Microsoft.Azure.ARO.HCP.ACM resourceGroup=management step=deploy-mce-config description="Step deploy-mce-config\n Kind: Helm\n"`,
		`time=2026-03-16T08:00:01Z level=ERROR msg="error running Helm release deployment Step, failed to deploy helm release: failed post-install: resource not ready, name: finalize-mce-config, kind: Job, status: InProgress"`,
		`time=2026-03-16T08:04:01Z level=ERROR msg="context deadline exceeded"`,
	}, "\n")
}

func reviewSignalsAPIRunsCurrent() []storecontracts.RunRecord {
	return []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/review-current-1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/review-current-2",
			JobName:     "periodic-ci-install",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
	}
}

func reviewSignalsAPIRawFailuresCurrent() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "review-current-row-1",
			RunURL:         "https://prow.example.com/view/review-current-1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-oauth",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
		{
			Environment:    "dev",
			RowID:          "review-current-row-2",
			RunURL:         "https://prow.example.com/view/review-current-2",
			TestName:       "should install",
			TestSuite:      "suite-b",
			SignatureID:    "sig-install",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}
}

func reviewSignalsAPIRunsPrevious() []storecontracts.RunRecord {
	return []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/review-prev-1",
			JobName:     "periodic-ci",
			Failed:      true,
			OccurredAt:  "2026-03-09T08:00:00Z",
		},
	}
}

func reviewSignalsAPIRawFailuresPrevious() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "review-prev-row-1",
			RunURL:         "https://prow.example.com/view/review-prev-1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-oauth-prev",
			OccurredAt:     "2026-03-09T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
	}
}

func jobHistoryAPIRuns() []storecontracts.RunRecord {
	return []storecontracts.RunRecord{
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/job-history-1",
			JobName:     "periodic-ci",
			PRNumber:    123,
			PRState:     "open",
			PRSHA:       "1111111abcdef",
			Failed:      true,
			OccurredAt:  "2026-03-16T08:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/job-history-2",
			JobName:     "periodic-ci-install",
			Failed:      true,
			OccurredAt:  "2026-03-16T09:00:00Z",
		},
		{
			Environment: "dev",
			RunURL:      "https://prow.example.com/view/job-history-3",
			JobName:     "periodic-ci-missing-raw",
			Failed:      true,
			OccurredAt:  "2026-03-16T10:00:00Z",
		},
	}
}

func jobHistoryAPIRawFailures() []storecontracts.RawFailureRecord {
	return []storecontracts.RawFailureRecord{
		{
			Environment:    "dev",
			RowID:          "job-history-row-1",
			RunURL:         "https://prow.example.com/view/job-history-1",
			TestName:       "should oauth",
			TestSuite:      "suite-a",
			SignatureID:    "sig-oauth",
			OccurredAt:     "2026-03-16T08:00:00Z",
			RawText:        "OAuth timeout while waiting for cluster operator",
			NormalizedText: "oauth timeout while waiting for cluster operator",
		},
		{
			Environment:    "dev",
			RowID:          "job-history-row-2",
			RunURL:         "https://prow.example.com/view/job-history-1",
			TestName:       "should throttle",
			TestSuite:      "suite-a",
			SignatureID:    "sig-throttle",
			OccurredAt:     "2026-03-16T08:05:00Z",
			RawText:        "API throttling while reconciling install state",
			NormalizedText: "api throttling while reconciling install state",
		},
		{
			Environment:    "dev",
			RowID:          "job-history-row-3",
			RunURL:         "https://prow.example.com/view/job-history-2",
			TestName:       "should install",
			TestSuite:      "suite-b",
			SignatureID:    "sig-unmatched",
			OccurredAt:     "2026-03-16T09:00:00Z",
			RawText:        "Installer failed to reach bootstrap machine",
			NormalizedText: "installer failed to reach bootstrap machine",
		},
	}
}
