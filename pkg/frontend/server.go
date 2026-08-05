package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	frontfailurepatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/failurepatterns"
	frontservice "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel"
	readmodelpatterns "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/patterns"
	readmodelreports "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/reports"
	readmodelreview "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/review"
	readmodelrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/runlog"
	readmodelwindow "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/window"
	reportweekly "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/report"
	frontrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/runlog"
	frontui "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/ui"
	sourcelanes "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/lanes"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HandlerOptions struct {
	Context               context.Context
	DefaultWeek           string
	HistoryHorizonWeeks   int
	FailurePatternsEngine string
	PostgresPool          *pgxpool.Pool
	PreparedWindowCache   frontservice.PreparedWindowCacheOptions
	MetricsHandler        http.Handler
}

type handler struct {
	service      *frontservice.Service
	postgresPool *pgxpool.Pool
}

type reportPageMode string

const (
	reportPageModeReport  reportPageMode = "report"
	reportPageModeRolling reportPageMode = "rolling"
	reportPageModeSprint  reportPageMode = "sprint"
)

func NewHandler(opts HandlerOptions) (http.Handler, error) {
	service, err := frontservice.New(frontservice.Options{
		DefaultWeek:           opts.DefaultWeek,
		HistoryHorizonWeeks:   opts.HistoryHorizonWeeks,
		FailurePatternsEngine: opts.FailurePatternsEngine,
		PostgresPool:          opts.PostgresPool,
		PreparedWindowCache:   opts.PreparedWindowCache,
	})
	if err != nil {
		return nil, err
	}
	service.StartPreparedWindowCache(opts.Context)
	h := &handler{
		service:      service,
		postgresPool: opts.PostgresPool,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)
	mux.HandleFunc("/api/run-log/day", h.handleAPIRunsDay)
	mux.HandleFunc("/api/failure-patterns/window", h.handleAPIFailurePatterns)
	mux.HandleFunc("/api/review/signals/window", h.handleAPIReviewSignalsWindow)
	mux.HandleFunc("/report", h.handleReportPage)
	mux.HandleFunc("/run-log", h.handleRunsPage)
	mux.HandleFunc("/failure-patterns", h.handleFailurePatternsPage)
	mux.HandleFunc("/global", h.handleLegacyGlobalRedirect)
	if opts.MetricsHandler != nil {
		mux.Handle("/metrics", opts.MetricsHandler)
	}
	return mux, nil
}

func (h *handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil || strings.TrimSpace(r.URL.Path) != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	href, err := h.currentRollingReportHref(r.Context())
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	http.Redirect(w, r, href, http.StatusFound)
}

