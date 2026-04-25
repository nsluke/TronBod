package fitbod

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nsluke/TronBod/sync/parse"
)

type Syncer struct {
	Client *parse.Client
	Config *Config
	RawDir string // where to dump raw responses; "" disables
	Logger *slog.Logger
}

// Run pulls one snapshot's worth of data from each configured class.
func (s *Syncer) Run(ctx context.Context) (*Snapshot, error) {
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	now := time.Now().UTC()
	snap := &Snapshot{FetchedAt: now}

	for _, key := range []string{"exercise", "workout", "set"} {
		cc, ok := s.Config.Classes[key]
		if !ok {
			return nil, fmt.Errorf("class %q not configured", key)
		}
		raw, err := s.queryClass(ctx, cc)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", key, err)
		}
		if err := s.dumpRaw(key, now, raw); err != nil {
			s.Logger.Warn("raw dump failed", "class", key, "err", err)
		}
		switch key {
		case "exercise":
			for _, r := range raw {
				snap.Exercises = append(snap.Exercises, NormalizeExercise(r, cc))
			}
		case "workout":
			for _, r := range raw {
				snap.Workouts = append(snap.Workouts, NormalizeWorkout(r, cc))
			}
		case "set":
			for _, r := range raw {
				snap.Sets = append(snap.Sets, NormalizeSet(r, cc))
			}
		}
		s.Logger.Info("fitbod: fetched", "class", key, "count", len(raw))
	}
	return snap, nil
}

func (s *Syncer) queryClass(ctx context.Context, cc ClassConfig) ([]map[string]any, error) {
	res, err := s.Client.Query(ctx, cc.Name, parse.QueryParams{
		Where:   cc.Where,
		Order:   cc.Order,
		Limit:   cc.Limit,
		Skip:    cc.Skip,
		Include: cc.Include,
	})
	if err != nil {
		return nil, err
	}
	return res.Results, nil
}

func (s *Syncer) dumpRaw(class string, t time.Time, rows []map[string]any) error {
	if s.RawDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.RawDir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.json", class, t.Format("20060102T150405Z"))
	path := filepath.Join(s.RawDir, name)
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
