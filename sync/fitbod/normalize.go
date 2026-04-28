package fitbod

import (
	"strconv"
	"strings"
	"time"
)

// Fitbod stores weights in kg on the wire; the rest of this codebase (and
// the stats layer's WeightLbs / TotalVolumeLbs fields) speaks lbs. We
// convert at the normalize boundary.
const kgToLbs = 2.20462

// NormalizeWorkout maps one /api/v3/workout_data JSON:API object into a
// Workout. EndTime is left zero; the stats layer derives durations from
// DurationSec.
func NormalizeWorkout(raw map[string]any, cfg ClassConfig) Workout {
	return Workout{
		ID:          toString(getPath(raw, cfg.Field("id"))),
		StartTime:   parseTime(getPath(raw, cfg.Field("start_time"))),
		Name:        toString(getPath(raw, cfg.Field("name"))),
		DurationSec: toInt(getPath(raw, cfg.Field("duration_seconds"))),
	}
}

// NormalizeSets walks attributes.exercise_sets[].set_breakdown.individual_sets[]
// inside one workout JSON:API object, producing one fitbod.Set per
// individual_set (one fitbod.Set == one actual logged rep). The exercise_set's
// theoretical_max becomes the e1RM for every individual_set under it.
func NormalizeSets(workoutRaw map[string]any, wcfg, scfg, bcfg ClassConfig) []Set {
	workoutID := toString(getPath(workoutRaw, wcfg.Field("id")))
	workoutTime := parseTime(getPath(workoutRaw, wcfg.Field("start_time")))

	setsArr, _ := getPath(workoutRaw, scfg.NestedPath).([]any)

	var out []Set
	for _, sItem := range setsArr {
		setRaw, ok := sItem.(map[string]any)
		if !ok {
			continue
		}
		exerciseID := toString(getPath(setRaw, scfg.Field("exercise_id")))
		e1rmKg := toFloat(getPath(setRaw, scfg.Field("e1rm_kg")))

		breakdowns, _ := getPath(setRaw, bcfg.NestedPath).([]any)
		for _, bItem := range breakdowns {
			bRaw, ok := bItem.(map[string]any)
			if !ok {
				continue
			}
			ts := parseTime(getPath(bRaw, bcfg.Field("logged_at")))
			if ts.IsZero() {
				ts = workoutTime
			}
			out = append(out, Set{
				ID:          toString(getPath(bRaw, bcfg.Field("id"))),
				WorkoutID:   workoutID,
				ExerciseID:  exerciseID,
				WeightLbs:   toFloat(getPath(bRaw, bcfg.Field("weight_kg"))) * kgToLbs,
				Reps:        toInt(getPath(bRaw, bcfg.Field("reps"))),
				E1RM:        e1rmKg * kgToLbs,
				IsWarmup:    toBool(getPath(bRaw, bcfg.Field("is_warmup"))),
				IsCompleted: true, // wire has no completion flag — assume true if it's in the response
				CreatedAt:   ts,
			})
		}
	}
	return out
}

// NormalizeExercise maps one JSON:API exercise item ({id, type, attributes,
// relationships}) into an Exercise. IsCompound is best-effort from sparse
// data: true if attributes.mechanics contains "compound", or if
// movement_pattern is non-empty (single-joint isolation lifts don't get
// tagged with movement patterns in Fitbod's catalog). Both arrays were empty
// for ~95% of exercises in our 2026-04-28 capture, so the headline-lift
// selector still falls back often — that's expected.
func NormalizeExercise(raw map[string]any, cfg ClassConfig) Exercise {
	return Exercise{
		ID:   toString(raw["id"]),
		Name: toString(getPath(raw, cfg.Field("name"))),
		IsCompound: hasCompoundMechanic(getPath(raw, cfg.Field("mechanics"))) ||
			isNonEmptyArray(getPath(raw, cfg.Field("movement_pattern"))),
	}
}

func hasCompoundMechanic(v any) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range arr {
		if s, ok := x.(string); ok && strings.EqualFold(s, "compound") {
			return true
		}
	}
	return false
}

func isNonEmptyArray(v any) bool {
	arr, ok := v.([]any)
	return ok && len(arr) > 0
}

// --- helpers ---------------------------------------------------------------

// getPath walks a dotted path through nested maps. "attributes.name" reads
// m["attributes"]["name"]. Missing keys / non-map intermediates → nil.
func getPath(m map[string]any, path string) any {
	if path == "" || m == nil {
		return nil
	}
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// Fitbod sometimes encodes IDs as integers (workout_id, exercise_id).
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		n, _ := strconv.ParseFloat(x, 64)
		return n
	}
	return 0
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// parseTime accepts an ISO-8601 string ("2026-04-26T19:55:09Z") or nil. The
// Parse {"__type":"Date","iso":...} sentinel is no longer used by Fitbod
// but kept for backwards compatibility with old fixture data.
func parseTime(v any) time.Time {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t
		}
	case map[string]any:
		if x["__type"] == "Date" {
			if iso, ok := x["iso"].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, iso); err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}
