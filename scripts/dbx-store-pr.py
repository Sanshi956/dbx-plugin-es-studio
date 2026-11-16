#!/usr/bin/env python3
"""Open the DBX Store candidate PR for ES Studio 0.5.1.

Uses the same gitignored token source as github-publish.py (GITHUB_TOKEN env or
.dbx-dev/github-token.env). Creates a fork of t8y2/dbx-store, adds
candidates/local.es.studio.json on a new branch and opens one PR against main.
Does NOT touch plugins/*.json or catalog/index.json — the store signing
workflow finalizes those after review.
"""

from __future__ import annotations

import base64
import json
import os
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
API = "https://api.github.com"
UPSTREAM = "t8y2/dbx-store"
PLUGIN_ID = "local.es.studio"
BRANCH = "submit-local-es-studio-0.5.1"
_TOKEN = ""


def safe(text: str) -> str:
    return text.replace(_TOKEN, "***") if _TOKEN else text


def api(method: str, path: str, payload=None, allow_404: bool = False, accept_none: bool = False):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        API + path,
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {_TOKEN}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "es-studio-store-pr",
            **({"Content-Type": "application/json"} if payload is not None else {}),
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        if exc.code == 404 and allow_404:
            return {}
        body = exc.read().decode("utf-8", errors="replace")
        raise SystemExit(f"GitHub API {method} {path} -> HTTP {exc.code}: {safe(body[:600])}") from exc


def get_token() -> str:
    token = os.environ.get("GITHUB_TOKEN", "").strip()
    if not token:
        path = ROOT / ".dbx-dev" / "github-token.env"
        if path.exists():
            for line in path.read_text(encoding="utf-8").splitlines():
                line = line.strip()
                if line and not line.startswith("#"):
                    token = line.split("=", 1)[-1].strip().strip('"').strip("'")
                    break
    if not token:
        raise SystemExit("No GitHub token found (.dbx-dev/github-token.env or GITHUB_TOKEN).")
    return token


def main() -> None:
    global _TOKEN
    _TOKEN = get_token()

    me = api("GET", "/user")
    owner = me["login"]
    fork_slug = f"{owner}/dbx-store"
    print("fork:", fork_slug)

    fork = api("GET", f"/repos/{fork_slug}", allow_404=True)
    if not fork.get("full_name"):
        fork = api("POST", f"/repos/{UPSTREAM}/forks", {"name": "dbx-store"})
        print("fork requested")
    for _ in range(30):
        fork = api("GET", f"/repos/{fork_slug}", allow_404=True)
        if fork.get("default_branch"):
            break
        time.sleep(2)
    assert fork.get("default_branch"), "fork not ready"
    print("fork ready:", fork["html_url"])

    main_ref = api("GET", f"/repos/{fork_slug}/git/ref/heads/{fork['default_branch']}")
    base_sha = main_ref["object"]["sha"]
    if not api("GET", f"/repos/{fork_slug}/git/ref/heads/{BRANCH}", allow_404=True):
        api(
            "POST",
            f"/repos/{fork_slug}/git/refs",
            {"ref": f"refs/heads/{BRANCH}", "sha": base_sha},
        )
    print("branch ready:", BRANCH)

    candidate_path = ROOT / "dist" / "marketplace-0.5.1" / "store" / "candidates" / f"{PLUGIN_ID}.json"
    content = base64.b64encode(candidate_path.read_bytes()).decode()
    existing = api(
        "GET", f"/repos/{fork_slug}/contents/candidates/{PLUGIN_ID}.json?ref={BRANCH}", allow_404=True
    )
    payload = {
        "message": f"candidate: {PLUGIN_ID} 0.5.1",
        "content": content,
        "branch": BRANCH,
    }
    if existing.get("sha"):
        payload["sha"] = existing["sha"]
    api("PUT", f"/repos/{fork_slug}/contents/candidates/{PLUGIN_ID}.json", payload)
    print("candidate file committed")

    source = f"https://github.com/Sanshi956/dbx-plugin-es-studio/tree/v0.5.1"
    body = f"""## Plugin submission

- Plugin ID: `{PLUGIN_ID}`
- Version: `0.5.1`
- Publisher ID: `xihonglei` (already registered in `publishers/xihonglei.json`)

## Source repository and tag

Public source: https://github.com/Sanshi956/dbx-plugin-es-studio
Exact immutable tag: `{source}`
Release (unsigned candidate + artifact + release-candidates.json): https://github.com/Sanshi956/dbx-plugin-es-studio/releases/tag/v0.5.1
Candidate target SHA-256 is pinned in `candidates/{PLUGIN_ID}.json`.

## Capabilities and user workflow

ES Studio is a DBX workbench for Elasticsearch cluster administration. It contributes a connection
provider (ES endpoint/credentials via the host connection form), 25 navigation items grouped into
overview, analysis, index management, lifecycle/snapshots/tasks, monitoring and MCP tools, plus an
MCP contribution exposing 45 read-only tools in 9 toolsets. Users create an ES connection, run
Test Connection, then open the workbench tab; all requests go through the Go sidecar.

## Permissions, data access, and network access

- `host.workbench` — tab title and workbench/navigation contributions.
- `host.storage` — small (<1 MiB) per-connection preferences only.
- `host.connections:read` — read the active ES connection endpoint; credentials stay in the host
  Secret Store (`json:"-"` in Go, `binding:"secret"` in the connection form) and are never persisted
  by the plugin.
- `host.events` — react to host environment changes (locale, font size, theme) without reload.

The frontend never connects to Elasticsearch directly. All ES REST calls are issued by the sidecar
over host stdio-jsonl RPC. No third-party hosts, telemetry, or external APIs are contacted. The
external MCP server requires an explicit per-connection grant (DPAPI-sealed token, 30-day default,
revocable); the built-in DBX AI channel uses the same 45 read-only tools without extra setup.

## Native sidecar behavior

Yes: `bin/windows-x64/es-studio-sidecar.exe` (Go, current-host build, vendored SDK). It runs as a
stdio-jsonl child process, opens no listening ports, spawns no external commands, and writes only
rolling monitoring time series (JSONL, 7-day / 20000-point / 8 MiB caps) under `DBX_PLUGIN_DATA_DIR`.
No other filesystem writes and no subprocesses.

## License

MIT

## Homepage or support URL

https://github.com/Sanshi956/dbx-plugin-es-studio

## Confirmation

- [x] The candidate packages are unsigned and match the disclosed source tag.
- [x] I have not included private keys, tokens, credentials, or proprietary data.
- [x] The candidates are listed in `candidates/{PLUGIN_ID}.json`; `plugins/` and `catalog/` were not edited manually.
"""

    existing_pr = api(
        "GET",
        f"/repos/{UPSTREAM}/pulls?head={owner}:{BRANCH}&state=open",
        allow_404=True,
    )
    if isinstance(existing_pr, list) and existing_pr:
        print("PR already open:", existing_pr[0]["html_url"])
        return
    pr = api(
        "POST",
        f"/repos/{UPSTREAM}/pulls",
        {
            "title": f"Add {PLUGIN_ID} 0.5.1 (ES Studio)",
            "head": f"{owner}:{BRANCH}",
            "base": "main",
            "body": body,
        },
    )
    print("PR opened:", pr["html_url"])


if __name__ == "__main__":
    main()
