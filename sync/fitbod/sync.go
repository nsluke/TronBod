package fitbod

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nsluke/TronBod/sync/fitbodapi"
)

type Syncer struct {
	Client *fitbodapi.Client
	Config *Config
	RawDir string // where to dump raw responses; "" disables
	Logger *slog.Logger

	// MaxWorkouts caps how far into history we pull. The stats layer only
	// looks at the last ~30 days; pulling the user's full lifetime is wasteful.
	// 0 → 200.
	MaxWorkouts int
}

func (s *Syncer) Run(ctx context.Context) (*Snapshot, error) {
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.MaxWorkouts == 0 {
		s.MaxWorkouts = 200
	}

	now := time.Now().UTC()
	snap := &Snapshot{FetchedAt: now}

	excfg, ok := s.Config.Classes["exercise"]
	if !ok {
		return nil, fmt.Errorf("class %q not configured", "exercise")
	}
	exercises, err := s.fetchExercises(ctx, excfg)
	if err != nil {
		return nil, fmt.Errorf("fetch exercises: %w", err)
	}
	snap.Exercises = exercises
	s.Logger.Info("fitbod: fetched", "class", "exercise", "count", len(exercises))

	wcfg := s.Config.Classes["workout"]
	scfg := s.Config.Classes["set"]
	bcfg := s.Config.Classes["breakdown"]

	workouts, sets, err := s.fetchWorkouts(ctx, wcfg, scfg, bcfg, now)
	if err != nil {
		return nil, fmt.Errorf("fetch workouts: %w", err)
	}
	snap.Workouts = workouts
	snap.Sets = sets
	s.Logger.Info("fitbod: fetched", "class", "workout", "count", len(workouts))
	s.Logger.Info("fitbod: fetched", "class", "set", "count", len(sets))

	return snap, nil
}

func (s *Syncer) fetchExercises(ctx context.Context, cc ClassConfig) ([]Exercise, error) {
	rawAll, err := s.fetchPaginated(ctx, cc.Backend, cc.ListPath, nil, "exercise")
	if err != nil {
		return nil, err
	}
	out := make([]Exercise, 0, len(rawAll))
	for _, item := range rawAll {
		out = append(out, NormalizeExercise(item, cc))
	}
	return out, nil
}

func (s *Syncer) fetchWorkouts(ctx context.Context, wcfg, scfg, bcfg ClassConfig, now time.Time) ([]Workout, []Set, error) {
	extra := url.Values{}
	extra.Set("sort", "-date_performed")
	rawAll, err := s.fetchPaginated(ctx, wcfg.Backend, wcfg.ListPath, extra, "workout")
	if err != nil {
		return nil, nil, err
	}
	if len(rawAll) > s.MaxWorkouts {
		rawAll = rawAll[:s.MaxWorkouts]
	}
	workouts := make([]Workout, 0, len(rawAll))
	var sets []Set
	for _, item := range rawAll {
		workouts = append(workouts, NormalizeWorkout(item, wcfg))
		sets = append(sets, NormalizeSets(item, wcfg, scfg, bcfg)...)
	}
	return workouts, sets, nil
}

// fetchPaginated walks JSON:API page[number]=1..N until a short page, dumps
// the raw rows under classKey, returns the combined slice.
func (s *Syncer) fetchPaginated(ctx context.Context, backend, path string, extra url.Values, classKey string) ([]map[string]any, error) {
	pageSize := s.Config.Backends[backend].PageSize
	if pageSize == 0 {
		pageSize = 1000
	}

	var rawAll []map[string]any
	page := 1
	for {
		q := url.Values{}
		for k, v := range extra {
			q[k] = v
		}
		q.Set("page[number]", strconv.Itoa(page))
		q.Set("page[size]", strconv.Itoa(pageSize))

		var resp struct {
			Data []map[string]any `json:"data"`
		}
		if err := s.Client.Get(ctx, backend, path, q, &resp); err != nil {
			return nil, err
		}
		rawAll = append(rawAll, resp.Data...)
		if len(resp.Data) < pageSize {
			break
		}
		page++
	}
	if err := s.dumpRaw(classKey, time.Now().UTC(), rawAll); err != nil {
		s.Logger.Warn("raw dump failed", "class", classKey, "err", err)
	}
	return rawAll, nil
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
