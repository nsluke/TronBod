"""
Fitbod Stats — fetches the local fitbod-sync service and rotates through:
  1. Workouts this week + streak
  2. Total volume this week
  3. Last lift (exercise / weight × reps / date)
"""

load("render.star", "render")
load("http.star", "http")
load("encoding/json.star", "json")
load("time.star", "time")
load("schema.star", "schema")

DEFAULT_URL = "http://localhost:8090/stats.json"
FRAME_MS = 4000  # per-frame display time
CACHE_TTL = 60   # http cache, seconds

WHITE = "#fff"
DIM = "#888"
ACCENT = "#ffd23f"   # yellow — headlines
PR_COLOR = "#5cff5c" # green — PRs

FONT_HEAD = "tb-8"               # ~8px chunky
FONT_LABEL = "CG-pixel-3x5-mono" # 5px tiny
FONT_BIG = "6x13"                # tall numerals

def main(config):
    url = config.get("stats_url", DEFAULT_URL)
    resp = http.get(url, ttl_seconds = CACHE_TTL)
    if resp.status_code != 200:
        return []
    stats = resp.json()
    if not stats:
        return []

    frames = [
        frame_workouts(stats),
        frame_volume(stats),
        frame_last_lift(stats),
    ]
    # Drop frames that decided they had nothing to show.
    frames = [f for f in frames if f != None]
    if len(frames) == 0:
        return []

    return render.Root(
        delay = FRAME_MS,
        child = render.Animation(children = frames),
    )

# --- frames ---------------------------------------------------------------

def frame_workouts(stats):
    week = stats.get("this_week", {})
    n = week.get("workouts", 0)
    streak = stats.get("streak_weeks", 0)
    return render.Box(
        padding = 1,
        child = render.Column(
            expanded = True,
            main_align = "space_evenly",
            cross_align = "center",
            children = [
                render.Text(content = "THIS WEEK", font = FONT_LABEL, color = DIM),
                render.Text(content = "%d" % n, font = FONT_BIG, color = ACCENT),
                render.Text(content = "streak: %dw" % streak, font = FONT_LABEL, color = WHITE),
            ],
        ),
    )

def frame_volume(stats):
    v = stats.get("this_week", {}).get("total_volume_lbs", 0)
    return render.Box(
        padding = 1,
        child = render.Column(
            expanded = True,
            main_align = "space_evenly",
            cross_align = "center",
            children = [
                render.Text(content = "VOLUME", font = FONT_LABEL, color = DIM),
                render.Text(content = format_volume(v), font = FONT_BIG, color = ACCENT),
                render.Text(content = "lb this wk", font = FONT_LABEL, color = WHITE),
            ],
        ),
    )

def frame_last_lift(stats):
    lw = stats.get("last_workout")
    if lw == None:
        return None
    hl = lw.get("headline_lift")
    if hl == None:
        return None
    name = (hl.get("exercise", "") or "").upper()
    if name == "":
        return None
    weight = hl.get("weight_lbs", 0)
    reps = hl.get("reps", 0)
    date_str = format_date(lw.get("date", ""))

    is_pr = is_recent_pr(stats, hl)
    big_color = PR_COLOR if is_pr else ACCENT

    return render.Box(
        padding = 1,
        child = render.Column(
            expanded = True,
            main_align = "space_evenly",
            cross_align = "center",
            children = [
                render.Marquee(
                    width = 64,
                    child = render.Text(content = name, font = FONT_LABEL, color = DIM),
                ),
                render.Text(content = "%d×%d" % (int(weight), int(reps)), font = FONT_BIG, color = big_color),
                render.Text(content = date_str, font = FONT_LABEL, color = WHITE),
            ],
        ),
    )

# --- helpers --------------------------------------------------------------

def format_volume(v):
    if v >= 1000:
        thousands = v // 1000
        tenths = (v % 1000) // 100
        return "%d.%dk" % (thousands, tenths)
    return "%d" % v

def format_date(iso):
    if iso == "":
        return ""
    t = time.parse_time(iso)
    if t == None:
        return ""
    return t.format("Jan 2")

def is_recent_pr(stats, hl):
    prs = stats.get("prs_this_month", [])
    if prs == None:
        return False
    name = hl.get("exercise", "")
    weight = hl.get("weight_lbs", 0)
    reps = hl.get("reps", 0)
    for pr in prs:
        if pr.get("exercise", "") == name and pr.get("weight_lbs", 0) == weight and pr.get("reps", 0) == reps:
            return True
    return False

def get_schema():
    return schema.Schema(
        version = "1",
        fields = [
            schema.Text(
                id = "stats_url",
                name = "Stats URL",
                desc = "URL of the fitbod-sync stats.json endpoint on your LAN.",
                icon = "link",
                default = DEFAULT_URL,
            ),
        ],
    )