func (h *handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (h *handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.postgresPool == nil {
		http.Error(w, "postgres pool not configured", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.postgresPool.Ping(ctx); err != nil {
		http.Error(w, fmt.Sprintf("postgres not ready: %v", err), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (h *handler) handleFailurePatternsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	windowedQuery, err := h.resolveFailurePatternsPageQuery(r.Context(), failurePatternsQueryFromRequest(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reportHTML, err := h.generateFailurePatternsReport(r.Context(), windowedQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(reportHTML))
}

func (h *handler) handleRunsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, err := h.resolveRunLogPageQuery(r.Context(), runLogDayQueryFromRequest(r))
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	reportHTML, err := h.generateDayRunHistoryPage(r.Context(), query)
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(reportHTML))
}

func (h *handler) handleLegacyGlobalRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Redirect(w, r, viewHref("/failure-patterns", strings.TrimSpace(r.URL.Query().Get("week"))), http.StatusMovedPermanently)
}

func (h *handler) handleReportPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, mode, err := h.resolveReportPageQuery(
		r.Context(),
		reportQueryFromRequest(r),
		normalizeReportPageMode(strings.TrimSpace(r.URL.Query().Get("mode"))),
	)
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	reportHTML, err := h.generateReportPage(r.Context(), query, mode)
	if err != nil {
		http.Error(w, fmt.Sprintf("generate report: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(reportHTML))
}

func (h *handler) handleAPIFailurePatterns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	response, err := readmodelpatterns.BuildWindowData(r.Context(), h.service, failurePatternsQueryFromRequest(r))
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		writeJSONError(w, statusCode, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) handleAPIReviewSignalsWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	query, err := reviewSignalsWindowQueryFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	response, err := readmodelreview.BuildWindow(r.Context(), h.service, query)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) handleAPIRunsDay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	response, err := readmodelrunlog.BuildDay(r.Context(), h.service, runLogDayQueryFromRequest(r))
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, frontservice.ErrNoAvailableWeeks) {
			statusCode = http.StatusNotFound
		}
		writeJSONError(w, statusCode, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) generateFailurePatternsReport(ctx context.Context, query readmodelpatterns.FailurePatternsQuery) (string, error) {
	data, err := readmodelpatterns.BuildWindowData(ctx, h.service, query)
	if err != nil {
		return "", err
	}
	scope, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate: query.StartDate,
		EndDate:   query.EndDate,
		StartAt:   query.StartAt,
		EndAt:     query.EndAt,
	})
	if err != nil {
		return "", err
	}
	timeMode := failurePatternsTimeSelectorMode(query.Mode, scope)
	shiftDuration := failurePatternsTimeSelectorShift(timeMode, scope)
	return frontfailurepatterns.RenderHTML(data, frontfailurepatterns.PageOptions{
		Query: query,
		Chrome: frontui.ReportChromeOptions{
			CurrentView:         frontui.ReportViewFailurePatterns,
			OverviewHref:        "/",
			FailurePatternsHref: "/failure-patterns",
			RunLogHref:          "/run-log",
			FilterFormAction:    "/failure-patterns",
			TimeSelector: frontui.TimeSelectorOptions{
				Mode:            timeMode,
				Label:           formatFailurePatternsTimeSelectorLabel(timeMode, scope),
				PreviousHref:    h.shiftedFailurePatternsHref(ctx, scope, -shiftDuration, query.Environments, query.FailedAt, timeMode),
				NextHref:        h.shiftedFailurePatternsHref(ctx, scope, shiftDuration, query.Environments, query.FailedAt, timeMode),
				MenuLinks:       h.failurePatternsTimeSelectorLinks(ctx, scope, query.Environments, query.FailedAt, timeMode),
				ShowRangeInputs: true,
				RangeStartAt:    readmodelwindow.FormatDatetimeLocalValue(scope.StartTime),
				RangeEndAt:      readmodelwindow.FormatDatetimeLocalValue(scope.EndTime),
			},
			Environment: frontui.EnvironmentControlOptions{
				Value:      chromeEnvironmentValue(query.Environments),
				AutoSubmit: true,
			},
			FailedAt: frontui.FailedAtControlOptions{
				Value:      chromeFailedAtValue(query.FailedAt),
				AutoSubmit: true,
			},
			JSONAPIHref: failurePatternsWindowHref(
				"/api/failure-patterns/window",
				"",
				scope.StartDate,
				scope.EndDate,
				query.StartAt,
				query.EndAt,
				query.Environments,
				query.FailedAt,
				"",
			),
			ResetHref: "/failure-patterns",
			ShowApply: true,
		},
	}), nil
}

func (h *handler) generateDayRunHistoryPage(ctx context.Context, query readmodelrunlog.RunLogDayQuery) (string, error) {
	data, err := readmodelrunlog.BuildDay(ctx, h.service, query)
	if err != nil {
		return "", err
	}
	environments := query.Environments
	failedAt := query.FailedAt
	return frontrunlog.RenderHTML(data, frontrunlog.PageOptions{
		Query:               query,
		FailurePatternsHref: failurePatternsHref("/failure-patterns", "", data.Meta.Date, data.Meta.Date, environments, failedAt, ""),
		Chrome: frontui.ReportChromeOptions{
			CurrentView:         frontui.ReportViewRunLog,
			OverviewHref:        "/",
			FailurePatternsHref: "/failure-patterns",
			RunLogHref:          "/run-log",
			FilterFormAction:    "/run-log",
			TimeSelector: frontui.TimeSelectorOptions{
				Mode:          frontui.TimeSelectorModeDay,
				Label:         formatTimeSelectorLabel(frontui.TimeSelectorModeDay, data.Meta.Date, data.Meta.Date),
				PreviousHref:  h.shiftedRunLogHref(ctx, data.Meta.Date, -1, environments, failedAt),
				NextHref:      h.shiftedRunLogHref(ctx, data.Meta.Date, 1, environments, failedAt),
				ShowDateInput: true,
				DateValue:     data.Meta.Date,
				AutoSubmit:    true,
			},
			Environment: frontui.EnvironmentControlOptions{
				Value:      chromeEnvironmentValue(environments),
				AutoSubmit: true,
			},
			FailedAt: frontui.FailedAtControlOptions{
				Value:      chromeFailedAtValue(failedAt),
				AutoSubmit: true,
			},
			JSONAPIHref: runLogDayHref("/api/run-log/day", data.Meta.Date, "", environments, failedAt),
		},
	}), nil
}

func (h *handler) generateReportPage(
	ctx context.Context,
	query readmodelreports.ReportQuery,
	mode reportPageMode,
) (string, error) {
	data, err := readmodelreports.BuildWindowData(ctx, h.service, query)
	if err != nil {
		return "", err
	}
	scope, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate: query.StartDate,
		EndDate:   query.EndDate,
	})
	if err != nil {
		return "", err
	}
	timeMode := reportTimeSelectorMode(mode, scope.StartDate, scope.EndDate)
	shiftDays := timeSelectorShiftDays(timeMode, scope.StartDate, scope.EndDate)

	opts := reportweekly.DefaultOptions()
	opts.RunLogDayBasePath = "/run-log"
	opts.Chrome = frontui.ReportChromeOptions{
		CurrentView:                reportChromeView(mode),
		OverviewHref:               "/",
		FailurePatternsHref:        "/failure-patterns",
		ContextFailurePatternsHref: failurePatternsHref("/failure-patterns", "", scope.StartDate, scope.EndDate, nil, nil, failurePatternsModeQueryValue(timeMode)),
		RunLogHref:                 "/run-log",
		TimeSelector: frontui.TimeSelectorOptions{
			Mode:         timeMode,
			Label:        formatTimeSelectorLabel(timeMode, scope.StartDate, scope.EndDate),
			PreviousHref: h.shiftedReportHref(ctx, scope.StartDate, scope.EndDate, -shiftDays, reportPageModeForTimeSelectorMode(timeMode)),
			NextHref:     h.shiftedReportHref(ctx, scope.StartDate, scope.EndDate, shiftDays, reportPageModeForTimeSelectorMode(timeMode)),
			MenuLinks:    h.reportTimeSelectorLinks(ctx, scope.StartDate, scope.EndDate, timeMode),
		},
		Environment: frontui.EnvironmentControlOptions{
			Disabled: true,
		},
	}
	return reportweekly.RenderHTML(data, opts), nil
}

func viewHref(path string, week string) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ""
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	q := url.Values{}
	if strings.TrimSpace(week) != "" {
		q.Set("week", strings.TrimSpace(week))
	}
	if encoded := q.Encode(); encoded != "" {
		return trimmedPath + "?" + encoded
	}
	return trimmedPath
}

func navigationHref(path string, week string) string {
	if strings.TrimSpace(week) == "" {
		return ""
	}
	return viewHref(path, week)
}

