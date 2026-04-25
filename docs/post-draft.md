# I put my Fitbod stats on a tiny LED display

> _draft — fill in once the build is real_

## Hook

[Photo of the Tronbyt on the desk showing "12.4k lb · streak 6w"]

## Why

- I lift, I track in Fitbod, I want the numbers in my peripheral vision.
- Tronbyt (open-source Tidbyt fork) lives next to my monitor.
- Fitbod has no public API.

## What I found

- Fitbod's backend is Parse Server. Their public GitHub
  ([github.com/Fitbod](https://github.com/Fitbod)) ships a `my_workout`
  sample Parse project that confirms it.
- The Android app uses `X-Parse-Application-Id` / `X-Parse-Client-Key`
  headers on every request, against `parse.fitbod.me` (or whatever the
  current host is — I'm not publishing the keys here).
- Schema is roughly: `Workout` ←pointers← `SingleSet` →pointer→
  `Exercise`. _[fill in real shape after capture]_

## How

- ~400 LOC Go service polls the workout class on a 15-min cadence,
  derives a summary, and serves `stats.json` over LAN.
- Pixlet app fetches it and rotates through 3 frames: weekly volume,
  workout count + streak, last lift.
- Whole thing runs in Docker on the Pi alongside the Tronbyt server.

## Pitch to Fitbod

Open a real read-only API. People who care about their data enough to
reverse-engineer your app are exactly the ones you want to keep.

## Code

[github.com/nsluke/TronBod](https://github.com/nsluke/TronBod)
