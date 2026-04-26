"""
mitmproxy addon for capturing Fitbod's Parse Server traffic.

Filters every flow to Fitbod hosts, extracts the Parse credentials and
class schema, and writes both a discovery summary and per-class sample
request/response pairs.

Usage:
    # Set up mitmproxy and install its CA on your phone first.
    # See: https://docs.mitmproxy.org/stable/concepts-certificates/
    mitmdump -s tools/mitm-fitbod.py

    # Open the Fitbod app, log in, view a workout, log a set.
    # Ctrl-C when you're done.

    cat data/mitm/discovery.json     # app id, client key, base URLs, classes
    ls data/mitm/sample-*.json       # one full sample per class
"""

from collections import defaultdict
from pathlib import Path
import json
import re

from mitmproxy import http, ctx

OUTPUT_DIR = Path("data/mitm")
HOST_NEEDLE = "fitbod"   # any host whose name contains this is captured
PARSE_CLASS_RE = re.compile(r"^/parse/classes/([^/?]+)")
PARSE_LOGIN_RE = re.compile(r"^/parse/login")


class FitbodSchemaProbe:
    def __init__(self):
        self.app_ids = set()
        self.client_keys = set()
        self.base_urls = set()
        self.installation_ids = set()
        self.endpoints = defaultdict(int)
        # class_name -> {"count": N, "fields": {field: count, ...}}
        self.classes = defaultdict(lambda: {"count": 0, "fields": defaultdict(int)})
        # class_name -> first observed (request, response) pair
        self.samples = {}
        OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
        ctx.log.info(f"fitbod probe: writing to {OUTPUT_DIR.resolve()}")

    def _matches(self, host: str) -> bool:
        return HOST_NEEDLE in host.lower()

    def request(self, flow: http.HTTPFlow):
        if not self._matches(flow.request.host):
            return

        h = flow.request.headers
        if h.get("X-Parse-Application-Id"):
            self.app_ids.add(h["X-Parse-Application-Id"])
        if h.get("X-Parse-Client-Key"):
            self.client_keys.add(h["X-Parse-Client-Key"])
        if h.get("X-Parse-Installation-Id"):
            self.installation_ids.add(h["X-Parse-Installation-Id"])

        # Reconstruct the base URL the client sees.
        scheme = flow.request.scheme
        host = flow.request.host
        port = flow.request.port
        default_port = (scheme == "https" and port == 443) or (scheme == "http" and port == 80)
        base = f"{scheme}://{host}" if default_port else f"{scheme}://{host}:{port}"
        self.base_urls.add(base)

        path = flow.request.path.split("?", 1)[0]
        self.endpoints[f"{flow.request.method} {path}"] += 1

        m = PARSE_CLASS_RE.match(path)
        if m:
            self.classes[m.group(1)]["count"] += 1

    def response(self, flow: http.HTTPFlow):
        if not self._matches(flow.request.host):
            return
        if flow.response is None:
            return

        path = flow.request.path.split("?", 1)[0]
        m = PARSE_CLASS_RE.match(path)
        if not m:
            return
        class_name = m.group(1)

        try:
            body = flow.response.get_text() or ""
            data = json.loads(body)
        except Exception:
            return

        # Parse class queries return {"results": [...]}; single-object fetches
        # return the object directly. Handle both.
        rows = []
        if isinstance(data, dict) and "results" in data and isinstance(data["results"], list):
            rows = data["results"]
        elif isinstance(data, list):
            rows = data
        elif isinstance(data, dict):
            rows = [data]

        for row in rows:
            if isinstance(row, dict):
                for k in row.keys():
                    self.classes[class_name]["fields"][k] += 1

        if class_name not in self.samples and rows:
            self.samples[class_name] = {
                "request": {
                    "method": flow.request.method,
                    "path": flow.request.path,
                    "headers": _safe_headers(flow.request.headers),
                },
                "response_status": flow.response.status_code,
                "response_sample": rows[:3],
            }

    def done(self):
        summary = {
            "app_ids": sorted(self.app_ids),
            "client_keys": sorted(self.client_keys),
            "base_urls": sorted(self.base_urls),
            "installation_ids": sorted(self.installation_ids),
            "classes": {
                name: {
                    "count": info["count"],
                    "fields": dict(sorted(info["fields"].items(), key=lambda kv: -kv[1])),
                }
                for name, info in sorted(self.classes.items())
            },
            "endpoints": dict(sorted(self.endpoints.items(), key=lambda kv: -kv[1])),
        }
        path = OUTPUT_DIR / "discovery.json"
        path.write_text(json.dumps(summary, indent=2))
        ctx.log.info(f"wrote {path}")

        for class_name, sample in self.samples.items():
            p = OUTPUT_DIR / f"sample-{class_name}.json"
            p.write_text(json.dumps(sample, indent=2))
            ctx.log.info(f"wrote {p}")

        # Headline summary on stderr — the bit you want to paste into .env.
        print("\n=== Fitbod schema discovery ===", flush=True)
        if self.app_ids:
            print(f"FITBOD_APP_ID={next(iter(self.app_ids))}", flush=True)
        if self.client_keys:
            print(f"FITBOD_CLIENT_KEY={next(iter(self.client_keys))}", flush=True)
        if self.base_urls:
            print(f"FITBOD_BASE_URL={next(iter(self.base_urls))}", flush=True)
        if self.classes:
            print(f"Classes seen: {sorted(self.classes.keys())}", flush=True)
        print("Full details: data/mitm/discovery.json + data/mitm/sample-*.json", flush=True)


def _safe_headers(headers) -> dict:
    """Strip the session token from captured headers — it's a long-lived secret."""
    out = {}
    for k, v in headers.items():
        if k.lower() == "x-parse-session-token":
            out[k] = "<redacted>"
        else:
            out[k] = v
    return out


addons = [FitbodSchemaProbe()]
