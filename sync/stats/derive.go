package stats

import (
	"sort"
	"time"

	"github.com/nsluke/TronBod/sync/fitbod"
)

type Options struct {
	Now      time.Time        // injectable for tests; defaults to time.Now()
	Headline HeadlineSelector // defaults to CompoundTopSetByE1RM
}

func Derive(snap *fitbod.Snapshot, opts Options) Stats {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	} else {
		opts.Now = opts.Now.UTC()
	}
	if opts.Headline == nil {
		opts.Headline = CompoundTopSetByE1RM{}
	}

	exByID := snap.ExerciseByID()
	setsByWorkout := groupSetsByWorkout(snap.Sets)

	weekStart := startOfWeek(opts.Now)
	weekEnd := weekStart.AddDate(0, 0, 7)

	out := Stats{
		UpdatedAt:    opts.Now,
		ThisWeek:     weekSummary(snap, setsByWorkout, weekStart, weekEnd),
		StreakWeeks:  streakWeeks(snap.Workouts, opts.Now),
		LastWorkout:  lastWorkout(snap, setsByWorkout, exByID, opts.Headline),
		PRsThisMonth: prsThisMonth(snap, exByID, opts.Now),
	}
	if out.PRsThisMonth == nil {
		out.PRsThisMonth = []PR{}
	}
	return out
}

func groupSetsByWorkout(sets []fitbod.Set) map[string][]fitbod.Set {
	m := map[string][]fitbod.Set{}
	for _, s := range sets {
		m[s.WorkoutID] = append(m[s.WorkoutID], s)
	}
	return m
}

func weekSummary(snap *fitbod.Snapshot, byWorkout map[string][]fitbod.Set, start, end time.Time) WeekSummary {
	var sum WeekSummary
	for _, w := range snap.Workouts {
		ts := workoutTime(w)
		if ts.IsZero() || ts.Before(start) || !ts.Before(end) {
			continue
		}
		sum.Workouts++
		sum.TotalDurationMin += int(w.Duration().Minutes())
		for _, s := range byWorkout[w.ID] {
			if s.IsWarmup || !s.IsCompleted {
				continue
			}
			sum.TotalSets++
			sum.TotalVolumeLbs += int(s.Volume())
		}
	}
	return sum
}

// streakWeeks counts consecutive weeks (including the current one if it has
// a workout, otherwise starting from the previous week) that have at least
// one workout, walking back from now.
func streakWeeks(workouts []fitbod.Workout, now time.Time) int {
	if len(workouts) == 0 {
		return 0
	}
	hasWeek := map[time.Time]bool{}
	for _, w := range workouts {
		ts := workoutTime(w)
		if ts.IsZero() {
			continue
		}
		hasWeek[startOfWeek(ts)] = true
	}
	cur := startOfWeek(now)
	if !hasWeek[cur] {
		// "this week" hasn't happened yet — start counting from last week.
		cur = cur.AddDate(0, 0, -7)
	}
	streak := 0
	for hasWeek[cur] {
		streak++
		cur = cur.AddDate(0, 0, -7)
	}
	return streak
}

func lastWorkout(snap *fitbod.Snapshot, byWorkout map[string][]fitbod.Set, exByID map[string]fitbod.Exercise, sel HeadlineSelector) *LastWorkout {
	var latest *fitbod.Workout
	var latestTS time.Time
	for i := range snap.Workouts {
		w := &snap.Workouts[i]
		ts := workoutTime(*w)
		if ts.IsZero() {
			continue
		}
		if latest == nil || ts.After(latestTS) {
			latest = w
			latestTS = ts
		}
	}
	if latest == nil {
		return nil
	}
	return &LastWorkout{
		Date:         latestTS,
		DurationMin:  int(latest.Duration().Minutes()),
		HeadlineLift: sel.Pick(byWorkout[latest.ID], exByID),
	}
}

// prsThisMonth: for each (exercise, reps) bucket, find the heaviest weight
// across all known sets. A PR-set is one that ties or beats that best weight
// AND happened in the trailing 30 days. Dedupe by exercise (keep heaviest).
// Sorted by date desc.
func prsThisMonth(snap *fitbod.Snapshot, exByID map[string]fitbod.Exercise, now time.Time) []PR {
	type key struct {
		ex   string
		reps int
	}
	bestWeight := map[key]float64{}
	for _, s := range snap.Sets {
		if s.IsWarmup || !s.IsCompleted || s.Reps <= 0 {
			continue
		}
		k := key{s.ExerciseID, s.Reps}
		if w, ok := bestWeight[k]; !ok || s.WeightLbs > w {
			bestWeight[k] = s.WeightLbs
		}
	}

	cutoff := now.AddDate(0, 0, -30)
	bestPerExercise := map[string]PR{} // exercise_id → heaviest PR in window
	for _, s := range snap.Sets {
		if s.IsWarmup || !s.IsCompleted || s.Reps <= 0 {
			continue
		}
		ts := setTime(s)
		if ts.IsZero() || ts.Before(cutoff) {
			continue
		}
		if s.WeightLbs < bestWeight[key{s.ExerciseID, s.Reps}] {
			continue
		}
		pr := PR{
			Exercise:  exerciseName(s.ExerciseID, exByID),
			WeightLbs: s.WeightLbs,
			Reps:      s.Reps,
			Date:      ts,
		}
		if cur, ok := bestPerExercise[s.ExerciseID]; !ok || pr.WeightLbs > cur.WeightLbs {
			bestPerExercise[s.ExerciseID] = pr
		}
	}

	out := make([]PR, 0, len(bestPerExercise))
	for _, pr := range bestPerExercise {
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out
}

// workoutTime prefers StartTime, falling back to EndTime.
func workoutTime(w fitbod.Workout) time.Time {
	if !w.StartTime.IsZero() {
		return w.StartTime
	}
	return w.EndTime
}

func setTime(s fitbod.Set) time.Time {
	return s.CreatedAt
}

// startOfWeek returns Monday 00:00 UTC of the week containing t.
func startOfWeek(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -(weekday - 1))
}
