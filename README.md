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

## Setup

1. Capture Parse credentials.

   Fitbod ships a Parse Server backend. Confirmed via
   [github.com/Fitbod](https://github.com/Fitbod). To find the app ID,
   client key, base URL, and class names:

   - Set up [mitmproxy](https://docs.mitmproxy.org/stable/) on a machine
     your phone can route through.
   - Install the mitmproxy CA on an Android device (Fitbod's iOS app is
     more painful to inspect).
   - Open the Fitbod app, log in, view a workout. Watch the requests.
   - The Parse app ID and client key are sent as `X-Parse-Application-Id`
     and `X-Parse-Client-Key` headers on every request.
   - Note the class names (path segments under `/parse/classes/`) and the
     fields you see in responses.

   This repo's `runtime` code does **not** bypass cert pinning — that's a
   capture-time concern only.

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
