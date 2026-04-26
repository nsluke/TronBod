# TronBod

Pulls workout data from Fitbod's private Parse backend and renders summary
stats on a Tronbyt (64×32 RGB LED matrix, open-source Tidbyt fork). Personal
project — no public API exists, so the Parse credentials and class schema
have to be captured with mitmproxy against the Android app.

## Architecture

```
iPhone (Fitbod app)
        │
        ▼  (sync as normal)
Fitbod Parse backend
        ▲
        │  (poll on interval)
fitbod-sync (Go, on Pi)
        │  writes data/stats.json
        ▼
HTTP :8090 /stats.json
        ▲
        │  (60s cache)
Tronbyt server → Pixlet app
```

## Quick start (mock mode)

Test the LED display path before doing any MITM capture. Mock mode
bypasses Parse entirely and serves stats derived from a fixture
snapshot.

```bash
make mock                  # serves on http://localhost:8090
curl localhost:8090/stats.json
make pixlet-serve          # render preview at http://localhost:8080
```

The fixture lives at `sync/stats/testdata/sample_snapshot.json` —
edit it to play with different demo numbers. Mock mode requires no
`.env` and no `classes.yaml`.

## Setup

1. Capture Parse credentials.

   Fitbod ships a Parse Server backend. Confirmed via
   [github.com/Fitbod](https://github.com/Fitbod). To find the app ID,
   client key, base URL, and class schema:

   - Install [mitmproxy](https://docs.mitmproxy.org/stable/) and put its
     CA on an Android device (Fitbod's iOS app is more painful to
     inspect — Android Fitbod is what's been verified to work).
   - Route your phone's traffic through this machine.
   - Run the helper addon, which filters Fitbod traffic and extracts the
     credentials and schema automatically:

     ```bash
     make mitm                      # runs `mitmdump -s tools/mitm-fitbod.py`
     # → open the Fitbod app, log in, view a workout, log a set, Ctrl-C
     cat data/mitm/discovery.json   # app id, client key, base URLs, classes+fields
     ls data/mitm/sample-*.json     # one full sample per Parse class
     ```

   See [`docs/schema-notes.md`](./docs/schema-notes.md) for what we already
   guess about the schema and what to verify on the wire. The runtime code
   does **not** bypass cert pinning — that's a capture-time concern only.

2. Fill in `.env` and `classes.yaml`.

   ```bash
   cp .env.example .env           # then edit
   cp classes.yaml.example classes.yaml   # then edit after MITM
   ```

3. One-shot capture (no HTTP server):

   ```bash
   make capture
   ls data/raw/
   ```

4. Run the service:

   ```bash
   make run            # local
   docker compose up   # on the Pi
   curl localhost:8090/stats.json
   ```

5. Preview the Pixlet app:

   ```bash
   make pixlet-serve
   # open http://localhost:8080
   ```

   When you're happy with it, install via your Tronbyt server's web UI
   pointing at `http://<pi-host>:8090/stats.json`.

## Deploy to a Raspberry Pi

The Dockerfile is multi-arch (the Go binary is built statically, the
distroless base image has an arm64 variant). Build directly on the Pi:

```bash
ssh pi
git clone https://github.com/nsluke/TronBod.git && cd TronBod
cp .env.example .env && $EDITOR .env                     # paste in MITM values
cp classes.yaml.example classes.yaml && $EDITOR classes.yaml
docker compose up -d --build
curl localhost:8090/stats.json
```

If you'd rather build on your dev machine and copy a binary across:

```bash
make build-arm64
scp bin/sync-arm64 pi:/usr/local/bin/tronbod-sync
# then write a small systemd unit on the Pi pointing at it
```

## Environment variables

See [`.env.example`](./.env.example).

## Layout

```
sync/         Go service (Parse client → stats → HTTP server)
pixlet/       Starlark app for the Tronbyt
docs/         Notes + writeup draft
data/         Raw responses + computed stats.json (gitignored)
```

## Notes

- Minimum poll interval is enforced at 5m — please don't hammer Fitbod's
  servers.
- The User-Agent header includes `USER_AGENT_CONTACT` so Fitbod can find
  you if they want this taken down.
