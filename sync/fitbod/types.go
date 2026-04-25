package fitbod

import "time"

// Workout, Set, and Exercise are the normalized shapes the stats layer
// operates on. Anything Fitbod-specific stops at the normalize boundary.

type Workout struct {
	ID            string
	StartTime     time.Time
	EndTime       time.Time
	Name          string
	DurationSec   int
}

func (w Workout) Duration() time.Duration {
	if w.DurationSec > 0 {
		return time.Duration(w.DurationSec) * time.Second
	}
	if !w.StartTime.IsZero() && !w.EndTime.IsZero() {
		return w.EndTime.Sub(w.StartTime)
	}
	return 0
}

type Set struct {
	ID          string
	WorkoutID   string
	ExerciseID  string
	WeightLbs   float64
	Reps        int
	E1RM        float64
	IsWarmup    bool
	IsCompleted bool
	CreatedAt   time.Time
}

// Volume is the simple weight × reps contribution. Warmups don't count.
func (s Set) Volume() float64 {
	if s.IsWarmup || !s.IsCompleted {
		return 0
	}
	return s.WeightLbs * float64(s.Reps)
}

type Exercise struct {
	ID         string
	Name       string
	IsCompound bool
}

// Snapshot is one polling cycle's normalized data.
type Snapshot struct {
	FetchedAt time.Time
	Workouts  []Workout
	Sets      []Set
	Exercises []Exercise
}

// ExerciseByID returns a lookup map. Useful in stats derivation.
func (s Snapshot) ExerciseByID() map[string]Exercise {
	m := make(map[string]Exercise, len(s.Exercises))
	for _, e := range s.Exercises {
		m[e.ID] = e
	}
	return m
}
