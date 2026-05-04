package window

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type WeekWindowResolver interface {
	ResolveWeekWindow(ctx context.Context, requestedWeek string, now time.Time) (WeekWindow, error)
}

func Resolve(ctx context.Context, resolver WeekWindowResolver, request Request) (Scope, error) {
	if resolver == nil {
		return Scope{}, fmt.Errorf("resolver is required")
	}

	startDate := strings.TrimSpace(request.StartDate)
	endDate := strings.TrimSpace(request.EndDate)
	startAt := strings.TrimSpace(request.StartAt)
	endAt := strings.TrimSpace(request.EndAt)
	week := strings.TrimSpace(request.Week)
	date := strings.TrimSpace(request.Date)

	switch {
	case startAt != "" || endAt != "":
		if startAt == "" || endAt == "" {
			return Scope{}, fmt.Errorf("start_at and end_at must both be set")
		}
		if startDate != "" || endDate != "" || week != "" || date != "" {
			return Scope{}, fmt.Errorf("start_at/end_at cannot be combined with start_date, end_date, date, or week")
		}
	case date != "":
		startDate = date
		endDate = date
	case startDate != "" || endDate != "":
		if startDate == "" || endDate == "" {
			return Scope{}, fmt.Errorf("start_date and end_date must both be set")
		}
	case week != "":
		startDate, endDate = CompatWeekDateRange(week)
		if startDate == "" || endDate == "" {
			return Scope{}, fmt.Errorf("invalid week %q", week)
		}
	default:
		switch request.DefaultMode {
		case DefaultLatestWeek:
			window, err := resolver.ResolveWeekWindow(ctx, "", request.Now)
			if err != nil {
				return Scope{}, err
			}
			startDate, endDate = CompatWeekDateRange(window.CurrentWeek)
		case DefaultRolling:
			now := request.Now
			if now.IsZero() {
				now = time.Now().UTC()
			}
			rollingDays := request.RollingDays
			if rollingDays <= 0 {
				rollingDays = 7
			}
			endValue := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
			startValue := endValue.AddDate(0, 0, -(rollingDays - 1))
			startDate = startValue.Format("2006-01-02")
			endDate = endValue.Format("2006-01-02")
		case DefaultLatestSprint:
			now := request.Now
			if now.IsZero() {
				now = time.Now().UTC()
			}
			sprintStart, sprintEnd := SprintWindowForDate(now)
			startDate = sprintStart.Format("2006-01-02")
			endDate = sprintEnd.Format("2006-01-02")
		default:
			return Scope{}, fmt.Errorf("start_date and end_date are required")
		}
	}

	var (
		startLabel     string
		endLabel       string
		startAtLabel   string
		endAtLabel     string
		startTime      time.Time
		endTime        time.Time
		hasExactBounds bool
		err            error
	)

	if startAt != "" || endAt != "" {
		startAtLabel, startTime, err = NormalizeTimestampLabel("start_at", startAt)
		if err != nil {
			return Scope{}, fmt.Errorf("invalid start_at: %w", err)
		}
		endAtLabel, endTime, err = NormalizeTimestampLabel("end_at", endAt)
		if err != nil {
			return Scope{}, fmt.Errorf("invalid end_at: %w", err)
		}
		if !startTime.Before(endTime) {
			return Scope{}, fmt.Errorf("end_at %s must be after start_at %s", endAtLabel, startAtLabel)
		}
		startLabel = startTime.UTC().Format("2006-01-02")
		endLabel = endTime.UTC().Add(-time.Nanosecond).Format("2006-01-02")
		hasExactBounds = true
	} else {
		var startValue time.Time
		var endValue time.Time
		startLabel, startValue, err = NormalizeDateLabel(startDate)
		if err != nil {
			return Scope{}, fmt.Errorf("invalid start_date: %w", err)
		}
		endLabel, endValue, err = NormalizeDateLabel(endDate)
		if err != nil {
			return Scope{}, fmt.Errorf("invalid end_date: %w", err)
		}
		if endValue.Before(startValue) {
			return Scope{}, fmt.Errorf("end_date %s must be on or after start_date %s", endLabel, startLabel)
		}
		startTime = time.Date(startValue.Year(), startValue.Month(), startValue.Day(), 0, 0, 0, 0, time.UTC)
		endInclusive := time.Date(endValue.Year(), endValue.Month(), endValue.Day(), 0, 0, 0, 0, time.UTC)
		endTime = endInclusive.AddDate(0, 0, 1).UTC()
		startAtLabel = startTime.Format(time.RFC3339)
		endAtLabel = endTime.Format(time.RFC3339)
	}
	anchorWeek := ""
	if weekStart := WeekStartForDate(endTime.Add(-time.Nanosecond)); !weekStart.IsZero() {
		anchorWeek = weekStart.Format("2006-01-02")
	}

	return Scope{
		StartDate:      startLabel,
		EndDate:        endLabel,
		StartAt:        startAtLabel,
		EndAt:          endAtLabel,
		StartTime:      startTime,
		EndTime:        endTime,
		DateLabels:     MetricDateLabelsFromWindow(startTime, endTime),
		AnchorWeek:     anchorWeek,
		HasExactBounds: hasExactBounds,
	}, nil
}
