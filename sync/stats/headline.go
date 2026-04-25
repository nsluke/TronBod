package stats

import (
	"github.com/nsluke/TronBod/sync/fitbod"
)

// HeadlineSelector picks the "headline lift" from a workout's working sets.
// Replaceable so the heuristic can be swapped without touching Derive.
type HeadlineSelector interface {
	Pick(sets []fitbod.Set, exByID map[string]fitbod.Exercise) *HeadlineLift
}

// CompoundTopSetByE1RM is the default heuristic:
//   - prefer compound lifts (Exercise.IsCompound)
//   - among those, pick the set with the highest e1RM
//   - tiebreak by heaviest weight, then most reps
//
// Falls back to the highest-e1RM working set of any kind if no compound is
// present.
type CompoundTopSetByE1RM struct{}

func (CompoundTopSetByE1RM) Pick(sets []fitbod.Set, exByID map[string]fitbod.Exercise) *HeadlineLift {
	var bestCompound, bestAny *fitbod.Set
	for i := range sets {
		s := &sets[i]
		if s.IsWarmup || !s.IsCompleted {
			continue
		}
		if better(s, bestAny) {
			bestAny = s
		}
		if ex, ok := exByID[s.ExerciseID]; ok && ex.IsCompound {
			if better(s, bestCompound) {
				bestCompound = s
			}
		}
	}
	pick := bestCompound
	if pick == nil {
		pick = bestAny
	}
	if pick == nil {
		return nil
	}
	return &HeadlineLift{
		Exercise:  exerciseName(pick.ExerciseID, exByID),
		WeightLbs: pick.WeightLbs,
		Reps:      pick.Reps,
	}
}

func better(a, b *fitbod.Set) bool {
	if b == nil {
		return true
	}
	ae, be := score(a), score(b)
	if ae != be {
		return ae > be
	}
	if a.WeightLbs != b.WeightLbs {
		return a.WeightLbs > b.WeightLbs
	}
	return a.Reps > b.Reps
}

// score returns the set's e1RM if recorded, otherwise an Epley estimate.
// Epley: 1RM ≈ w * (1 + reps/30).
func score(s *fitbod.Set) float64 {
	if s.E1RM > 0 {
		return s.E1RM
	}
	return s.WeightLbs * (1 + float64(s.Reps)/30.0)
}

func exerciseName(id string, m map[string]fitbod.Exercise) string {
	if e, ok := m[id]; ok {
		return e.Name
	}
	return ""
}
