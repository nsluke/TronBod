# Fitbod schema — captured

_2026-04-28 (revised after a fresh-login capture, which surfaced the auth
flow and the real workout-list endpoint that the prior already-logged-in
capture missed). Captured via mitmproxy + Frida + tls-client against
Fitbod 8.15.0-0 (build 10815000)._

## Methodology

- AVD: API 36 Google APIs arm64, rooted with Magisk via rootAVD, mitmproxy
  CA promoted to apex cert store via the AlwaysTrustUserCerts module.
- mitmproxy with `--set 'allow_hosts=\.fitbod\.me'` so we only intercept
  Fitbod traffic — Google Play Services has its own pinning that we don't
  bypass and that breaks Fitbod's "online" probe if intercepted.
- Frida `multi-unpinning.js` attached at app launch (Fitbod barely pins;
  most of the script's hooks log "pinner not found").
- `pm clear com.fitbod.fitbod` between runs to force the app to redo the
  full token-mint dance.
- Capture artifacts in `data/mitm/`: `discovery.json`, `sample-*.json` per
  resource, plus the raw `flows.mitm` archive (replay with
  `mitmdump -nr data/mitm/flows.mitm`).

## Hosts (all behind Cloudflare bot WAF)

| Host | Style | Purpose |
| --- | --- | --- |
| `gate-keeper.fitbod.me` | REST | Sign-in (`POST /users/login`); user object lookup. |
| `nautilus.fitbod.me` | JSON:API (`application/vnd.api+json`) | **Workout data + catalog.** Workouts at `/api/v3/workout_data`, exercises at `/api/v3/exercises`, plus equipment/muscle_groups/etc. |
| `metros.fitbod.me` | Custom REST | User-state aggregates: `/v1/user`, `/v1/user/set_goals`, `/v2/muscle_strength/summary`, `/v1/user/milestones`. NOT the workout source — `/v1/workouts?id=...` returns lean summaries only (no sets). |
| `gympulse.fitbod.me` | REST | Gym/club data (`/api/v1/clubs`). |
| `billing.fitbod.me` | REST | Subscriptions. |
| `blimp.fitbod.me` | REST | Telemetry config. |
| `pyserve.fitbod.me` | REST | Compute functions (`/v1/authenticated/functions/...`). |

Plus 3rd-party SDKs (Iterable, Mixpanel, Branch, Facebook, LaunchDarkly,
Appsflyer, Crashlytics) — irrelevant to a sync layer.

⚠️ **Cloudflare blocks Go's stock TLS+HTTP/2 fingerprint.** uTLS alone is
not enough — HTTP/2 SETTINGS frame and pseudo-header order also have to
match. Use `bogdanfinn/tls-client` with the `Okhttp4Android13` profile.

## Auth

```
POST https://gate-keeper.fitbod.me/users/login
  body: {"auth": {"google": {"token": "<google-id-jwt>"}}}
       (apple/email/facebook variants exist; keys: apple_id, facebook_id)
  → 201
  body:    {email, id, created_at, updated_at, parse_id, google_id, ...}
  HEADERS:
    Authorization: Bearer <refresh_token>     ← captured here
    x-auth-token: <opaque>                     (Rails session token; unused)
```

The `refresh_token` is the master JWT; subject claim is `user_id`, scope
`scp:user`, expiry ~1 year (`exp`), `aud:null`, `iss` absent.

Then for every backend host (`nautilus`, `metros`, `gympulse`, `billing`,
`blimp`, `pyserve`):

```
POST https://<host>.fitbod.me/access_token
  body: {"refresh_token": "<refresh_token>"}
  → 201
  body: {"access_token": "<host-scoped JWT>"}
```

Each access_token has host-specific claims:

| Host | `iss` | `aud` | Lifetime |
| --- | --- | --- | --- |
| nautilus | `nautilus` | `nautilus.prod.fitbod.me` | ~1 year (clones the refresh_token claims + adds aud/iss) |
| metros / blimp | `.prod` | — | ~30 days |
| billing | `v1.3.60.prod` | — | ~30 days |
| gympulse | `gympulse.production` | — | ~1 hour |
| pyserve | (none) | — | ~30 days |

A token minted for one host is **rejected** by another (`401 {"meta": {"error": "Auth token is invalid"}}`).

Subsequent host-specific calls send `Authorization: Bearer <host-access_token>`.
Content-Type is `application/vnd.api+json` for nautilus, `application/json`
for the REST hosts.

User-Agent on the wire: `fitbod/8.15.0-0 (com.fitbod, build:10815000, Android 36, Manufacturer: Google, Model: sdk_gphone64_arm64) okhttp/5.3.2`
(`gate-keeper /users/login` uses `ktor-client`).

## Resources

### `nautilus.fitbod.me/api/v3/workout_data` — **the workout source**

JSON:API, `data: [{id, type:"workout_data", attributes}, ...]`. Page-based
pagination (`page[number]`, `page[size]=1000` default). Sortable
(`sort=-date_performed`) and filterable (`filter[updated_at][gt]=<iso>`)
for incremental sync.

Each workout's `attributes`:

```jsonc
{
  "external_id":       584116534,           // int; matches metros' workout_id
  "workout_name":      "Upper A",
  "workout_duration":  2157,                // int seconds
  "date_performed":    "2026-04-28T00:11:34+00:00",  // ISO 8601
  "calories_burned":   338.46,              // float
  "source_id":         0,                   // 0=app, 1=imported (HealthKit etc)
  "deleted":           false,
  "created_at":        "...", "updated_at": "...",
  "parse_id":          null,                // legacy
  "originator_id":     "<uuid>",
  "workout_finalized": true,
  "health_connect_id": null,
  "individual_muscle_split":             { ... },
  "individual_muscle_split_specialized": { ... },
  "workout_config_muscle_split":         { ... },
  "block_id":          null, "block_phase_index": null, "block_phase_step": null,
  "circuits":          [ ... ],             // for circuit-style workouts
  "exercise_sets":     [ ... ]              // ← non-circuit set list
}
```

### `attributes.exercise_sets[]` — one entry per exercise group

```jsonc
{
  "id":                            "5f55cb19-...",   // uuid
  "workout_id":                    584116534,
  "exercise_id":                   480,                // legacy id (parse-era)
  "exercise_external_resource_id": 4,                  // ⚠️ JOIN KEY to catalog exercise.id
  "gym_equipment_id":              null,
  "is_max_effort":                 false,
  "theoretical_max":               0.0,                // server-computed e1RM (kg); 0 for warmup-only sets
  "intrinsic_theoretical_max":     0.0,
  "ssa_theoretical_max":           null,
  "list_position":                 0,
  "perceived_exertion_rating":     0,
  "is_focus_exercise":             false,
  "circuit_id":                    null,
  "originator_id":                 "<uuid>",
  "is_metric":                     null,
  "deleted":                       null,
  "notes":                         "",
  "set_breakdown":                 { ... },              // ← single object, not array
  "created_at":                    "...", "updated_at": "..."
}
```

### `set_breakdown.individual_sets[]` — one entry per actual rep performed

⚠️ **Field naming mixes camelCase and snake_case** on the wire — match exactly.

```jsonc
{
  "_id":            "1D1BF10A-...",     // uuid; note the underscore prefix
  "weight":         29.48,              // ⚠️ kg, NOT lbs
  "reps":           8,
  "duration":       0,                  // seconds, for timed exercises
  "distance":       0,                  // meters, for cardio
  "distance_m":     3777,
  "distance_unit":  "m",
  "incline":        0,                  // %
  "resistance":     0,
  "isWarmup":       true,               // ⚠️ camelCase
  "is_amrap":       false,              // snake_case
  "restTime":       45,                 // ⚠️ camelCase
  "logged_at":      "2026-04-28T00:25:23Z"
}
```

Cardio-only sets omit `weight`/`reps` (zero defaults); strength sets omit
`duration`/`distance` (zero defaults).

### `nautilus.fitbod.me/api/v3/exercises` — catalog

JSON:API. Each item is `{id, type:"exercises", attributes, relationships}`.
The `attributes` are stable across syncs; use `filter[updated_at][gt]=<iso>`
for incremental fetches. Relevant attributes:

```
name                    string         # e.g. "Battle Ropes"
slug                    string         # e.g. "battle-ropes"
parse_id                string         # legacy, kept for migrations
external_resource_id    int            # used in media URLs (/<id>.mp4)
created_at, updated_at  ISO 8601
warm_start_1rm_kgs      float | null   # ⚠️ confirms wire is kg
is_cardio               bool
is_bodyweight           bool
is_unilateral           bool
is_timed                bool
is_distance             bool
is_assisted             bool
mechanics               []string       # "compound"/"isolation" — ~95% empty in our capture
movement_pattern        []string       # "squat"/"hinge"/"push"/"pull" — also sparse
joint_movement          string | null
tier, body_tier, power_tier, oly_tier  int
```

`/api/v3/exercise_details` is parallel and adds `instructions`, `video_url`,
`image_url`, `animation_url`, plus an `attributes.media.v2` struct with
thumb/header/one_rep/full + multiple angles. Filter by
`filter[exercise_id][eq]=<csv>`.

### `metros.fitbod.me/v1/user` — aggregate dashboard

This single endpoint is essentially the Tronbyt headline:

```jsonc
{
  "data": {
    "streak_length":         1,
    "goal":                  3,
    "start_of_week":         "sun",
    "timezone":              "America/New_York",
    "workouts_this_week":    3,
    "total_workouts":        104,
    "total_streak_workouts": 3,
    "streak_period":         {"from": "...", "to": "..."},
    "streak_week_workouts":  [ {id, duration, date_performed, source_id}, ... ],
    "workout_details":       [ {id, duration, date_performed, source_id}, ... ],
    "milestones":            [
      { "name": "volume", "level": "Level 5", "next_level": "Level 6",
        "current_val": 157246.098, "target": 226797,
        "asset_url": "...", "color_primary": "#F4476D" }
    ]
  }
}
```

`source_id` distinguishes app-logged (0) vs imported (1, e.g. HealthKit).

### `metros.fitbod.me/v2/muscle_strength/summary` — Strength Index

```jsonc
{
  "data": {
    "is_locked":             false,
    "calculated_at":         "...",
    "overall_strength":      56.365,
    "prev_overall_strength": 55.66,
    "muscle_group_data":     [ {muscle_group_id, muscle_group_name, type, mg_external_id, history}, ... ],
    "summary_metrics":       [ {type, mstrength_score, trend}, ... ]
  }
}
```

Server-computed; not derivable from raw set history.

## Quirks worth remembering

- **Field-name casing is inconsistent** within a single payload: `isWarmup`/`restTime` are camelCase but `is_amrap`/`rest_time` are snake_case in different objects. Match exactly.
- **`exercise_external_resource_id`** on a set is the join key to the catalog `exercise.id` (which is itself a string in JSON:API). The set's plain `exercise_id` is the legacy parse-era id.
- **`theoretical_max`** on an exercise_set can legitimately be 0 (warmup-only sets). Don't treat 0 as missing.
- **kg, not lbs** everywhere on the wire. `warm_start_1rm_kgs` makes the unit explicit. Convert at the normalize boundary.
- **`/v1/workouts?id=ID,...`** on metros returns lean summaries (no sets) — useful for headline counters but not for set-level data. Use `/api/v3/workout_data` for the real list.
- **Per-host bearer tokens are not interchangeable.** A token minted at `nautilus/access_token` won't work on `metros`.
- **Telemetry SDK hosts** (mixpanel, branch, iterable, launchdarkly, appsflyer, crashlytics, facebook) account for ~half the captured flows. Filter them out (`allow_hosts=\.fitbod\.me`) so the addon focuses on Fitbod's own data.

## Privacy

`flows.mitm` contains the plaintext refresh_token JWT (subject = user_id,
embeds the user's email) and short-lived access tokens. `data/` is gitignored
— keep it that way. Sample JSON files have `Authorization: Bearer <redacted>`
thanks to the addon's header redaction; the raw `flows.mitm` archive does not.
