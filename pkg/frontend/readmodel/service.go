package readmodel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ci-failure-atlas/pkg/failurepatterns"
	readmodelwindow "ci-failure-atlas/pkg/frontend/readmodel/window"
	storecontracts "ci-failure-atlas/pkg/store/contracts"
	postgresstore "ci-failure-atlas/pkg/store/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultHistoryWeeks = 4

const (
	FailurePatternsEngineInline = "inline"
)

var (
	ErrNoAvailableWeeks = errors.New("no available weeks found in postgres fact store")
)

type Options struct {
	DefaultWeek           string
	HistoryHorizonWeeks   int
	FailurePatternsEngine string
	PostgresPool          *pgxpool.Pool
}

type Service struct {
	defaultWeek  string
	historyWeeks int
	postgresPool *pgxpool.Pool
}

func New(opts Options) (*Service, error) {
	if opts.PostgresPool == nil {
		return nil, fmt.Errorf("postgres pool is required")
	}
	defaultWeek, err := postgresstore.NormalizeWeek(opts.DefaultWeek)
	if err != nil {
		return nil, fmt.Errorf("invalid default week: %w", err)
	}
	historyWeeks := opts.HistoryHorizonWeeks
	if historyWeeks <= 0 {
		historyWeeks = DefaultHistoryWeeks
	}
	if _, err := normalizeFailurePatternsEngine(opts.FailurePatternsEngine); err != nil {
		return nil, err
	}
	return &Service{
		defaultWeek:  defaultWeek,
		historyWeeks: historyWeeks,
		postgresPool: opts.PostgresPool,
	}, nil
}

func (s *Service) DefaultWeek() string {
	if s == nil {
		return ""
	}
	return s.defaultWeek
}

func (s *Service) HistoryHorizonWeeks() int {
	if s == nil {
		return 0
	}
	return s.historyWeeks
}

func (s *Service) FailurePatternsEngine() string {
	return FailurePatternsEngineInline
}

func (s *Service) DiscoverAvailableWeeks(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("service is required")
	}
	weeks, err := s.discoverAvailableWeekStarts(ctx)
	if err != nil {
		return nil, err
	}
	if len(weeks) == 0 {
		return nil, ErrNoAvailableWeeks
	}
	return weeks, nil
}

func (s *Service) discoverAvailableWeekStarts(ctx context.Context) ([]string, error) {
	store, err := s.OpenStore()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = store.Close()
	}()

	runDates, err := store.ListRunDates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list run dates from postgres: %w", err)
	}
	metricDates, err := store.ListMetricDates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list metric dates from postgres: %w", err)
	}

	weekSet := map[string]struct{}{}
	for _, dateLabel := range append(append([]string(nil), runDates...), metricDates...) {
		_, dateValue, parseErr := readmodelwindow.NormalizeDateLabel(dateLabel)
		if parseErr != nil {
			continue
		}
		weekSet[readmodelwindow.WeekStartForDate(dateValue).Format("2006-01-02")] = struct{}{}
	}
	if len(weekSet) == 0 {
		return nil, nil
	}
	weeks := make([]string, 0, len(weekSet))
	for week := range weekSet {
		weeks = append(weeks, week)
	}
	sort.Strings(weeks)
	return weeks, nil
}

func (s *Service) ResolveWeekWindow(ctx context.Context, requestedWeek string, now time.Time) (readmodelwindow.WeekWindow, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	weeks, err := s.DiscoverAvailableWeeks(ctx)
	if err != nil {
		return readmodelwindow.WeekWindow{}, err
	}
	week, previousWeek, nextWeek, index := readmodelwindow.ResolveWindow(weeks, strings.TrimSpace(requestedWeek), s.defaultWeek, now.UTC())
	if strings.TrimSpace(week) == "" {
		return readmodelwindow.WeekWindow{}, ErrNoAvailableWeeks
	}
	return readmodelwindow.WeekWindow{
		Weeks:        append([]string(nil), weeks...),
		CurrentWeek:  week,
		PreviousWeek: previousWeek,
		NextWeek:     nextWeek,
		Index:        index,
	}, nil
}

func (s *Service) OpenStore() (storecontracts.Store, error) {
	if s == nil {
		return nil, fmt.Errorf("service is required")
	}
	store, err := postgresstore.New(s.postgresPool, postgresstore.Options{})
	if err != nil {
		return nil, fmt.Errorf("open postgres store: %w", err)
	}
	return store, nil
}

func (s *Service) BuildHistoryResolver(ctx context.Context, endTime time.Time) (failurepatterns.PresenceResolver, error) {
	if s == nil {
		return nil, fmt.Errorf("service is required")
	}
	store, err := s.OpenStore()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = store.Close()
	}()
	return failurepatterns.BuildPresenceResolver(ctx, failurepatterns.BuildPresenceOptions{
		Store:         store,
		EndTime:       endTime.UTC(),
		LookbackWeeks: s.historyWeeks,
	})
}

func normalizeFailurePatternsEngine(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", FailurePatternsEngineInline:
		return FailurePatternsEngineInline, nil
	default:
		return "", fmt.Errorf("invalid failure patterns engine %q (expected inline)", strings.TrimSpace(value))
	}
}
