# Build: Fitbod → Tronbyt pipeline

You're starting in an empty directory. Scaffold the full repo, then implement.

## What this is

A personal pipeline that pulls my workout history from Fitbod's private Parse
backend and renders summary stats on my Tronbyt (64×32 LED matrix, open-source
Tidbyt fork) running in Docker on a Raspberry Pi.

Fitbod has no public API, but their backend is Parse Server (confirmed via
github.com/Fitbod). I'll capture the Parse app ID, client key, and class
schema myself with mitmproxy and provide them via `.env`. **Do not guess or
hardcode any Parse credentials, class names, or field names.** Make those
configurable; fail fast with a clear error if missing.

## Architecture

```
iPhone (Fitbod) → Fitbod Parse backend ← Go sync service (on Pi)
                                           ↓ writes data/stats.json
                                         HTTP server :8090
                                           ↓
                                         Tronbyt server → Pixlet app
                                         (fetches /stats.json)
```

## Repo layout

```
.
├── README.md
├── .env.example
├── .gitignore                  # .env, data/, .session, docs/reverse-engineering.md
├── docker-compose.yml
├── Makefile                    # `make capture` runs one sync, no HTTP server
├── sync/                       # Go 1.22+, stdlib + caarlos0/env + log/slog
│   ├── go.mod
│   ├── main.go
│   ├── parse/                  # thin Parse REST client (login, query, session)
│   ├── fitbod/                 # class config, query builders
│   ├── stats/                  # summary derivation, with testdata fixtures
│   └── server/                 # serves stats.json + /healthz
├── pixlet/
│   ├── fitbod_stats.star
│   └── manifest.yaml           # supports2x: true
└── docs/
    ├── reverse-engineering.md  # gitignored, my private notes
    └── post-draft.md           # writeup skeleton (hook, motivation, schema
                                #   findings, pitch to Fitbod, code link)
```

## Go service requirements

- Config from env: `FITBOD_APP_ID`, `FITBOD_CLIENT_KEY`, `FITBOD_BASE_URL`,
  `FITBOD_EMAIL`, `FITBOD_PASSWORD`, `POLL_INTERVAL` (default 15m, min 5m),
  `HTTP_PORT` (default 8090), `USER_AGENT_CONTACT` (a GitHub URL).
- Login via `POST {BASE_URL}/parse/login` with Parse headers; persist session
  token to `.session` (chmod 600); re-login on 401/403.
- Polls Parse classes defined in a `classes.yaml` filled in post-MITM.
  Support where/limit/order/include query params.
- Caches raw responses to `data/raw/{class}-{ts}.json` so the stats layer
  can be iterated on without re-hitting Fitbod.
- Writes `data/stats.json` shaped like:
  ```json
  {
    "updated_at": "ISO8601",
    "this_week": {
      "workouts": 0,
      "total_volume_lbs": 0,
      "total_sets": 0,
      "total_duration_min": 0
    },
    "streak_weeks": 0,
    "last_workout": {
      "date": "ISO8601",
      "duration_min": 0,
      "headline_lift": {"exercise": "", "weight_lbs": 0, "reps": 0}
    },
    "prs_this_month": [
      {"exercise": "", "weight_lbs": 0, "reps": 0, "date": "ISO8601"}
    ]
  }
  ```
  Headline-lift heuristic is pluggable. Default: top set of the compound
  with highest e1RM that session.
- Serves `/stats.json` and `/healthz` on `HTTP_PORT`.
- Structured slog JSON to stdout. One info line per poll cycle.
- Exponential backoff on errors. Polite User-Agent including
  `USER_AGENT_CONTACT`.
- Unit tests for the stats layer using fixture JSON in
  `sync/stats/testdata/`. **Do not** test the live Parse client.

## Pixlet app requirements

- Fetch `http://localhost:8090/stats.json` with a 60s `http.get` cache.
- 3-frame rotation (~4s each):
  1. Workouts this week (big number) + "streak: Nw" below
  2. Total volume this week, formatted "48.2k lb"
  3. Last lift: "SQUAT 245×5" + small date tag
- Empty/missing stats → `return []`.
- Two-color high-contrast palette; `tb-8` for headlines,
  `CG-pixel-3x5-mono` for labels. Accent color for PRs.

## Constraints

- No "bypass cert pinning" logic in runtime code — that's a capture-time
  concern only.
- No secrets in code or examples beyond placeholder values in `.env.example`.
- README covers setup + env vars + a brief MITM section that *links to*
  mitmproxy docs rather than tutorializing.
- Don't add features I didn't ask for. No metrics endpoint, no web UI,
  no auth on `:8090` (LAN-only).
