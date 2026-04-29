# Fitbod API reference

A catalog of every endpoint we've observed Fitbod's Android client hit
(2026-04-28 capture). Use this when you want to add a new display frame
or a new derived stat — start by skimming the **Available but not yet
synced** section to see what's already retrievable without any new MITM
work.

Field-level shape for the two endpoints we currently consume
(`/api/v3/workout_data` and `/api/v3/exercises`) lives in
[`schema-notes.md`](./schema-notes.md). This doc covers the rest.

## Where the raw data lives

Both gitignored (under `data/`):

| Path | Source | Use |
| --- | --- | --- |
| `data/raw/exercise-<ts>.json` | Most recent sync | Current snapshot of the exercise catalog. |
| `data/raw/workout-<ts>.json` | Most recent sync | Current snapshot of all workouts (with sets+breakdowns). |
| `data/mitm/sample-<resource>.json` | MITM capture | One representative request+response per endpoint Fitbod's app hit. Headers redacted. |
| `data/mitm/discovery.json` | MITM capture | Per-resource summary (counts, top-level + data fields, query params seen). |
| `data/mitm/flows.mitm` | MITM capture | Full raw flow archive — replay with `mitmdump -nr data/mitm/flows.mitm`. |

To inspect any sample interactively:

```bash
python3 -m json.tool data/mitm/sample-user.json | less
# or just the response body:
jq '.response.body' data/mitm/sample-user.json
```

To re-capture (refresh schema or grab a write-path POST), run
`tools/mitm-prep/capture.sh start` against the AVD that's already set up.

## What sync.go currently fetches

Just two endpoints:

- `GET nautilus.fitbod.me/api/v3/workout_data` — paginated, sorted newest-first,
  capped at `MaxWorkouts` (default 200). Returns full workouts with nested
  `exercise_sets[].set_breakdown.individual_sets[]`.
- `GET nautilus.fitbod.me/api/v3/exercises` — paginated catalog. We use
  this to resolve `set.exercise_id` → `Exercise.Name` + `IsCompound`.

Everything else below is **available** (auth + Cloudflare bypass already
work for any `*.fitbod.me` host) but unused.

## Backends overview

| Host | Purpose | Endpoints below |
| --- | --- | --- |
| `nautilus.fitbod.me` | Catalog + workouts (JSON:API) | 19 |
| `metros.fitbod.me` | User aggregates (REST) | 6 |
| `gate-keeper.fitbod.me` | Auth + user object | 2 (login + users) |
| `gympulse.fitbod.me` | Gym/club data | 1 (mostly empty) |
| `billing.fitbod.me` | Subscriptions | 1 |
| `pyserve.fitbod.me` | Server-side compute | 1 (POST llamabod) |
| `app-media.fitbod.me` | Image/video CDN | (asset URLs from `exercise_details`) |

## Endpoints worth knowing — `metros.fitbod.me` (your data)

Most useful surface area for the LED display: pre-computed aggregates per
user. Cheap (single small payload each), no heavy join logic.

### `/v1/user` — dashboard aggregate
**Status:** unused.  Single object with:

```
streak_length, goal, start_of_week, timezone,
workouts_this_week, total_workouts, total_streak_workouts,
streak_period {from, to},
streak_week_workouts [{id, duration, date_performed, source_id}],
workout_details      [{id, duration, date_performed, source_id}],
milestones [{name, level, next_level, current_val, target,
             asset_url, asset_url_small, color_primary, color_secondary}]
```

Display ideas: Streak counter (server-computed, more accurate than our
own `streakWeeks()` calc which depends on workout-detail dates lining up).
"Workouts this week / weekly goal" gauge.

### `/v1/user/milestones` — leveling progress
**Status:** unused. Returns the same `milestones[]` array as `/v1/user`,
but standalone. Each entry:

```
{name: "volume", level: "Level 5", next_level: "Level 6",
 current_val: 157246.098, target: 226797,
 asset_url: "https://storage.googleapis.com/metros-milestones/<name>/64x64/<level>.png",
 asset_url_small: "https://storage.googleapis.com/metros-milestones/<name>/24x24/<level>.png",
 color_primary: "#F4476D", color_secondary: "#..."}
```

Three milestones in our capture: volume, days, sets. Server-hosted
24×24 + 64×64 PNG icons + brand colors — slot directly into a Pixlet
frame.

### `/v2/muscle_strength/summary` — Strength Index
**Status:** unused. Server-computed strength score per muscle group + overall.

```
{is_locked, calculated_at,
 overall_strength: 56.365,       prev_overall_strength: 55.66,
 muscle_group_data: [{muscle_group_id, muscle_group_name, type,
                      mg_external_id, history: [...]}],
 summary_metrics:   [{type, mstrength_score, trend}]}
```

Display idea: "Strength Index 56.4 (▲ 0.7)" — pre/now diff drives an arrow.

