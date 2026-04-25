package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/nsluke/TronBod/sync/fitbod"
	"github.com/nsluke/TronBod/sync/parse"
	"github.com/nsluke/TronBod/sync/server"
	"github.com/nsluke/TronBod/sync/stats"
)

type appConfig struct {
	AppID       string        `env:"FITBOD_APP_ID"`
	ClientKey   string        `env:"FITBOD_CLIENT_KEY"`
	BaseURL     string        `env:"FITBOD_BASE_URL"`
	Email       string        `env:"FITBOD_EMAIL"`
	Password    string        `env:"FITBOD_PASSWORD"`
	PollEvery   time.Duration `env:"POLL_INTERVAL" envDefault:"15m"`
	HTTPPort    int           `env:"HTTP_PORT" envDefault:"8090"`
	UAContact   string        `env:"USER_AGENT_CONTACT" envDefault:""`
	ClassesFile string        `env:"CLASSES_FILE" envDefault:"classes.yaml"`
	DataDir     string        `env:"DATA_DIR" envDefault:"data"`
}

const (
	minPollInterval     = 5 * time.Minute
	mockMinPollInterval = 5 * time.Second
)

// snapshotFetcher is what pollOnce calls to get the next batch of data.
// Real mode wires it to fitbod.Syncer.Run; mock mode reads a JSON file.
type snapshotFetcher func(context.Context) (*fitbod.Snapshot, error)

func main() {
	captureMode := flag.Bool("capture", false, "run a single sync and exit (no HTTP server)")
	mockPath := flag.String("mock", "", "path to a fixture Snapshot JSON; bypass Parse entirely")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig(*mockPath != "")
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(2)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Error("mkdir data dir", "err", err, "path", cfg.DataDir)
		os.Exit(1)
	}

	statsPath := filepath.Join(cfg.DataDir, "stats.json")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fetcher, err := buildFetcher(ctx, cfg, *mockPath, logger)
	if err != nil {
		logger.Error("setup failed", "err", err)
		os.Exit(1)
	}

	if *captureMode {
		if err := pollOnce(ctx, fetcher, nil, statsPath, logger); err != nil {
			logger.Error("capture failed", "err", err)
			os.Exit(1)
		}
		return
	}

	srv := &server.Server{StatsPath: statsPath}
	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	go func() {
		logger.Info("http server listening", "addr", addr)
		if err := srv.Run(ctx, addr); err != nil {
			logger.Error("http server", "err", err)
		}
	}()

	runLoop(ctx, fetcher, srv, statsPath, cfg.PollEvery, logger)
}

func loadConfig(mock bool) (*appConfig, error) {
	var c appConfig
	if err := env.Parse(&c); err != nil {
		return nil, err
	}
	min := minPollInterval
	if mock {
		min = mockMinPollInterval
	}
	if c.PollEvery < min {
		return nil, fmt.Errorf("POLL_INTERVAL must be >= %s (got %s)", min, c.PollEvery)
	}
	if !mock {
		if err := c.validateParse(); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (c *appConfig) validateParse() error {
	pairs := []struct {
		name, val string
	}{
		{"FITBOD_APP_ID", c.AppID},
		{"FITBOD_CLIENT_KEY", c.ClientKey},
		{"FITBOD_BASE_URL", c.BaseURL},
		{"FITBOD_EMAIL", c.Email},
		{"FITBOD_PASSWORD", c.Password},
	}
	var missing []string
	for _, p := range pairs {
		if p.val == "" {
			missing = append(missing, p.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s (or use -mock <fixture>)", strings.Join(missing, ", "))
	}
	return nil
}

func buildFetcher(ctx context.Context, cfg *appConfig, mockPath string, logger *slog.Logger) (snapshotFetcher, error) {
	if mockPath != "" {
		logger.Info("mock mode enabled", "fixture", mockPath)
		return mockFetcher(mockPath), nil
	}

	classes, err := fitbod.LoadConfig(cfg.ClassesFile)
	if err != nil {
		return nil, err
	}

	pc, err := parse.New(parse.Config{
		BaseURL:   cfg.BaseURL,
		AppID:     cfg.AppID,
		ClientKey: cfg.ClientKey,
		UserAgent: userAgent(cfg.UAContact),
		Email:     cfg.Email,
		Password:  cfg.Password,
		Session:   parse.FileSession{Path: filepath.Join(cfg.DataDir, ".session")},
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	if !pc.HasSession() {
		loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := pc.Login(loginCtx); err != nil {
			return nil, fmt.Errorf("initial login: %w", err)
		}
	}

	syncer := &fitbod.Syncer{
		Client: pc,
		Config: classes,
		RawDir: filepath.Join(cfg.DataDir, "raw"),
		Logger: logger,
	}
	return syncer.Run, nil
}

func mockFetcher(path string) snapshotFetcher {
	return func(_ context.Context) (*fitbod.Snapshot, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mock read %s: %w", path, err)
		}
		var snap fitbod.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			return nil, fmt.Errorf("mock unmarshal %s: %w", path, err)
		}
		return &snap, nil
	}
}

func userAgent(contact string) string {
	base := "TronBod-Sync/0.1 (+personal Fitbod→Tronbyt bridge)"
	if contact != "" {
		base += " contact=" + contact
	}
	return base
}

func runLoop(ctx context.Context, fetch snapshotFetcher, srv *server.Server, statsPath string, every time.Duration, logger *slog.Logger) {
	backoff := time.Second
	for {
		err := pollOnce(ctx, fetch, srv, statsPath, logger)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Error("poll failed", "err", err, "retry_in", backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		if !sleepCtx(ctx, every) {
			return
		}
	}
}

func pollOnce(ctx context.Context, fetch snapshotFetcher, srv *server.Server, statsPath string, logger *slog.Logger) error {
	start := time.Now()
	snap, err := fetch(ctx)
	if err != nil {
		return err
	}
	out := stats.Derive(snap, stats.Options{Now: time.Now().UTC()})
	if err := writeStats(statsPath, out); err != nil {
		return fmt.Errorf("write stats: %w", err)
	}
	if srv != nil {
		_ = srv.Refresh()
	}
	logger.Info("poll done",
		"workouts", len(snap.Workouts),
		"sets", len(snap.Sets),
		"exercises", len(snap.Exercises),
		"week_workouts", out.ThisWeek.Workouts,
		"week_volume", out.ThisWeek.TotalVolumeLbs,
		"streak", out.StreakWeeks,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

func writeStats(path string, s stats.Stats) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	cur *= 2
	if cur > 5*time.Minute {
		cur = 5 * time.Minute
	}
	return cur
}
