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

func NormalizeTimestampLabel(fieldName string, value string) (string, time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", time.Time{}, fmt.Errorf("%s is required", strings.TrimSpace(fieldName))
	}
	layouts := []struct {
		layout  string
		hasZone bool
	}{
		{layout: time.RFC3339Nano, hasZone: true},
		{layout: time.RFC3339, hasZone: true},
		{layout: "2006-01-02T15:04:05", hasZone: false},
		{layout: "2006-01-02T15:04", hasZone: false},
	}
	for _, candidate := range layouts {
		var (
			parsed time.Time
			err    error
		)
		if candidate.hasZone {
			parsed, err = time.Parse(candidate.layout, trimmed)
		} else {
			parsed, err = time.ParseInLocation(candidate.layout, trimmed, time.UTC)
		}
		if err != nil {
			continue
		}
		return parsed.UTC().Format(time.RFC3339), parsed.UTC(), nil
	}
	return "", time.Time{}, fmt.Errorf("%s must use RFC3339 or YYYY-MM-DDTHH:MM[:SS] format", strings.TrimSpace(fieldName))
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
	current := time.Date(start.UTC().Year(), start.UTC().Month(), start.UTC().Day(), 0, 0, 0, 0, time.UTC)
	lastIncluded := end.UTC().Add(-time.Nanosecond)
	lastDate := time.Date(lastIncluded.Year(), lastIncluded.Month(), lastIncluded.Day(), 0, 0, 0, 0, time.UTC)
	if lastDate.Before(current) {
		return nil
	}
	out := make([]string, 0, int(lastDate.Sub(current)/(24*time.Hour))+1)
	for date := current; !date.After(lastDate); date = date.AddDate(0, 0, 1) {
		out = append(out, date.Format("2006-01-02"))
	}
	return NormalizeMetricDateLabels(out)
}

func FormatDatetimeLocalValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	value = value.UTC()
	if value.Second() != 0 || value.Nanosecond() != 0 {
		return value.Format("2006-01-02T15:04:05")
	}
	return value.Format("2006-01-02T15:04")
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
