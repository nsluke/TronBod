package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsluke/TronBod/sync/fitbod"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tt
}

// Reference "now" for these tests: Friday 2026-04-24T12:00:00Z. Week start
// (Monday) is 2026-04-20.
func now() time.Time { return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC) }

func TestDerive_EmptySnapshot(t *testing.T) {
	got := Derive(&fitbod.Snapshot{}, Options{Now: now()})
	if got.ThisWeek.Workouts != 0 {
		t.Errorf("workouts = %d, want 0", got.ThisWeek.Workouts)
	}
	if got.StreakWeeks != 0 {
		t.Errorf("streak = %d, want 0", got.StreakWeeks)
	}
	if got.LastWorkout != nil {
		t.Errorf("LastWorkout = %+v, want nil", got.LastWorkout)
	}
	if len(got.PRsThisMonth) != 0 {
		t.Errorf("PRs = %v, want empty", got.PRsThisMonth)
	}
}

func TestDerive_ThisWeekVolumeAndSets(t *testing.T) {
	mon := mustParse(t, "2026-04-20T18:00:00Z") // this week
	last := mustParse(t, "2026-04-13T18:00:00Z") // last week
	snap := &fitbod.Snapshot{
		Workouts: []fitbod.Workout{
			{ID: "w1", StartTime: mon, EndTime: mon.Add(60 * time.Minute)},
			{ID: "w0", StartTime: last, EndTime: last.Add(45 * time.Minute)},
		},
		Sets: []fitbod.Set{
			{WorkoutID: "w1", WeightLbs: 225, Reps: 5, IsCompleted: true},   // 1125
			{WorkoutID: "w1", WeightLbs: 135, Reps: 8, IsCompleted: true},   // 1080
			{WorkoutID: "w1", WeightLbs: 45, Reps: 10, IsCompleted: true, IsWarmup: true}, // ignored
			{WorkoutID: "w0", WeightLbs: 200, Reps: 5, IsCompleted: true},   // not this week
		},
	}
	got := Derive(snap, Options{Now: now()})
	if got.ThisWeek.Workouts != 1 {
		t.Errorf("workouts = %d, want 1", got.ThisWeek.Workouts)
	}
	if got.ThisWeek.TotalSets != 2 {
		t.Errorf("sets = %d, want 2", got.ThisWeek.TotalSets)
	}
	if got.ThisWeek.TotalVolumeLbs != 2205 {
		t.Errorf("volume = %d, want 2205", got.ThisWeek.TotalVolumeLbs)
	}
	if got.ThisWeek.TotalDurationMin != 60 {
		t.Errorf("duration = %d, want 60", got.ThisWeek.TotalDurationMin)
	}
}

func TestDerive_Streak(t *testing.T) {
	wk := func(s string) fitbod.Workout {
		return fitbod.Workout{StartTime: mustParse(t, s)}
	}
	snap := &fitbod.Snapshot{
		Workouts: []fitbod.Workout{
			wk("2026-04-20T18:00:00Z"), // this week (W17)
			wk("2026-04-13T18:00:00Z"), // W16
			wk("2026-04-06T18:00:00Z"), // W15
			// gap at W14
			wk("2026-03-23T18:00:00Z"),
		},
	}
	got := Derive(snap, Options{Now: now()})
	if got.StreakWeeks != 3 {
		t.Errorf("streak = %d, want 3", got.StreakWeeks)
	}
}

func TestDerive_StreakDoesNotResetMidWeek(t *testing.T) {
	// This week has no workout yet, but last 4 weeks did. Streak should be 4.
	wk := func(s string) fitbod.Workout {
		return fitbod.Workout{StartTime: mustParse(t, s)}
	}
	snap := &fitbod.Snapshot{
		Workouts: []fitbod.Workout{
			wk("2026-04-13T18:00:00Z"),
			wk("2026-04-06T18:00:00Z"),
			wk("2026-03-30T18:00:00Z"),
			wk("2026-03-23T18:00:00Z"),
		},
	}
	got := Derive(snap, Options{Now: now()})
	if got.StreakWeeks != 4 {
		t.Errorf("streak = %d, want 4 (current week empty should not reset)", got.StreakWeeks)
	}
}