### `/v1/user/set_goals` — set-count goal progress
**Status:** unused.

```
{period_start, period_end, progress_percentage,
 breakdown: { ... per-bucket counts },
 history:  [ ... prior periods ]}
```

Display idea: weekly set goal ring.

### `/v1/user/metrics/summary` — body-metric aggregates
**Status:** unused. List of per-metric latest values.

```
[{id, metric_id, label, units, user_id, date_recorded,
  metric_value, reporting_source}]
```

Pair with `/v1/metrics` (catalog of available metrics — bodyweight, BMI,
calories, etc.) to know what the metric_ids mean.

### `/v1/metrics` — metric catalog
**Status:** unused.

```
[{metric_id, metric_name, label, description, units, group_name,
  group_label, enabled}]
```

14 entries in our capture. Static reference data; fetch once.

## Endpoints worth knowing — `nautilus.fitbod.me` (catalog)

JSON:API. All take `page[number]` + `page[size]` + (where applicable)
`filter[updated_at][gt]=<iso>` for incremental sync. Each item is
`{id, type, attributes, relationships}`.

### `/api/v3/workout_data` — **USED**
The workout source. See [`schema-notes.md`](./schema-notes.md) for full
shape.

### `/api/v3/exercises` — **USED**
The exercise catalog. See `schema-notes.md`. Most relevant attributes:
`name, slug, parse_id, external_resource_id, is_cardio, is_bodyweight,
is_unilateral, is_timed, is_distance, is_assisted, mechanics,
movement_pattern, warm_start_1rm_kgs`.

### `/api/v3/exercise_details` — instructions + media
**Status:** unused. 1000+ items. Per exercise:

```
{instructions: "Stand upright with your feet shoulder-width apart...",
 website_name, full_website_name, filming_location,
 video_url:     "https://exercise-mp4s.fitbod.me/<id>.mp4",
 image_url:     "https://exercise-jpgs.fitbod.me/<id>.jpg",
 animation_url: "https://exercise-gifs.fitbod.me/<id>.gif",
 media: {v2: {images: {thumb, header},
              videos: {one_rep, header, full},
              angles: [{id, image, video}, ...]}}}
```

Display idea: show the headline-lift exercise's `image_url` thumbnail on
the LED frame.

### `/api/v3/exercise_aliases` — alternative names
**Status:** unused. 785 entries. `{alias_name, locale}` per alias, plus
`relationships.exercise` link. Useful for fuzzy display names ("Squats"
instead of "Back Squat").

### `/api/v3/exercise_primary_muscle_groups`, `..._secondary_muscle_groups`
**Status:** unused. 1000 each. Pure join tables (`relationships.exercise`
+ `relationships.muscle_group`). Combine with `/api/v3/muscle_groups`
(below) to know which muscles a given exercise hits.

### `/api/v3/exercise_categorizations`, `/api/v3/exercise_equipment`
**Status:** unused. Pure join tables linking exercises to categories /
equipment. The interesting attributes live on the related resources.

### `/api/v3/muscle_groups` — muscle catalog
**Status:** unused. Per group:

```
{name, is_front, is_pull, is_push, is_upperbody,
 is_accessory_muscle, utility_percentage, description}
```

Used by Fitbod for the "muscle map" rendering. The "front/back" flags
tell you which side of the body to draw.

### `/api/v3/equipment` — equipment catalog (78 items)
**Status:** unused. `{name, description, image_url, external_resource_id, ...}`.

### `/api/v3/gym_equipment` — equipment-per-gym (57 items)
**Status:** unused. Join table: `{gym_id, equipment_id, is_metric}`.

### `/api/v3/gyms` — your gyms (2 items)
**Status:** unused. Per gym:

```
{name, default_workout_config_id, deleted, originator_id, parse_id, ...}
```

### `/api/v3/equipment_weights`, `/api/v3/selected_equipment_weights`
**Status:** unused. Catalog of weight increments per equipment + which
ones the user has at their gym (357 selections in our capture). Useful
if you want to know "which dumbbell sizes does Luke own".

### `/api/v3/warm_start_values` — recommended starting weights
**Status:** unused. 837 entries keyed by `{age, height, weight, gender,
experience_level}` + `predicted_weight, predicted_sets, predicted_reps`.
Filterable: `?filter[age]=30&filter[gender]=1&filter[height]=180&...`.

