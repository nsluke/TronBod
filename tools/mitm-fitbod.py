"""
mitmproxy addon for Fitbod's JSON:API backend.

Filters traffic to Fitbod-owned hosts (anything ending in .fitbod.me),
groups requests by REST resource (e.g. "/api/v3/exercises" → "exercises",
"/api/v3/gyms/24482528" → "gyms"), and writes:

    data/mitm/discovery.json        — per-resource summary
    data/mitm/sample-<resource>.json — first request+response sample

Run via `make mitm`. To also archive raw flows for replay/post-processing:

    mitmdump -s tools/mitm-fitbod.py -w data/mitm/flows.mitm
"""

from collections import defaultdict
from pathlib import Path
import json
import re

from mitmproxy import http, ctx

OUTPUT_DIR = Path("data/mitm")
FITBOD_HOST_SUFFIX = ".fitbod.me"

# Match /api/v3/exercises, /api/v2/devices/12686551, /v1/user/metrics/summary.
RESOURCE_RE = re.compile(r"^/(?:api/)?v\d+/(.+?)/?(?:\?|$)")
# Strip numeric ids so /api/v3/gyms/24482528 collapses to "gyms".
NUMERIC_ID_RE = re.compile(r"/\d+(?=/|$)")

INTERESTING_REQ_HEADERS = {
    "authorization",
    "x-fitbod-version",
    "x-fitbod-build",
    "x-fitbod-platform",
    "x-fitbod-device-id",
    "user-agent",
    "accept",
    "content-type",
}


class FitbodJsonApiProbe:
    def __init__(self):
        self.hosts = defaultdict(int)
        self.endpoints = defaultdict(int)
        self.resources = defaultdict(lambda: {
            "count": 0,
            "methods": defaultdict(int),
            "query_params": defaultdict(int),
            "top_fields": defaultdict(int),
            "data_fields": defaultdict(int),
            "sample_paths": [],
        })
        self.samples = {}
        OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
        ctx.log.info(f"fitbod probe: writing to {OUTPUT_DIR.resolve()}")

    @staticmethod
    def _is_fitbod(host: str) -> bool:
        return host.endswith(FITBOD_HOST_SUFFIX)

    @staticmethod
    def _resource_key(path: str) -> str | None:
        m = RESOURCE_RE.match(path)
        if not m:
            return None
        key = NUMERIC_ID_RE.sub("", m.group(1))
        return key or None

    @staticmethod
    def _safe_headers(headers) -> dict:
        out = {}
        for k, v in headers.items():
            kl = k.lower()
            if kl == "authorization":
                parts = v.split(" ", 1)
                out[k] = (parts[0] + " <redacted>") if len(parts) == 2 else "<redacted>"
            elif kl in INTERESTING_REQ_HEADERS:
                out[k] = v
        return out

    def request(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host
        if not self._is_fitbod(host):
            return
        self.hosts[host] += 1

        path = flow.request.path.split("?", 1)[0]
        self.endpoints[f"{flow.request.method} {path}"] += 1

        key = self._resource_key(path)
        if key is None:
            return
        info = self.resources[key]
        info["count"] += 1
        info["methods"][flow.request.method] += 1
        for k in flow.request.query.keys():
            info["query_params"][k] += 1
        if len(info["sample_paths"]) < 5 and flow.request.path not in info["sample_paths"]:
            info["sample_paths"].append(flow.request.path)

    def response(self, flow: http.HTTPFlow):
        if flow.response is None:
            return
        host = flow.request.pretty_host
        if not self._is_fitbod(host):
            return

        path = flow.request.path.split("?", 1)[0]
        key = self._resource_key(path)
        if key is None:
            return
        info = self.resources[key]

        try:
            data = json.loads(flow.response.get_text() or "")
        except Exception:
            return

        if isinstance(data, dict):
            for k in data.keys():
                info["top_fields"][k] += 1
            inner = data.get("data")
            if isinstance(inner, list):
                for row in inner[:10]:
                    if isinstance(row, dict):
                        for k in row.keys():
                            info["data_fields"][k] += 1
            elif isinstance(inner, dict):
                for k in inner.keys():
                    info["data_fields"][k] += 1
        elif isinstance(data, list):
            for row in data[:10]:
                if isinstance(row, dict):
                    for k in row.keys():
                        info["data_fields"][k] += 1

        if key not in self.samples:
            req_body = None
            if flow.request.method != "GET":
                txt = flow.request.get_text() or ""
                try:
                    req_body = json.loads(txt)
                except Exception:
                    req_body = txt or None

            self.samples[key] = {
                "request": {
                    "method": flow.request.method,
                    "url": flow.request.pretty_url,
                    "headers": self._safe_headers(flow.request.headers),
                    "body": req_body,
                },
                "response": {
                    "status": flow.response.status_code,
                    "headers": self._safe_headers(flow.response.headers),
                    "body": data,
                },
            }

    def done(self):
        summary = {
            "hosts": dict(sorted(self.hosts.items(), key=lambda kv: -kv[1])),
            "endpoints": dict(sorted(self.endpoints.items(), key=lambda kv: -kv[1])),
            "resources": {
                key: {
                    "count": info["count"],
                    "methods": dict(info["methods"]),
                    "query_params": dict(sorted(info["query_params"].items(), key=lambda kv: -kv[1])),
                    "top_fields": dict(sorted(info["top_fields"].items(), key=lambda kv: -kv[1])),
                    "data_fields": dict(sorted(info["data_fields"].items(), key=lambda kv: -kv[1])),
                    "sample_paths": info["sample_paths"],
                }
                for key, info in sorted(self.resources.items())
            },
        }
        out = OUTPUT_DIR / "discovery.json"
        out.write_text(json.dumps(summary, indent=2))
        ctx.log.info(f"wrote {out}")

        for key, sample in self.samples.items():
            safe = key.replace("/", "_")
            p = OUTPUT_DIR / f"sample-{safe}.json"
            p.write_text(json.dumps(sample, indent=2, default=str))
            ctx.log.info(f"wrote {p}")

        print("\n=== Fitbod schema discovery ===", flush=True)
        print(f"Hosts: {sorted(self.hosts.keys())}", flush=True)
        print(f"Resources ({len(self.resources)}): {sorted(self.resources.keys())}", flush=True)
        print(f"Full details: {out} + {OUTPUT_DIR}/sample-*.json", flush=True)


addons = [FitbodJsonApiProbe()]