func failurePatternsWindowHref(
	path string,
	week string,
	startDate string,
	endDate string,
	startAt string,
	endAt string,
	environments []string,
	failedAt []string,
	mode string,
) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ""
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	q := url.Values{}
	trimmedStartAt := strings.TrimSpace(startAt)
	trimmedEndAt := strings.TrimSpace(endAt)
	if trimmedStartAt != "" && trimmedEndAt != "" {
		q.Set("start_at", trimmedStartAt)
		q.Set("end_at", trimmedEndAt)
	} else {
		if strings.TrimSpace(week) != "" {
			q.Set("week", strings.TrimSpace(week))
		}
		if strings.TrimSpace(startDate) != "" {
			q.Set("start_date", strings.TrimSpace(startDate))
		}
		if strings.TrimSpace(endDate) != "" {
			q.Set("end_date", strings.TrimSpace(endDate))
		}
	}
	if normalizedMode := normalizeFailurePatternsMode(mode); normalizedMode != "" {
		q.Set("mode", normalizedMode)
	}
	for _, environment := range normalizedQueryEnvironments(environments) {
		q.Add("env", environment)
	}
	for _, source := range normalizedQueryFailedAt(failedAt) {
		q.Add("failed_at", source)
	}
	if encoded := q.Encode(); encoded != "" {
		return trimmedPath + "?" + encoded
	}
	return trimmedPath
}

func failurePatternsHref(path string, week string, startDate string, endDate string, environments []string, failedAt []string, mode string) string {
	return failurePatternsWindowHref(path, week, startDate, endDate, "", "", environments, failedAt, mode)
}

func hasFailurePatternsQuery(query readmodelpatterns.FailurePatternsQuery) bool {
	return strings.TrimSpace(query.StartDate) != "" ||
		strings.TrimSpace(query.EndDate) != "" ||
		strings.TrimSpace(query.StartAt) != "" ||
		strings.TrimSpace(query.EndAt) != ""
}

func failurePatternsQueryFromRequest(r *http.Request) readmodelpatterns.FailurePatternsQuery {
	if r == nil {
		return readmodelpatterns.FailurePatternsQuery{}
	}
	return readmodelpatterns.FailurePatternsQuery{
		StartDate:    strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:      strings.TrimSpace(r.URL.Query().Get("end_date")),
		StartAt:      strings.TrimSpace(r.URL.Query().Get("start_at")),
		EndAt:        strings.TrimSpace(r.URL.Query().Get("end_at")),
		Week:         strings.TrimSpace(r.URL.Query().Get("week")),
		Mode:         normalizeFailurePatternsMode(r.URL.Query().Get("mode")),
		Environments: parseListQueryValues(r.URL.Query()["env"]),
		FailedAt:     parseListQueryValues(r.URL.Query()["failed_at"]),
	}
}

func reportQueryFromRequest(r *http.Request) readmodelreports.ReportQuery {
	if r == nil {
		return readmodelreports.ReportQuery{}
	}
	return readmodelreports.ReportQuery{
		StartDate: strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:   strings.TrimSpace(r.URL.Query().Get("end_date")),
		Week:      strings.TrimSpace(r.URL.Query().Get("week")),
	}
}

func reviewSignalsWindowQueryFromRequest(r *http.Request) (readmodelreview.WindowQuery, error) {
	if r == nil || r.URL == nil {
		return readmodelreview.WindowQuery{}, nil
	}
	if trimmed := strings.TrimSpace(r.URL.Query().Get("week")); trimmed != "" {
		return readmodelreview.WindowQuery{}, fmt.Errorf("week is not supported for review signals; use start_date and end_date")
	}
	if trimmed := strings.TrimSpace(r.URL.Query().Get("date")); trimmed != "" {
		return readmodelreview.WindowQuery{}, fmt.Errorf("date is not supported for review signals; use start_date and end_date")
	}
	if trimmed := strings.TrimSpace(r.URL.Query().Get("start_at")); trimmed != "" {
		return readmodelreview.WindowQuery{}, fmt.Errorf("start_at is not supported for review signals; use start_date and end_date")
	}
	if trimmed := strings.TrimSpace(r.URL.Query().Get("end_at")); trimmed != "" {
		return readmodelreview.WindowQuery{}, fmt.Errorf("end_at is not supported for review signals; use start_date and end_date")
	}
	return readmodelreview.WindowQuery{
		StartDate: strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:   strings.TrimSpace(r.URL.Query().Get("end_date")),
	}, nil
}

func (h *handler) resolveFailurePatternsPageQuery(
	ctx context.Context,
	query readmodelpatterns.FailurePatternsQuery,
) (readmodelpatterns.FailurePatternsQuery, error) {
	hasExplicitWindow := hasFailurePatternsQuery(query) || strings.TrimSpace(query.Week) != ""
	requestedMode := normalizeFailurePatternsMode(query.Mode)
	defaultMode := readmodelwindow.DefaultRolling
	switch requestedMode {
	case string(reportPageModeSprint):
		defaultMode = readmodelwindow.DefaultLatestSprint
	case string(reportPageModeRolling), "":
		defaultMode = readmodelwindow.DefaultRolling
	}
	window, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate:   query.StartDate,
		EndDate:     query.EndDate,
		StartAt:     query.StartAt,
		EndAt:       query.EndAt,
		Week:        query.Week,
		DefaultMode: defaultMode,
		RollingDays: 7,
	})
	if err != nil {
		if !hasExplicitWindow && defaultMode == readmodelwindow.DefaultRolling {
			if weekWindow, weekErr := h.service.ResolveWeekWindow(ctx, "", time.Time{}); weekErr == nil {
				query.Week = ""
				query.StartDate, query.EndDate = semanticWeekDateRange(weekWindow.CurrentWeek)
				query.Mode = string(reportPageModeRolling)
				return query, nil
			}
		}
		return readmodelpatterns.FailurePatternsQuery{}, err
	}
	query.Week = ""
	if window.HasExactBounds {
		query.StartDate = ""
		query.EndDate = ""
		query.StartAt = window.StartAt
		query.EndAt = window.EndAt
	} else {
		query.StartDate = window.StartDate
		query.EndDate = window.EndDate
		query.StartAt = ""
		query.EndAt = ""
	}
	if !hasExplicitWindow && requestedMode == "" && defaultMode == readmodelwindow.DefaultRolling {
		query.Mode = string(reportPageModeRolling)
	}
	return query, nil
}

