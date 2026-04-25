package fitbod

import (
	"time"
)

// Normalizers convert raw Parse `map[string]any` rows into the typed
// structs the stats layer wants. The cfg's field map decides which Parse
// fields supply which internal values, so renaming on the Fitbod side is
// just a config edit.

func NormalizeWorkout(raw map[string]any, cfg ClassConfig) Workout {
	return Workout{
		ID:          str(raw, "objectId"),
		StartTime:   parseTime(get(raw, cfg.Field("start_time"))),
		EndTime:     parseTime(get(raw, cfg.Field("end_time"))),
		Name:        toString(get(raw, cfg.Field("name"))),
		DurationSec: toInt(get(raw, cfg.Field("duration_seconds"))),
	}
}

func NormalizeSet(raw map[string]any, cfg ClassConfig) Set {
	return Set{
		ID:          str(raw, "objectId"),
		WorkoutID:   pointerID(get(raw, cfg.Field("workout"))),
		ExerciseID:  pointerID(get(raw, cfg.Field("exercise"))),
		WeightLbs:   toFloat(get(raw, cfg.Field("weight_lbs"))),
		Reps:        toInt(get(raw, cfg.Field("reps"))),
		E1RM:        toFloat(get(raw, cfg.Field("e1rm"))),
		IsWarmup:    toBool(get(raw, cfg.Field("is_warmup"))),
		IsCompleted: toBool(get(raw, cfg.Field("is_completed"))),
		CreatedAt:   parseTime(get(raw, "createdAt")),
	}
}

func NormalizeExercise(raw map[string]any, cfg ClassConfig) Exercise {
	return Exercise{
		ID:         str(raw, "objectId"),
		Name:       toString(get(raw, cfg.Field("name"))),
		IsCompound: toBool(get(raw, cfg.Field("is_compound"))),
	}
}

// --- helpers ---------------------------------------------------------------

// get returns m[key] safely. If key is empty (no field mapping configured)
// returns nil.
func get(m map[string]any, key string) any {
	if key == "" || m == nil {
		return nil
	}
	return m[key]
}

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
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
	}
	return 0
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// parseTime accepts either a Parse {"__type":"Date","iso":...} object, a raw
// ISO-8601 string, or `nil`. Anything else returns the zero time.
func parseTime(v any) time.Time {
	switch x := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339Nano, x)
		if err == nil {
			return t
		}
		t, err = time.Parse(time.RFC3339, x)
		if err == nil {
			return t
		}
	case map[string]any:
		if x["__type"] == "Date" {
			if iso, ok := x["iso"].(string); ok {
				t, err := time.Parse(time.RFC3339Nano, iso)
				if err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}

// pointerID extracts the objectId from a Parse pointer field, whether it's
// included (full nested object) or not (the {__type:Pointer,objectId:...}
// sentinel).
func pointerID(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if id, ok := m["objectId"].(string); ok {
		return id
	}
	return ""
}
