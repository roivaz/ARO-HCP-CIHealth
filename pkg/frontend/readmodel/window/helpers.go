package window

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func NormalizeDateLabel(value string) (string, time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", time.Time{}, fmt.Errorf("date query parameter is required (YYYY-MM-DD)")
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil || parsed.Format("2006-01-02") != trimmed {
		return "", time.Time{}, fmt.Errorf("date must use YYYY-MM-DD format")
	}
	return parsed.UTC().Format("2006-01-02"), parsed.UTC(), nil
}

func NormalizeWeekLabel(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("week is required")
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil || parsed.Format("2006-01-02") != trimmed {
		return "", fmt.Errorf("week must use YYYY-MM-DD format")
	}
	if parsed.Weekday() != time.Monday {
		return "", fmt.Errorf("week must start on Monday")
	}
	return parsed.UTC().Format("2006-01-02"), nil
}

func WeekStartForDate(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	date := time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return date.AddDate(0, 0, -int((date.Weekday()+6)%7)).UTC()
}

func CompatWeekDateRange(week string) (string, string) {
	normalizedWeek, err := NormalizeWeekLabel(week)
	if err != nil {
		return "", ""
	}
	startDate, err := time.Parse("2006-01-02", normalizedWeek)
	if err != nil {
		return "", ""
	}
	startDate = startDate.UTC()
	return startDate.Format("2006-01-02"), startDate.AddDate(0, 0, 6).Format("2006-01-02")
}

func CompatWeekTimeRange(week string) (time.Time, time.Time, error) {
	normalizedWeek, err := NormalizeWeekLabel(week)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if normalizedWeek == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("week is required")
	}
	startDate, err := time.Parse("2006-01-02", normalizedWeek)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	startDate = startDate.UTC()
	return startDate, startDate.AddDate(0, 0, 7).UTC(), nil
}

func MetricDateLabelsFromWindow(start time.Time, end time.Time) []string {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil
	}
	out := make([]string, 0, int(end.Sub(start)/(24*time.Hour)))
	for date := start.UTC(); date.Before(end.UTC()); date = date.AddDate(0, 0, 1) {
		out = append(out, date.Format("2006-01-02"))
	}
	return NormalizeMetricDateLabels(out)
}

func NormalizeMetricDateLabels(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	return SortedStringSet(set)
}

func SortedStringSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