func (h *handler) resolveRunLogPageQuery(
	ctx context.Context,
	query readmodelrunlog.RunLogDayQuery,
) (readmodelrunlog.RunLogDayQuery, error) {
	if strings.TrimSpace(query.Date) != "" {
		return query, nil
	}

	if strings.TrimSpace(query.Week) != "" {
		window, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
			Week: query.Week,
		})
		if err != nil {
			return readmodelrunlog.RunLogDayQuery{}, err
		}
		query.Date = runLogDefaultDate(window.EndDate)
		return query, nil
	}

	query.Date = time.Now().UTC().Format("2006-01-02")
	return query, nil
}

func (h *handler) resolveReportPageQuery(
	ctx context.Context,
	query readmodelreports.ReportQuery,
	mode reportPageMode,
) (readmodelreports.ReportQuery, reportPageMode, error) {
	defaultMode := readmodelwindow.DefaultLatestWeek
	switch mode {
	case reportPageModeRolling:
		defaultMode = readmodelwindow.DefaultRolling
	case reportPageModeSprint:
		defaultMode = readmodelwindow.DefaultLatestSprint
	}
	window, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate:   query.StartDate,
		EndDate:     query.EndDate,
		Week:        query.Week,
		DefaultMode: defaultMode,
		RollingDays: 7,
	})
	if err != nil {
		return readmodelreports.ReportQuery{}, "", err
	}
	query.Week = ""
	query.StartDate = window.StartDate
	query.EndDate = window.EndDate
	return query, mode, nil
}

func reportHref(path string, startDate string, endDate string, mode reportPageMode) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ""
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	q := url.Values{}
	if strings.TrimSpace(startDate) != "" {
		q.Set("start_date", strings.TrimSpace(startDate))
	}
	if strings.TrimSpace(endDate) != "" {
		q.Set("end_date", strings.TrimSpace(endDate))
	}
	switch normalizeReportPageMode(string(mode)) {
	case reportPageModeRolling:
		q.Set("mode", string(reportPageModeRolling))
	case reportPageModeSprint:
		q.Set("mode", string(reportPageModeSprint))
	}
	if encoded := q.Encode(); encoded != "" {
		return trimmedPath + "?" + encoded
	}
	return trimmedPath
}

func normalizeReportPageMode(value string) reportPageMode {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case string(reportPageModeRolling):
		return reportPageModeRolling
	case string(reportPageModeSprint):
		return reportPageModeSprint
	default:
		return reportPageModeReport
	}
}

func runLogDefaultDate(endDate string) string {
	today := time.Now().UTC().Format("2006-01-02")
	if strings.TrimSpace(endDate) == "" || endDate > today {
		return today
	}
	return endDate
}

func reportChromeView(mode reportPageMode) frontui.ReportView {
	switch mode {
	case reportPageModeRolling:
		return frontui.ReportViewRolling
	case reportPageModeSprint:
		return frontui.ReportViewSprint
	default:
		return frontui.ReportViewReport
	}
}

func normalizeFailurePatternsMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(reportPageModeRolling):
		return string(reportPageModeRolling)
	case string(reportPageModeSprint):
		return string(reportPageModeSprint)
	default:
		return ""
	}
}

func reportTimeSelectorMode(mode reportPageMode, startDate string, endDate string) frontui.TimeSelectorMode {
	switch mode {
	case reportPageModeRolling:
		return frontui.TimeSelectorModeRolling
	case reportPageModeSprint:
		return frontui.TimeSelectorModeSprint
	default:
		return inferredTimeSelectorMode(startDate, endDate)
	}
}

func failurePatternsTimeSelectorMode(mode string, scope readmodelwindow.Scope) frontui.TimeSelectorMode {
	switch normalizeFailurePatternsMode(mode) {
	case string(reportPageModeRolling):
		return frontui.TimeSelectorModeRolling
	case string(reportPageModeSprint):
		return frontui.TimeSelectorModeSprint
	default:
		return inferredFailurePatternsTimeSelectorMode(scope)
	}
}

func inferredFailurePatternsTimeSelectorMode(scope readmodelwindow.Scope) frontui.TimeSelectorMode {
	switch {
	case isSingleDayScope(scope):
		return frontui.TimeSelectorModeDay
	case isSprintScope(scope):
		return frontui.TimeSelectorModeSprint
	case isWeeklyScope(scope):
		return frontui.TimeSelectorModeWeekly
	default:
		return frontui.TimeSelectorModeCustom
	}
}

func inferredTimeSelectorMode(startDate string, endDate string) frontui.TimeSelectorMode {
	switch {
	case isSingleDayWindow(startDate, endDate):
		return frontui.TimeSelectorModeDay
	case isSprintDateWindow(startDate, endDate):
		return frontui.TimeSelectorModeSprint
	case isWeeklyDateWindow(startDate, endDate):
		return frontui.TimeSelectorModeWeekly
	default:
		return frontui.TimeSelectorModeCustom
	}
}

func timeSelectorShiftDays(mode frontui.TimeSelectorMode, startDate string, endDate string) int {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return 0
	case frontui.TimeSelectorModeWeekly:
		return 7
	case frontui.TimeSelectorModeSprint:
		return readmodelwindow.SprintDurationDays()
	case frontui.TimeSelectorModeDay:
		return 1
	default:
		return timeWindowSpanDays(startDate, endDate)
	}
}

func failurePatternsTimeSelectorShift(mode frontui.TimeSelectorMode, scope readmodelwindow.Scope) time.Duration {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return 0
	case frontui.TimeSelectorModeWeekly:
		return 7 * 24 * time.Hour
	case frontui.TimeSelectorModeSprint:
		return time.Duration(readmodelwindow.SprintDurationDays()) * 24 * time.Hour
	case frontui.TimeSelectorModeDay:
		return 24 * time.Hour
	default:
		if scope.StartTime.IsZero() || scope.EndTime.IsZero() || !scope.StartTime.Before(scope.EndTime) {
			return 0
		}
		return scope.EndTime.Sub(scope.StartTime)
	}
}

