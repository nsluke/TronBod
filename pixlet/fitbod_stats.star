"""
Fitbod Stats — fetches stats.json from the local fitbod-sync service and
renders all the headline numbers on a single static 64×32 screen.

Layout:
    +----------+----------+----------+   8px  (chunky number)
    |    3     |    11    |   173k   |   5px  (tiny label)
    |   WK     |   STK    |    LB    |
    +-------------------------------+    1px  separator
    |  LEG PRESS  (marquee if long) |    5px
    |  180 × 10  ★                  |    8px  (PR star + green if recent PR)
    |  Apr 28                       |    5px  (dim)
    +-------------------------------+
"""

load("http.star", "http")
load("render.star", "render")
load("schema.star", "schema")
load("time.star", "time")

DEFAULT_URL = "http://localhost:8090/stats.json"
CACHE_TTL = 60

WHITE = "#fff"
DIM = "#888"
ACCENT = "#ffd23f"  # yellow — headline numbers
PR_COLOR = "#5cff5c"  # green — recent PR
SEP_COLOR = "#222"  # dark separator line

FONT_BIG = "tb-8"  # ~8px chunky numerals
FONT_LABEL = "CG-pixel-3x5-mono"  # 3×5 tiny text

def main(config):
    url = config.get("stats_url", DEFAULT_URL)
    resp = http.get(url, ttl_seconds = CACHE_TTL)
    if resp.status_code != 200:
        return []
    stats = resp.json()
    if not stats:
        return []

    return render.Root(
        child = render.Column(
            expanded = True,
            main_align = "start",
            cross_align = "center",
            children = [
                top_stats(stats),
                separator(),
                last_lift(stats),
            ],
        ),
    )

# --- top: workouts / streak / volume --------------------------------------

def top_stats(stats):
    week = stats.get("this_week", {})
    n_workouts = week.get("workouts", 0)
    volume = week.get("total_volume_lbs", 0)
    streak = stats.get("streak_weeks", 0)

    return render.Row(
        expanded = True,
        main_align = "space_evenly",
        cross_align = "center",
        children = [
            stat_block("%d" % n_workouts, "WK"),
            stat_block("%d" % streak, "STK"),
            stat_block(format_volume(volume), "LB"),
        ],
    )

def stat_block(value, label):
    return render.Column(
        cross_align = "center",
        children = [
            render.Text(content = value, font = FONT_BIG, color = ACCENT),
            render.Text(content = label, font = FONT_LABEL, color = DIM),
        ],
    )

# --- bottom: last lift -----------------------------------------------------

def last_lift(stats):
    lw = stats.get("last_workout")
    if lw == None:
        return render.Box(child = render.Text(content = "no lift yet", font = FONT_LABEL, color = DIM))
    hl = lw.get("headline_lift")
    if hl == None:
        return render.Box(child = render.Text(content = "no lift yet", font = FONT_LABEL, color = DIM))

    name = (hl.get("exercise", "") or "").upper()
    weight = int(hl.get("weight_lbs", 0))
    reps = int(hl.get("reps", 0))
    date_str = format_date(lw.get("date", ""))
    is_pr = is_recent_pr(stats, hl)
    big_color = PR_COLOR if is_pr else ACCENT
    star = " *" if is_pr else ""

    return render.Column(
        cross_align = "center",
        children = [
            render.Marquee(
                width = 64,
                child = render.Text(content = name, font = FONT_LABEL, color = WHITE),
            ),
            render.Text(
                content = "%d x %d%s" % (weight, reps, star),
                font = FONT_BIG,
                color = big_color,
            ),
            render.Text(content = date_str, font = FONT_LABEL, color = DIM),
        ],
    )

# --- helpers --------------------------------------------------------------

def separator():
    # 1px-tall dark bar across the full width to visually divide the two halves.
    return render.Box(width = 64, height = 1, color = SEP_COLOR)

def format_volume(v):
    if v >= 1000:
        thousands = v // 1000
        tenths = (v % 1000) // 100
        if tenths == 0:
            return "%dk" % thousands
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