### `/api/v3/resistance_bands`, `/api/v3/selected_resistance_bands`
**Status:** unused. Empty in our capture (no bands in user's gym).

### `/api/v3/custom_exercises`, `/api/v3/user_exercise_ratings`,
### `/api/v3/user_injuries`, `/api/v3/circuit_templates`,
### `/api/v3/exercise_set_templates`, `/api/v3/set_breakdown_templates`,
### `/api/v3/workout_templates`
**Status:** unused, all empty in our capture.

### `/api/v3/workout_configs` — current workout-plan settings
**Status:** unused. Single object with `cardio_as_hiit, cardio_enabled,
cardio_position, days_of_week, display_fitness_goal,
entire_hiit_workout, ...`.

### `/api/v2/workouts?page[size]=0&stats[total]=count` — count-only
**Status:** used implicitly only. Returns `meta.stats.total.count` and
empty `data: []`. Useful to detect "you have N workouts" without paging
the full list. Counter to `/api/v3/workout_data` which we use for actual
data.

### `/api/v2/user_profiles`
**Status:** unused. Single object: `{first_name, last_name, username,
timezone, user_id, gender, height, dob}`.

### `/api/v2/devices`
**Status:** unused. List of devices the user has signed in on:
`{make, model, os_version, fitbod_version, carrier, push_token,
logs_enabled, ip}`. (Privacy-sensitive — push_token + IP.)

### `/api/v2/app_configs`
**Status:** unused. Per-user UI prefs: `{current_gym_id,
current_workout_config_id, workout_running_notifications, view_as_metric,
fresh_muscle_groups_notifications, rest_timer_notifications,
workout_preview_time}`. `view_as_metric` would tell us whether the user
prefers kg or lbs in the UI — useful if we ever want to honor the
user's preference instead of always converting to lbs.

### `/api/v2/selected_cardio_exercises`
**Status:** unused. 12 items. Maps the user's preferred cardio
exercises to their workout config.

### `/api/v3/block_data`
**Status:** unused. The user's current training block. 2 items. Per
block: `{originator_id, workout_split, completed_at, deleted_at,
focus_exercises}`. Useful for showing block progress.

### `/api/v2/workout_config_priority_exercises`
**Status:** unused. 1 item. Tells the algorithm which exercises to
prioritize.

## Other backends

### `gate-keeper.fitbod.me`

- **`POST /users/login`** — auth entry point. Returns the master refresh
  token in the `Authorization` response header. Only call when capturing
  a fresh token. (See `schema-notes.md` § Auth.)
- **`GET /api/v1/users`** — user object. Same as `/api/v2/user_profiles`
  on nautilus, slightly different shape: `{created_at, updated_at,
  parse_id, email, original_email, facebook_id, apple_id, google_id, ...}`.

### `gympulse.fitbod.me`

- **`POST /access_token`** — host-token mint. Used implicitly.
- **`GET /api/v1/clubs`** — gym/squad data. Empty in our capture.

### `billing.fitbod.me`

- **`POST /access_token`** — host-token mint.
- **`GET /v2/subscriptions?includeExpired=1`** — your active +
  historical subscriptions. Useful if the display should show "Premium
  active until <date>" or behave differently outside trial.

### `pyserve.fitbod.me`

- **`POST /access_token`** — host-token mint.
- **`POST /v1/authenticated/functions/algo_llamabod`** — server-side
  compute. Body: `{individual_muscle_split, exercise_groups, block}`.
  Output: workout-recommendation. We never call this; it's how
  Fitbod's algorithm picks the next workout.

### `blimp.fitbod.me`

- **`POST /access_token`**.
- **`GET /blimps`** — telemetry config flags. Not interesting for sync.

## Display ideas, ranked

1. **Frame: "Strength Index 56.4 ▲0.7"** — one GET to
   `metros.fitbod.me/v2/muscle_strength/summary`. Shows progress in a
   single number Fitbod already computes.
2. **Frame: "Volume Level 5 → 6 (69 %)"** — one GET to
   `metros.fitbod.me/v1/user/milestones`. Server-hosted icon URL
   available.
3. **Frame: "Goal: 3 / 5 workouts this week"** — fold into the existing
   week summary, source `goal` from `/v1/user`.
4. **Frame: headline-lift exercise thumbnail** — fetch
   `image_url` from `/api/v3/exercise_details` for the picked
   exercise; render as a 32×32 left panel beside the text.
5. **Use server's streak counter** — replace our `streakWeeks()` calc
   with `/v1/user`'s `streak_length`. Less risk of drift.

## Adding a new endpoint to the sync

1. Pick a host + path from above.
2. Add the resource to `classes.yaml.example` under `classes:` with the
   field mapping you care about.
3. Wire `Syncer.Run` in `sync/fitbod/sync.go` to call
   `s.Client.Get(ctx, host, path, query, &resp)`.
4. Add a normalizer (mirror `NormalizeWorkout` / `NormalizeExercise`).
5. Surface in `sync/stats/types.go` + `derive.go` if it should land in
   `stats.json`.
6. Render in `pixlet/fitbod_stats.star` if it should hit the LED.

`fitbodapi.Client` already handles auth (mints host-scoped access tokens
from your refresh_token on demand) — adding a new host is just
`Client.Get(ctx, "<host>", ...)` against any `*.fitbod.me` you've seen
on the wire.