func reportPageModeForTimeSelectorMode(mode frontui.TimeSelectorMode) reportPageMode {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return reportPageModeRolling
	case frontui.TimeSelectorModeSprint:
		return reportPageModeSprint
	default:
		return reportPageModeReport
	}
}

func failurePatternsModeQueryValue(mode frontui.TimeSelectorMode) string {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return string(reportPageModeRolling)
	case frontui.TimeSelectorModeSprint:
		return string(reportPageModeSprint)
	default:
		return ""
	}
}

func formatTimeSelectorLabel(mode frontui.TimeSelectorMode, startDate string, endDate string) string {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return "Last 7 Days"
	case frontui.TimeSelectorModeWeekly:
		return "Weekly: " + formatCompactDateRange(startDate, endDate)
	case frontui.TimeSelectorModeSprint:
		return "Sprint: " + formatCompactDateRange(startDate, endDate)
	case frontui.TimeSelectorModeDay:
		return "Single Day: " + formatCompactDateLabel(startDate)
	default:
		return "Custom: " + formatCompactDateRange(startDate, endDate)
	}
}

func formatFailurePatternsTimeSelectorLabel(mode frontui.TimeSelectorMode, scope readmodelwindow.Scope) string {
	switch mode {
	case frontui.TimeSelectorModeRolling:
		return "Last 7 Days"
	case frontui.TimeSelectorModeWeekly:
		return "Weekly: " + formatCompactDateRange(scope.StartDate, scope.EndDate)
	case frontui.TimeSelectorModeSprint:
		return "Sprint: " + formatCompactDateRange(scope.StartDate, scope.EndDate)
	case frontui.TimeSelectorModeDay:
		return "Single Day: " + formatCompactDateLabel(scope.StartDate)
	default:
		return "Custom: " + formatCompactTimestampRange(scope.StartTime, scope.EndTime)
	}
}

func formatCompactDateRange(startDate string, endDate string) string {
	startValue, okStart := parseChromeDate(startDate)
	endValue, okEnd := parseChromeDate(endDate)
	if !okStart || !okEnd {
		trimmedStart := strings.TrimSpace(startDate)
		trimmedEnd := strings.TrimSpace(endDate)
		switch {
		case trimmedStart == "" && trimmedEnd == "":
			return ""
		case trimmedStart == trimmedEnd:
			return trimmedStart
		default:
			return trimmedStart + " - " + trimmedEnd
		}
	}
	if startValue.Year() == endValue.Year() {
		return startValue.Format("Jan 2") + " - " + endValue.Format("Jan 2")
	}
	return startValue.Format("Jan 2, 2006") + " - " + endValue.Format("Jan 2, 2006")
}

func formatCompactDateLabel(date string) string {
	dateValue, ok := parseChromeDate(date)
	if !ok {
		return strings.TrimSpace(date)
	}
	return dateValue.Format("Jan 2")
}

func formatCompactTimestampRange(start time.Time, end time.Time) string {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return ""
	}
	start = start.UTC()
	end = end.UTC()
	if start.Year() == end.Year() {
		return start.Format("Jan 2 15:04") + " - " + end.Format("Jan 2 15:04 MST")
	}
	return start.Format("Jan 2 2006 15:04") + " - " + end.Format("Jan 2 2006 15:04 MST")
}