func TestDerive_HeadlinePrefersCompound(t *testing.T) {
	mon := mustParse(t, "2026-04-23T18:00:00Z")
	snap := &fitbod.Snapshot{
		Workouts: []fitbod.Workout{
			{ID: "w1", StartTime: mon, EndTime: mon.Add(50 * time.Minute)},
		},
		Sets: []fitbod.Set{
			// Bicep curl with high e1RM relative to its bodyweight category.
			{WorkoutID: "w1", ExerciseID: "curl", WeightLbs: 60, Reps: 10, IsCompleted: true, E1RM: 80},
			// Squat: a real heavy compound
			{WorkoutID: "w1", ExerciseID: "squat", WeightLbs: 245, Reps: 5, IsCompleted: true, E1RM: 285},
		},
		Exercises: []fitbod.Exercise{
			{ID: "curl", Name: "Bicep Curl", IsCompound: false},
			{ID: "squat", Name: "Back Squat", IsCompound: true},
		},
	}
	got := Derive(snap, Options{Now: now()})
	if got.LastWorkout == nil || got.LastWorkout.HeadlineLift == nil {
		t.Fatalf("no headline lift")
	}
	if got.LastWorkout.HeadlineLift.Exercise != "Back Squat" {
		t.Errorf("headline = %q, want Back Squat", got.LastWorkout.HeadlineLift.Exercise)
	}
	if got.LastWorkout.HeadlineLift.WeightLbs != 245 {
		t.Errorf("weight = %v, want 245", got.LastWorkout.HeadlineLift.WeightLbs)
	}
}

func TestDerive_HeadlineFallsBackToAnyWhenNoCompound(t *testing.T) {
	mon := mustParse(t, "2026-04-23T18:00:00Z")
	snap := &fitbod.Snapshot{
		Workouts: []fitbod.Workout{
			{ID: "w1", StartTime: mon, EndTime: mon.Add(30 * time.Minute)},
		},
		Sets: []fitbod.Set{
			{WorkoutID: "w1", ExerciseID: "curl", WeightLbs: 35, Reps: 12, IsCompleted: true},
			{WorkoutID: "w1", ExerciseID: "tri", WeightLbs: 50, Reps: 10, IsCompleted: true},
		},
		Exercises: []fitbod.Exercise{
			{ID: "curl", Name: "Bicep Curl"},
			{ID: "tri", Name: "Triceps Extension"},
		},
	}
	got := Derive(snap, Options{Now: now()})
	if got.LastWorkout.HeadlineLift == nil {
		t.Fatal("expected fallback headline lift")
	}
	if got.LastWorkout.HeadlineLift.Exercise != "Triceps Extension" {
		t.Errorf("headline = %q, want Triceps Extension", got.LastWorkout.HeadlineLift.Exercise)
	}
}

func TestDerive_PRsThisMonthDedupesByExercise(t *testing.T) {
	snap := &fitbod.Snapshot{
		Sets: []fitbod.Set{
			// Older 5RM PR at 300lb.
			{ExerciseID: "dl", WeightLbs: 300, Reps: 5, IsCompleted: true,
				CreatedAt: mustParse(t, "2026-01-15T00:00:00Z")},
			// New PR this month at 315lb × 5.
			{ExerciseID: "dl", WeightLbs: 315, Reps: 5, IsCompleted: true,
				CreatedAt: mustParse(t, "2026-04-10T00:00:00Z")},
			// Squat PR also recent.
			{ExerciseID: "sq", WeightLbs: 245, Reps: 5, IsCompleted: true,
				CreatedAt: mustParse(t, "2026-04-12T00:00:00Z")},
			// Old squat — not a PR (matches but outside window).
			{ExerciseID: "sq", WeightLbs: 245, Reps: 5, IsCompleted: true,
				CreatedAt: mustParse(t, "2026-02-01T00:00:00Z")},
		},
		Exercises: []fitbod.Exercise{
			{ID: "dl", Name: "Deadlift", IsCompound: true},
			{ID: "sq", Name: "Back Squat", IsCompound: true},
		},
	}
	got := Derive(snap, Options{Now: now()})
	if len(got.PRsThisMonth) != 2 {
		t.Fatalf("PRs = %d, want 2: %+v", len(got.PRsThisMonth), got.PRsThisMonth)
	}
	// Sorted by date desc → squat first.
	if got.PRsThisMonth[0].Exercise != "Back Squat" {
		t.Errorf("first PR = %q, want Back Squat", got.PRsThisMonth[0].Exercise)
	}
	if got.PRsThisMonth[1].Exercise != "Deadlift" || got.PRsThisMonth[1].WeightLbs != 315 {
		t.Errorf("second PR = %+v, want Deadlift @ 315", got.PRsThisMonth[1])
	}
}

func TestDerive_FromFixture(t *testing.T) {
	path := filepath.Join("testdata", "sample_snapshot.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snap fitbod.Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	got := Derive(&snap, Options{Now: now()})
	if got.ThisWeek.Workouts == 0 {
		t.Errorf("expected at least one workout this week from fixture; got 0")
	}
	if got.LastWorkout == nil || got.LastWorkout.HeadlineLift == nil {
		t.Errorf("expected fixture to produce a headline lift")
	}
}
