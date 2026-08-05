package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend"
	frontreadmodel "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/metrics"
	postgresstore "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres"
	postgresoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/postgres/options"
)

func NewAppCommand() (*cobra.Command, error) {
	listen := "127.0.0.1:8082"
	defaultWeek := ""
	historyWeeks := 4
	failurePatternsEngine := "inline"
	preparedWindowCacheEnabled := true
	preparedWindowCacheEnvelopeDuration := frontreadmodel.DefaultPreparedWindowCacheEnvelopeDuration
	preparedWindowCacheRefreshInterval := frontreadmodel.DefaultPreparedWindowCacheRefreshInterval
	preparedWindowCacheTTL := frontreadmodel.DefaultPreparedWindowCacheTTL
	metricsEnabled := true
	metricsRollingWindowDays := 7
	metricsRefreshInterval := 60 * time.Second
	metricsEnvironments := ""
	servePostgresRaw := postgresoptions.DefaultCLIOptions()

	cmd := &cobra.Command{
		Use:           "app",
		Short:         "Run the unified app.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			postgresCompleted, err := completePostgresForCommand(cmd.Context(), servePostgresRaw)
			if err != nil {
				return err
			}
			defer postgresCompleted.Cleanup()

			var metricsHandler http.Handler
			if metricsEnabled {
				envs := splitAndTrim(metricsEnvironments)
				if len(envs) == 0 {
					cmd.PrintErrln("Warning: --metrics.enabled is true but --metrics.environments is empty; skipping /metrics endpoint")
				} else {
					store, err := postgresstore.New(postgresCompleted.Connection, postgresstore.Options{})
					if err != nil {
						return fmt.Errorf("create store for metrics collector: %w", err)
					}
					defer store.Close()

					collector, err := metrics.NewCollector(metrics.CollectorOptions{
						Logger:            klog.NewKlogr().WithName("metrics-collector"),
						Store:             store,
						Environments:      envs,
						RollingWindowDays: metricsRollingWindowDays,
						RefreshInterval:   metricsRefreshInterval,
					})
					if err != nil {
						return fmt.Errorf("create metrics collector: %w", err)
					}

					registry := prometheus.NewRegistry()
					registry.MustRegister(collector)
					metricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})

					go collector.Start(cmd.Context())
				}
			}

			handler, err := frontend.NewHandler(frontend.HandlerOptions{
				Context:               cmd.Context(),
				DefaultWeek:           defaultWeek,
				HistoryHorizonWeeks:   historyWeeks,
				FailurePatternsEngine: failurePatternsEngine,
				PostgresPool:          postgresCompleted.Connection,
				MetricsHandler:        metricsHandler,
				PreparedWindowCache: frontreadmodel.PreparedWindowCacheOptions{
					Enabled:          preparedWindowCacheEnabled,
					EnvelopeDuration: preparedWindowCacheEnvelopeDuration,
					RefreshInterval:  preparedWindowCacheRefreshInterval,
					TTL:              preparedWindowCacheTTL,
				},
			})
			if err != nil {
				return err
			}

			listenAddress := strings.TrimSpace(listen)
			if listenAddress == "" {
				listenAddress = "127.0.0.1:8082"
			}
			listener, err := net.Listen("tcp", listenAddress)
			if err != nil {
				return fmt.Errorf("listen on %q: %w", listenAddress, err)
			}
			defer func() {
				_ = listener.Close()
			}()

			server := &http.Server{Handler: handler}
			cmd.Printf(
				"Serving unified app at %s (Ctrl+C to stop)\n",
				siteRunURLFromListenAddress(listenAddress),
			)

			go func() {
				<-cmd.Context().Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownCtx)
			}()
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve unified app: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&listen, "app.listen", listen, "listen address for unified app (host:port)")
	cmd.Flags().StringVar(&defaultWeek, "week", defaultWeek, "default week to open when no week query is provided (YYYY-MM-DD)")
	cmd.Flags().IntVar(&historyWeeks, "history.weeks", historyWeeks, "number of prior calendar weeks used for failure-pattern history scoring")
	cmd.Flags().StringVar(&failurePatternsEngine, "app.failure-patterns-engine", failurePatternsEngine, "failure-patterns engine to use (inline only)")
	cmd.Flags().BoolVar(&preparedWindowCacheEnabled, "app.failure-patterns-cache", preparedWindowCacheEnabled, "enable the app-local prepared failure-pattern window cache")
	cmd.Flags().DurationVar(&preparedWindowCacheEnvelopeDuration, "app.failure-patterns-cache-window", preparedWindowCacheEnvelopeDuration, "prepared window cache envelope duration (for example 840h for 35 days)")
	cmd.Flags().DurationVar(&preparedWindowCacheRefreshInterval, "app.failure-patterns-cache-refresh", preparedWindowCacheRefreshInterval, "refresh interval for the prepared window cache")
	cmd.Flags().DurationVar(&preparedWindowCacheTTL, "app.failure-patterns-cache-ttl", preparedWindowCacheTTL, "maximum age for serving prepared window cache entries before on-demand fallback")
	cmd.Flags().BoolVar(&metricsEnabled, "metrics.enabled", metricsEnabled, "enable Prometheus /metrics endpoint")
	cmd.Flags().IntVar(&metricsRollingWindowDays, "metrics.rolling-window-days", metricsRollingWindowDays, "number of days in the rolling window for metric aggregation")
	cmd.Flags().DurationVar(&metricsRefreshInterval, "metrics.refresh-interval", metricsRefreshInterval, "how often to refresh the metrics cache from PostgreSQL")
	cmd.Flags().StringVar(&metricsEnvironments, "metrics.environments", metricsEnvironments, "comma-separated list of environments to expose metrics for")
	if err := postgresoptions.BindOptions(servePostgresRaw, cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

func siteRunURLFromListenAddress(listenAddress string) string {
	trimmed := strings.TrimSpace(listenAddress)
	if trimmed == "" {
		return "http://127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "http://" + trimmed
	}
	normalizedHost := strings.Trim(strings.TrimSpace(host), "[]")
	if normalizedHost == "" || normalizedHost == "0.0.0.0" || normalizedHost == "::" {
		normalizedHost = "localhost"
	}
	return fmt.Sprintf("http://%s:%s", normalizedHost, port)
}

func splitAndTrim(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