func parseChromeDate(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func startOfUTCDay(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func isSingleDayScope(scope readmodelwindow.Scope) bool {
	if scope.StartTime.IsZero() || scope.EndTime.IsZero() || !scope.StartTime.Before(scope.EndTime) {
		return false
	}
	startDay := startOfUTCDay(scope.StartTime)
	return scope.StartTime.Equal(startDay) && scope.EndTime.Equal(startDay.AddDate(0, 0, 1))
}

func isWeeklyScope(scope readmodelwindow.Scope) bool {
	if scope.StartTime.IsZero() || scope.EndTime.IsZero() || !scope.StartTime.Before(scope.EndTime) {
		return false
	}
	startDay := startOfUTCDay(scope.StartTime)
	return scope.StartTime.Equal(startDay) &&
		scope.EndTime.Equal(startDay.AddDate(0, 0, 7)) &&
		startDay.Weekday() == time.Monday
}

func isSprintScope(scope readmodelwindow.Scope) bool {
	if scope.StartTime.IsZero() || scope.EndTime.IsZero() || !scope.StartTime.Before(scope.EndTime) {
		return false
	}
	startDay := startOfUTCDay(scope.StartTime)
	sprintStart, sprintEnd := readmodelwindow.SprintWindowForDate(startDay)
	return scope.StartTime.Equal(sprintStart.UTC()) && scope.EndTime.Equal(sprintEnd.AddDate(0, 0, 1).UTC())
}

func timeWindowSpanDays(startDate string, endDate string) int {
	startValue, okStart := parseChromeDate(startDate)
	endValue, okEnd := parseChromeDate(endDate)
	if !okStart || !okEnd || endValue.Before(startValue) {
		return 0
	}
	return int(endValue.Sub(startValue)/(24*time.Hour)) + 1
}

func isWeeklyDateWindow(startDate string, endDate string) bool {
	startValue, okStart := parseChromeDate(startDate)
	endValue, okEnd := parseChromeDate(endDate)
	if !okStart || !okEnd {
		return false
	}
	if timeWindowSpanDays(startDate, endDate) != 7 {
		return false
	}
	return startValue.Weekday() == time.Monday && endValue.Equal(startValue.AddDate(0, 0, 6))
}

func isSprintDateWindow(startDate string, endDate string) bool {
	startValue, okStart := parseChromeDate(startDate)
	endValue, okEnd := parseChromeDate(endDate)
	if !okStart || !okEnd {
		return false
	}
	sprintStart, sprintEnd := readmodelwindow.SprintWindowForDate(startValue)
	return startValue.Equal(sprintStart.UTC()) &&
		endValue.Equal(sprintEnd.UTC()) &&
		timeWindowSpanDays(startDate, endDate) == readmodelwindow.SprintDurationDays()
}

func rollingWindowEndingOn(endDate string, days int) (string, string, bool) {
	endValue, ok := parseChromeDate(endDate)
	if !ok || days <= 0 {
		return "", "", false
	}
	startValue := endValue.AddDate(0, 0, -(days - 1))
	return startValue.Format("2006-01-02"), endValue.Format("2006-01-02"), true
}

func weeklyWindowContaining(date string) (string, string, bool) {
	dateValue, ok := parseChromeDate(date)
	if !ok {
		return "", "", false
	}
	startValue := dateValue.AddDate(0, 0, -int((dateValue.Weekday()+6)%7))
	endValue := startValue.AddDate(0, 0, 6)
	return startValue.Format("2006-01-02"), endValue.Format("2006-01-02"), true
}

func sprintWindowContaining(date string) (string, string, bool) {
	dateValue, ok := parseChromeDate(date)
	if !ok {
		return "", "", false
	}
	startValue, endValue := readmodelwindow.SprintWindowForDate(dateValue)
	return startValue.Format("2006-01-02"), endValue.Format("2006-01-02"), true
}

func chromeEnvironmentValue(environments []string) string {
	normalized := normalizedQueryEnvironments(environments)
	if len(normalized) == 1 {
		return normalized[0]
	}
	return ""
}

func chromeFailedAtValue(failedAt []string) string {
	normalized := normalizedQueryFailedAt(failedAt)
	if len(normalized) == 1 {
		return normalized[0]
	}
	return ""
}

func runLogDateForWindow(startDate string, endDate string) string {
	if isSingleDayWindow(startDate, endDate) {
		return strings.TrimSpace(startDate)
	}
	if strings.TrimSpace(endDate) != "" {
		return runLogDefaultDate(endDate)
	}
	return runLogDefaultDate(startDate)
}

func timeSelectorAnchorDate(startDate string, endDate string) string {
	if strings.TrimSpace(endDate) != "" {
		return strings.TrimSpace(endDate)
	}
	return strings.TrimSpace(startDate)
}

func (h *handler) reportTimeSelectorLinks(
	ctx context.Context,
	startDate string,
	endDate string,
	activeMode frontui.TimeSelectorMode,
) []frontui.ChromeLink {
	anchorDate := timeSelectorAnchorDate(startDate, endDate)
	links := make([]frontui.ChromeLink, 0, 3)
	if href, err := h.currentRollingReportHref(ctx); err == nil && strings.TrimSpace(href) != "" {
		links = append(links, frontui.ChromeLink{
			Label:  "Last 7 Days",
			Href:   href,
			Active: activeMode == frontui.TimeSelectorModeRolling,
		})
	}
	if weekStart, weekEnd, ok := weeklyWindowContaining(anchorDate); ok {
		if href := h.validatedReportWindowHref(ctx, weekStart, weekEnd, reportPageModeReport); href != "" {
			links = append(links, frontui.ChromeLink{
				Label:  "Weekly: " + formatCompactDateRange(weekStart, weekEnd),
				Href:   href,
				Active: activeMode == frontui.TimeSelectorModeWeekly,
			})
		}
	}
	if sprintStart, sprintEnd, ok := sprintWindowContaining(anchorDate); ok {
		if href := h.validatedReportWindowHref(ctx, sprintStart, sprintEnd, reportPageModeSprint); href != "" {
			links = append(links, frontui.ChromeLink{
				Label:  "Sprint: " + formatCompactDateRange(sprintStart, sprintEnd),
				Href:   href,
				Active: activeMode == frontui.TimeSelectorModeSprint,
			})
		}
	}
	return links
}

func (h *handler) failurePatternsTimeSelectorLinks(
	ctx context.Context,
	scope readmodelwindow.Scope,
	environments []string,
	failedAt []string,
	activeMode frontui.TimeSelectorMode,
) []frontui.ChromeLink {
	anchorDate := timeSelectorAnchorDate(scope.StartDate, scope.EndDate)
	links := make([]frontui.ChromeLink, 0, 4)
	if rollingStart, rollingEnd, ok := rollingWindowEndingOn(anchorDate, 7); ok {
		if href := h.validatedFailurePatternsWindowHref(ctx, rollingStart, rollingEnd, "", "", environments, failedAt, frontui.TimeSelectorModeRolling); href != "" {
			links = append(links, frontui.ChromeLink{
				Label:  "Last 7 Days",
				Href:   href,
				Active: activeMode == frontui.TimeSelectorModeRolling,
			})
		}
	}
	if weekStart, weekEnd, ok := weeklyWindowContaining(anchorDate); ok {
		if href := h.validatedFailurePatternsWindowHref(ctx, weekStart, weekEnd, "", "", environments, failedAt, frontui.TimeSelectorModeWeekly); href != "" {
			links = append(links, frontui.ChromeLink{
				Label:  "Weekly: " + formatCompactDateRange(weekStart, weekEnd),
				Href:   href,
				Active: activeMode == frontui.TimeSelectorModeWeekly,
			})
		}
	}
	if sprintStart, sprintEnd, ok := sprintWindowContaining(anchorDate); ok {
		if href := h.validatedFailurePatternsWindowHref(ctx, sprintStart, sprintEnd, "", "", environments, failedAt, frontui.TimeSelectorModeSprint); href != "" {
			links = append(links, frontui.ChromeLink{
				Label:  "Sprint: " + formatCompactDateRange(sprintStart, sprintEnd),
				Href:   href,
				Active: activeMode == frontui.TimeSelectorModeSprint,
			})
		}
	}
	if href := h.validatedFailurePatternsWindowHref(ctx, anchorDate, anchorDate, "", "", environments, failedAt, frontui.TimeSelectorModeDay); href != "" {
		links = append(links, frontui.ChromeLink{
			Label:  "Single Day: " + formatCompactDateLabel(anchorDate),
			Href:   href,
			Active: activeMode == frontui.TimeSelectorModeDay,
		})
	}
	return links
}

func (h *handler) validatedReportWindowHref(
	ctx context.Context,
	startDate string,
	endDate string,
	mode reportPageMode,
) string {
	if strings.TrimSpace(startDate) == "" || strings.TrimSpace(endDate) == "" {
		return ""
	}
	if _, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate: startDate,
		EndDate:   endDate,
	}); err != nil {
		return ""
	}
	return reportHref("/report", startDate, endDate, mode)
}

