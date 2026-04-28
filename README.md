# TronBod

Pulls workout data from Fitbod and renders summary stats on a Tronbyt
(64×32 RGB LED matrix, open-source Tidbyt fork). Personal project — no
public Fitbod API exists, so a refresh-token JWT has to be captured once
via mitmproxy against the Android app.

## Architecture

```
              ┌──────────────────────────────────┐
              │   nautilus.fitbod.me  (catalog)  │
Fitbod        │   metros.fitbod.me    (user)     │
backends ────►│   gympulse / billing / blimp     │
              │   gate-keeper / pyserve          │
              └────────────────┬─────────────────┘
                               │ Cloudflare WAF
                               │ (tls-client w/ Okhttp4Android13 profile)
                               ▼
                         fitbod-sync (Go, on Pi)
                               │ writes data/stats.json
                               ▼
                       HTTP :8090 /stats.json
                               ▲
                               │ (60s cache)
                       Tronbyt server → Pixlet app
```

The sync service mints a host-scoped access_token per backend (every host
has its own `POST /access_token` endpoint that takes the master refresh_token
and returns a host-scoped JWT). Tokens are cached in-memory and re-minted
on 401. Workouts come from `nautilus.fitbod.me/api/v3/workout_data` in one
paginated JSON:API call with the full set/breakdown hierarchy nested inside.

See [`docs/schema-notes.md`](./docs/schema-notes.md) for the wire-level
shape of every endpoint and field.

## Quick start (mock mode)

Test the LED display path before doing any MITM capture. Mock mode bypasses
Fitbod entirely and serves stats derived from a fixture snapshot.

```bash
make mock                  # serves on http://localhost:8090
curl localhost:8090/stats.json
make pixlet-serve          # render preview at http://localhost:8080
```

The fixture lives at `sync/stats/testdata/sample_snapshot.json` — edit it
to play with different demo numbers. Mock mode requires no `.env` and no
`classes.yaml`.

## Setup

### 1. Capture a refresh token

The sync service needs a long-lived (~1 year) refresh-token JWT to mint
short-lived access tokens for each Fitbod backend. The token is issued
when you sign into the Fitbod Android app, and arrives in the
`Authorization` response header of `POST gate-keeper.fitbod.me/users/login`.

Capture it once:

- Run a rooted Android emulator (Google APIs image, arm64) with Magisk +
  the AlwaysTrustUserCerts module so mitmproxy's CA lands in the apex
  cert store.
- Start mitmproxy with `--set 'allow_hosts=\.fitbod\.me'` so Google Play
  Services keeps its own pinned cert chain (Fitbod refuses to load
  otherwise).
- Spawn Fitbod under Frida with `frida-multiple-unpinning` (Fitbod barely
  pins, but the script is harmless and ensures TLS interception works).
- Sign in. Look at the response of `POST /users/login` — copy the
  `Authorization: Bearer <jwt>` value.

`tools/mitm-prep/capture.sh` is a guided runbook for the above. It assumes
a pre-built AVD called `TronBod_MITM` — see the script for setup.

Alternatively, parse `flows.mitm` after a capture session:

```bash
python3 -c '
from mitmproxy.io import FlowReader  # or use mitmdump -nr ...
import sys
'
```

### 2. Fill in `.env` and `classes.yaml`

```bash
cp .env.example .env           # set FITBOD_REFRESH_TOKEN
cp classes.yaml.example classes.yaml
```

### 3. Run a one-shot to verify

```bash
make capture                   # one sync, exits — writes data/stats.json
cat data/stats.json
```

### 4. Run the service

```bash
make run                       # local
docker compose up              # on the Pi
curl localhost:8090/stats.json
```

### 5. Preview the Pixlet app

```bash
make pixlet-serve
# open http://localhost:8080
```

When the rotation looks right, install via your Tronbyt server's web UI
pointing at `http://<pi-host>:8090/stats.json`.

## Deploy to a Raspberry Pi

The Dockerfile is multi-arch (Go binary built statically, distroless base
image has an arm64 variant). Build directly on the Pi:

```bash
ssh pi
git clone https://github.com/nsluke/TronBod.git && cd TronBod
cp .env.example .env && $EDITOR .env
cp classes.yaml.example classes.yaml
docker compose up -d --build
curl localhost:8090/stats.json
```

If you'd rather build on your dev machine and copy a binary across:

```bash
make build-arm64
scp bin/sync-arm64 pi:/usr/local/bin/tronbod-sync
scp .env classes.yaml pi:~/                       # or set env via systemd
# then write a small systemd unit on the Pi pointing at it
```

## Environment variables

See [`.env.example`](./.env.example).

## Layout

```
sync/         Go service: poll → derive stats → HTTP server
  fitbodapi/  HTTP client for Fitbod's REST + JSON:API hosts (tls-client)
  fitbod/     normalize wire shape into typed Workouts/Sets/Exercises
  stats/      week summary, streak, last-workout headline, PRs
  server/     /stats.json + /healthz
pixlet/       Starlark app for the Tronbyt
docs/         schema-notes.md (wire schema, captured)
data/         Raw responses + computed stats.json (gitignored)
tools/        mitmproxy schema-probe addon + capture rig
```

## Notes

- Minimum poll interval is enforced at 5m — please don't hammer Fitbod.
- All Fitbod hosts sit behind Cloudflare's bot WAF. Stock Go HTTP clients
  get blocked at layer 7. The sync uses
  [`bogdanfinn/tls-client`](https://github.com/bogdanfinn/tls-client) with
  the `Okhttp4Android13` profile to mimic the real Fitbod Android client's
  TLS + HTTP/2 fingerprint. If Cloudflare ever updates and starts blocking
  this profile, bump to a newer one in `sync/fitbodapi/client.go`.
- The refresh token expires ~1 year after issue. When it does, every
  `/access_token` call will start 401-ing. Re-capture and update `.env`.
