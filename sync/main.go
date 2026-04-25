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
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/nsluke/TronBod/sync/fitbod"
	"github.com/nsluke/TronBod/sync/parse"
	"github.com/nsluke/TronBod/sync/server"
	"github.com/nsluke/TronBod/sync/stats"
)

type appConfig struct {
	AppID       string        `env:"FITBOD_APP_ID,required"`
	ClientKey   string        `env:"FITBOD_CLIENT_KEY,required"`
	BaseURL     string        `env:"FITBOD_BASE_URL,required"`
	Email       string        `env:"FITBOD_EMAIL,required"`
	Password    string        `env:"FITBOD_PASSWORD,required"`
	PollEvery   time.Duration `env:"POLL_INTERVAL" envDefault:"15m"`
	HTTPPort    int           `env:"HTTP_PORT" envDefault:"8090"`
	UAContact   string        `env:"USER_AGENT_CONTACT" envDefault:""`
	ClassesFile string        `env:"CLASSES_FILE" envDefault:"classes.yaml"`
	DataDir     string        `env:"DATA_DIR" envDefault:"data"`
}

const minPollInterval = 5 * time.Minute

func main() {
	captureMode := flag.Bool("capture", false, "run a single sync, dump raw JSON, then exit (no HTTP server)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(2)
	}

	classes, err := fitbod.LoadConfig(cfg.ClassesFile)
	if err != nil {
		logger.Error("classes config", "err", err)
		os.Exit(2)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Error("mkdir data dir", "err", err, "path", cfg.DataDir)
		os.Exit(1)
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
		logger.Error("parse client", "err", err)
		os.Exit(1)
	}

	syncer := &fitbod.Syncer{
		Client: pc,
		Config: classes,
		RawDir: filepath.Join(cfg.DataDir, "raw"),
		Logger: logger,
	}

	statsPath := filepath.Join(cfg.DataDir, "stats.json")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if !pc.HasSession() {
		loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := pc.Login(loginCtx); err != nil {
			cancel()
			logger.Error("initial login failed", "err", err)
			os.Exit(1)
		}
		cancel()
	}

	if *captureMode {
		if err := pollOnce(ctx, syncer, nil, statsPath, logger); err != nil {
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

	runLoop(ctx, syncer, srv, statsPath, cfg.PollEvery, logger)
}

func loadConfig() (*appConfig, error) {
	var c appConfig
	if err := env.Parse(&c); err != nil {
		return nil, err
	}
	if c.PollEvery < minPollInterval {
		return nil, fmt.Errorf("POLL_INTERVAL must be >= %s (got %s)", minPollInterval, c.PollEvery)
	}
	return &c, nil
}

func userAgent(contact string) string {
	base := "TronBod-Sync/0.1 (+personal Fitbod→Tronbyt bridge)"
	if contact != "" {
		base += " contact=" + contact
	}
	return base
}

func runLoop(ctx context.Context, syncer *fitbod.Syncer, srv *server.Server, statsPath string, every time.Duration, logger *slog.Logger) {
	backoff := time.Second
	for {
		err := pollOnce(ctx, syncer, srv, statsPath, logger)
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

func pollOnce(ctx context.Context, syncer *fitbod.Syncer, srv *server.Server, statsPath string, logger *slog.Logger) error {
	start := time.Now()
	snap, err := syncer.Run(ctx)
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