func (h *handler) validatedFailurePatternsWindowHref(
	ctx context.Context,
	startDate string,
	endDate string,
	startAt string,
	endAt string,
	environments []string,
	failedAt []string,
	mode frontui.TimeSelectorMode,
) string {
	request := readmodelwindow.Request{
		StartDate: startDate,
		EndDate:   endDate,
		StartAt:   startAt,
		EndAt:     endAt,
	}
	if strings.TrimSpace(startAt) == "" && strings.TrimSpace(endAt) == "" &&
		(strings.TrimSpace(startDate) == "" || strings.TrimSpace(endDate) == "") {
		return ""
	}
	if _, err := readmodelwindow.Resolve(ctx, h.service, request); err != nil {
		return ""
	}
	return failurePatternsWindowHref(
		"/failure-patterns",
		"",
		startDate,
		endDate,
		startAt,
		endAt,
		environments,
		failedAt,
		failurePatternsModeQueryValue(mode),
	)
}

func (h *handler) currentSprintReportHref(_ context.Context) (string, error) {
	start, end := readmodelwindow.SprintWindowForDate(time.Now().UTC())
	return reportHref("/report", start.Format("2006-01-02"), end.Format("2006-01-02"), reportPageModeSprint), nil
}

func (h *handler) currentRollingReportHref(ctx context.Context) (string, error) {
	window, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		DefaultMode: readmodelwindow.DefaultRolling,
		RollingDays: 7,
	})
	if err == nil {
		return reportHref("/report", window.StartDate, window.EndDate, reportPageModeRolling), nil
	}
	weekWindow, weekErr := h.service.ResolveWeekWindow(ctx, "", time.Time{})
	if weekErr != nil {
		return "", err
	}
	fallbackStart, fallbackEnd := semanticWeekDateRange(weekWindow.CurrentWeek)
	if fallbackStart == "" || fallbackEnd == "" {
		return "", err
	}
	return reportHref("/report", fallbackStart, fallbackEnd, reportPageModeRolling), nil
}

func (h *handler) shiftedReportHref(
	ctx context.Context,
	startDate string,
	endDate string,
	days int,
	mode reportPageMode,
) string {
	if mode == reportPageModeRolling {
		return ""
	}
	targetStart, targetEnd, err := shiftDateWindow(startDate, endDate, days)
	if err != nil {
		return ""
	}
	if _, err := readmodelwindow.Resolve(ctx, h.service, readmodelwindow.Request{
		StartDate: targetStart,
		EndDate:   targetEnd,
	}); err != nil {
		return ""
	}
	return reportHref("/report", targetStart, targetEnd, mode)
}

func (h *handler) shiftedFailurePatternsHref(
	ctx context.Context,
	scope readmodelwindow.Scope,
	shift time.Duration,
	environments []string,
	failedAt []string,
	mode frontui.TimeSelectorMode,
) string {
	if mode == frontui.TimeSelectorModeRolling || shift == 0 {
		return ""
	}
	if scope.StartTime.IsZero() || scope.EndTime.IsZero() || !scope.StartTime.Before(scope.EndTime) {
		return ""
	}
	targetStart := scope.StartTime.Add(shift).UTC()
	targetEnd := scope.EndTime.Add(shift).UTC()
	if !targetStart.Before(targetEnd) {
		return ""
	}
	targetStartDate := targetStart.Format("2006-01-02")
	targetEndDate := targetEnd.Add(-time.Nanosecond).Format("2006-01-02")
	targetStartAt := ""
	targetEndAt := ""
	if scope.HasExactBounds {
		targetStartAt = targetStart.Format(time.RFC3339)
		targetEndAt = targetEnd.Format(time.RFC3339)
	}
	return h.validatedFailurePatternsWindowHref(
		ctx,
		targetStartDate,
		targetEndDate,
		targetStartAt,
		targetEndAt,
		environments,
		failedAt,
		mode,
	)
}

func (h *handler) shiftedRunLogHref(
	ctx context.Context,
	currentDate string,
	days int,
	environments []string,
	failedAt []string,
) string {
	dateValue, err := parseDateInputValue("date", currentDate)
	if err != nil {
		return ""
	}
	targetDate := dateValue.AddDate(0, 0, days).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")
	if targetDate > today {
		return ""
	}
	return runLogDayHref("/run-log", targetDate, "", environments, failedAt)
}

func shiftDateWindow(startDate string, endDate string, days int) (string, string, error) {
	startValue, err := parseDateInputValue("start_date", startDate)
	if err != nil {
		return "", "", err
	}
	endValue, err := parseDateInputValue("end_date", endDate)
	if err != nil {
		return "", "", err
	}
	return startValue.AddDate(0, 0, days).Format("2006-01-02"), endValue.AddDate(0, 0, days).Format("2006-01-02"), nil
}

func semanticWeekDateRange(week string) (string, string) {
	startDate, err := time.Parse("2006-01-02", strings.TrimSpace(week))
	if err != nil {
		return "", ""
	}
	startDate = startDate.UTC()
	return startDate.Format("2006-01-02"), startDate.AddDate(0, 0, 6).Format("2006-01-02")
}

