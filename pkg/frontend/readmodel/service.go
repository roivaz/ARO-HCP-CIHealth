package readmodel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ci-failure-atlas/pkg/failurepatterns"
	failurepatterncontracts "ci-failure-atlas/pkg/failurepatterns/contracts"
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
	ErrWeekNotFound     = errors.New("week not found in postgres fact store")
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

type WeekWindow struct {
	Weeks        []string `json:"weeks,omitempty"`
	CurrentWeek  string   `json:"current_week"`
	PreviousWeek string   `json:"previous_week,omitempty"`
	NextWeek     string   `json:"next_week,omitempty"`
	Index        int      `json:"-"`
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

func (s *Service) DiscoverSemanticWeeks(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("service is required")
	}
	weeks, err := s.discoverAllSemanticWeeks(ctx)
	if err != nil {
		return nil, err
	}
	if len(weeks) == 0 {
		return nil, ErrNoAvailableWeeks
	}
	return weeks, nil
}

func (s *Service) discoverAllSemanticWeeks(ctx context.Context) ([]string, error) {
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
		dateLabel, dateValue, parseErr := normalizeDateLabel(dateLabel)
		if parseErr != nil {
			continue
		}
		_ = dateLabel
		weekSet[weekStartForDate(dateValue).Format("2006-01-02")] = struct{}{}
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

func (s *Service) semanticWeekSchemaVersion(ctx context.Context, week string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("service is required")
	}
	if _, err := s.ensureWeekExists(ctx, week); err != nil {
		return "", err
	}
	return failurepatterncontracts.CurrentSchemaVersion, nil
}

func (s *Service) explainUnavailableWeek(ctx context.Context, week string) error {
	rawWeeks, err := s.discoverAllSemanticWeeks(ctx)
	if err != nil {
		return err
	}
	index := sort.SearchStrings(rawWeeks, week)
	if index >= len(rawWeeks) || rawWeeks[index] != week {
		return fmt.Errorf("%w: %s", ErrWeekNotFound, week)
	}
	return fmt.Errorf("%w: %s", ErrWeekNotFound, week)
}

func (s *Service) ResolveWeekWindow(ctx context.Context, requestedWeek string, now time.Time) (WeekWindow, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	weeks, err := s.DiscoverSemanticWeeks(ctx)
	if err != nil {
		return WeekWindow{}, err
	}
	week, previousWeek, nextWeek, index := ResolveWindow(weeks, strings.TrimSpace(requestedWeek), s.defaultWeek, now.UTC())
	if strings.TrimSpace(week) == "" {
		return WeekWindow{}, ErrNoAvailableWeeks
	}
	return WeekWindow{
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

func (s *Service) BuildHistoryResolver(ctx context.Context, week string) (failurepatterns.PresenceResolver, error) {
	return s.BuildHistoryResolverForWeek(ctx, week, "")
}

func (s *Service) BuildHistoryResolverForWeek(
	ctx context.Context,
	week string,
	currentSchemaVersion string,
) (failurepatterns.PresenceResolver, error) {
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
		AnchorWeek:    strings.TrimSpace(week),
		LookbackWeeks: s.historyWeeks,
	})
}

func (s *Service) ensureWeekExists(ctx context.Context, week string) (string, error) {
	normalizedWeek, err := postgresstore.NormalizeWeek(week)
	if err != nil {
		return "", fmt.Errorf("invalid semantic week %q: %w", strings.TrimSpace(week), err)
	}
	if normalizedWeek == "" {
		return "", fmt.Errorf("week is required")
	}
	weeks, err := s.DiscoverSemanticWeeks(ctx)
	if err != nil {
		return "", err
	}
	index := sort.SearchStrings(weeks, normalizedWeek)
	if index >= len(weeks) || weeks[index] != normalizedWeek {
		return "", s.explainUnavailableWeek(ctx, normalizedWeek)
	}
	return normalizedWeek, nil
}

func normalizeFailurePatternsEngine(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", FailurePatternsEngineInline:
		return FailurePatternsEngineInline, nil
	default:
		return "", fmt.Errorf("invalid failure patterns engine %q (expected inline)", strings.TrimSpace(value))
	}
}
