// Package stats derives the summary the LED display cares about from a
// normalized fitbod snapshot.
package stats

import "time"

type Stats struct {
	UpdatedAt    time.Time     `json:"updated_at"`
	ThisWeek     WeekSummary   `json:"this_week"`
	StreakWeeks  int           `json:"streak_weeks"`
	LastWorkout  *LastWorkout  `json:"last_workout,omitempty"`
	PRsThisMonth []PR          `json:"prs_this_month"`
}

type WeekSummary struct {
	Workouts         int `json:"workouts"`
	TotalVolumeLbs   int `json:"total_volume_lbs"`
	TotalSets        int `json:"total_sets"`
	TotalDurationMin int `json:"total_duration_min"`
}

type LastWorkout struct {
	Date         time.Time     `json:"date"`
	DurationMin  int           `json:"duration_min"`
	HeadlineLift *HeadlineLift `json:"headline_lift,omitempty"`
}

type HeadlineLift struct {
	Exercise  string  `json:"exercise"`
	WeightLbs float64 `json:"weight_lbs"`
	Reps      int     `json:"reps"`
}

type PR struct {
	Exercise  string    `json:"exercise"`
	WeightLbs float64   `json:"weight_lbs"`
	Reps      int       `json:"reps"`
	Date      time.Time `json:"date"`
}