func anchorWeekDateRange(anchorWeek string, fallbackStart string, fallbackEnd string) (string, string) {
	start, end := semanticWeekDateRange(anchorWeek)
	if start != "" && end != "" {
		return start, end
	}
	return fallbackStart, fallbackEnd
}

func parseDateInputValue(fieldName string, value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("%s is required (YYYY-MM-DD)", strings.TrimSpace(fieldName))
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil || parsed.Format("2006-01-02") != trimmed {
		return time.Time{}, fmt.Errorf("%s must use YYYY-MM-DD format", strings.TrimSpace(fieldName))
	}
	return parsed.UTC(), nil
}

func shiftedFailurePatternsHref(
	path string,
	targetWeek string,
	currentWeek string,
	startDate string,
	endDate string,
	environments []string,
) string {
	trimmedTargetWeek := strings.TrimSpace(targetWeek)
	if trimmedTargetWeek == "" {
		return ""
	}
	targetStart, targetEnd := semanticWeekDateRange(trimmedTargetWeek)
	if targetStart == "" || targetEnd == "" {
		return viewHref(path, trimmedTargetWeek)
	}
	currentWeekStart, errCurrentWeek := time.Parse("2006-01-02", strings.TrimSpace(currentWeek))
	currentStartDate, errCurrentStart := time.Parse("2006-01-02", strings.TrimSpace(startDate))
	currentEndDate, errCurrentEnd := time.Parse("2006-01-02", strings.TrimSpace(endDate))
	targetWeekStart, errTargetWeek := time.Parse("2006-01-02", trimmedTargetWeek)
	if errCurrentWeek != nil || errCurrentStart != nil || errCurrentEnd != nil || errTargetWeek != nil {
		return failurePatternsHref(path, trimmedTargetWeek, targetStart, targetEnd, environments, nil, "")
	}
	startOffset := int(currentStartDate.UTC().Sub(currentWeekStart.UTC()) / (24 * time.Hour))
	endOffset := int(currentEndDate.UTC().Sub(currentWeekStart.UTC()) / (24 * time.Hour))
	if startOffset < 0 || endOffset < startOffset || endOffset > 6 {
		return failurePatternsHref(path, trimmedTargetWeek, targetStart, targetEnd, environments, nil, "")
	}
	shiftedStart := targetWeekStart.UTC().AddDate(0, 0, startOffset).Format("2006-01-02")
	shiftedEnd := targetWeekStart.UTC().AddDate(0, 0, endOffset).Format("2006-01-02")
	return failurePatternsHref(path, trimmedTargetWeek, shiftedStart, shiftedEnd, environments, nil, "")
}

func runLogDayHref(path string, date string, week string, environments []string, failedAt []string) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ""
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	q := url.Values{}
	if strings.TrimSpace(date) != "" {
		q.Set("date", strings.TrimSpace(date))
	}
	if strings.TrimSpace(week) != "" {
		q.Set("week", strings.TrimSpace(week))
	}
	for _, environment := range normalizedQueryEnvironments(environments) {
		q.Add("env", environment)
	}
	for _, source := range normalizedQueryFailedAt(failedAt) {
		q.Add("failed_at", source)
	}
	if encoded := q.Encode(); encoded != "" {
		return trimmedPath + "?" + encoded
	}
	return trimmedPath
}

func shiftedDayRunHistoryHref(
	path string,
	targetWeek string,
	currentDate string,
	currentWeek string,
	environments []string,
) string {
	trimmedTargetWeek := strings.TrimSpace(targetWeek)
	if trimmedTargetWeek == "" {
		return ""
	}
	targetWeekStart, errTargetWeek := time.Parse("2006-01-02", trimmedTargetWeek)
	currentWeekStart, errCurrentWeek := time.Parse("2006-01-02", strings.TrimSpace(currentWeek))
	currentDateValue, errCurrentDate := time.Parse("2006-01-02", strings.TrimSpace(currentDate))
	if errTargetWeek != nil || errCurrentWeek != nil || errCurrentDate != nil {
		return runLogDayHref(path, trimmedTargetWeek, trimmedTargetWeek, environments, nil)
	}
	offset := int(currentDateValue.UTC().Sub(currentWeekStart.UTC()) / (24 * time.Hour))
	if offset < 0 || offset > 6 {
		return runLogDayHref(path, trimmedTargetWeek, trimmedTargetWeek, environments, nil)
	}
	targetDate := targetWeekStart.UTC().AddDate(0, 0, offset).Format("2006-01-02")
	return runLogDayHref(path, targetDate, trimmedTargetWeek, environments, nil)
}

func isSingleDayWindow(startDate string, endDate string) bool {
	trimmedStartDate := strings.TrimSpace(startDate)
	return trimmedStartDate != "" && trimmedStartDate == strings.TrimSpace(endDate)
}

func normalizedQueryEnvironments(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizedQueryFailedAt(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	order := map[string]int{}
	for index, source := range sourcelanes.FilterableSources() {
		allowed[source] = struct{}{}
		order[source] = index
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; !ok {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return order[out[i]] < order[out[j]]
	})
	return out
}

func runLogDayQueryFromRequest(r *http.Request) readmodelrunlog.RunLogDayQuery {
	if r == nil {
		return readmodelrunlog.RunLogDayQuery{}
	}
	return readmodelrunlog.RunLogDayQuery{
		Date:         strings.TrimSpace(r.URL.Query().Get("date")),
		Week:         strings.TrimSpace(r.URL.Query().Get("week")),
		Environments: parseListQueryValues(r.URL.Query()["env"]),
		FailedAt:     normalizedQueryFailedAt(parseListQueryValues(r.URL.Query()["failed_at"])),
	}
}

func parseListQueryValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, statusCode int, err error) {
	writeJSON(w, statusCode, map[string]any{
		"error": strings.TrimSpace(err.Error()),
	})
}
