#!/usr/bin/env python
"""Probe one Elasticsearch distribution with the exact calls this plugin makes.

Why this exists: the plugin was written against 8.19 and every shape assumption
(mapping layout, _cat column names, which endpoints 404, what a timeout looks
like) is a guess until a real server of that version answers. This script starts
one distribution, replays the UI's request set, and prints what came back, so
compatibility claims are evidence instead of documentation archaeology.

Usage:
    python scripts/version-probe.py --home D:/tmp/es-versions/elasticsearch-6.8.23 --port 19202
    python scripts/version-probe.py --home ... --port 19202 --keep   # leave it running

Only one distribution at a time: each one is 500 MB-1 GB of RSS and this is a
16 GB workstation that also runs the DBX host.
"""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request

NO_PROXY = {"http": None, "https": None}


def http(method: str, url: str, body: dict | None = None, timeout: int = 30) -> tuple[int, object]:
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=timeout) as resp:
            raw = resp.read()
            status = resp.status
    except urllib.error.HTTPError as err:
        raw = err.read()
        status = err.code
    except Exception as err:  # connection refused while booting
        return 0, {"transport": str(err)}
    try:
        return status, json.loads(raw)
    except Exception:
        return status, raw.decode("utf-8", "replace")[:400]


def write_config(home: pathlib.Path, port: int, root: pathlib.Path, major: int) -> pathlib.Path:
    data = root / "data"
    logs = root / "logs"
    for path in (data, logs):
        if path.exists():
            shutil.rmtree(path)
        path.mkdir(parents=True)
    lines = [
        f"cluster.name: probe-{major}x",
        f"node.name: probe-{major}x",
        f"path.data: {data.as_posix()}",
        f"path.logs: {logs.as_posix()}",
        "network.host: 127.0.0.1",
        f"http.port: {port}",
        f"transport.port: {port + 1000}",
        "xpack.security.enabled: false",
        "xpack.ml.enabled: false",
        "action.destructive_requires_name: false",
    ]
    if major <= 6:
        # 6.x has no discovery.type; single-node comes from zen discovery.
        lines += ["discovery.zen.minimum_master_nodes: 1", "discovery.zen.ping.unicast.hosts: []"]
    else:
        lines += ["discovery.type: single-node"]
    if major >= 8:
        lines += ["xpack.security.enrollment.enabled: false"]
    config = home / "config" / "elasticsearch.yml"
    config.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return logs


def start(home: pathlib.Path, port: int, root: pathlib.Path, major: int) -> subprocess.Popen:
    logs = write_config(home, port, root, major)
    env = dict(os.environ)
    env["ES_JAVA_OPTS"] = "-Xms512m -Xmx512m"
    env["NO_PROXY"] = "127.0.0.1,localhost"
    if major <= 6:
        # 6.8 ships without a bundled JDK; it needs Java 8-11.
        env["JAVA_HOME"] = "D:/software/dev/Java/jdk8"
    exe = home / "bin" / "elasticsearch.bat"
    print(f"starting {home.name} on :{port} (logs in {logs})", flush=True)
    return subprocess.Popen([str(exe)], env=env, cwd=str(home), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def wait_up(base: str, timeout: int = 180) -> dict:
    deadline = time.time() + timeout
    while time.time() < deadline:
        status, body = http("GET", base + "/")
        if status == 200 and isinstance(body, dict) and "version" in body:
            return body
        time.sleep(2)
    raise SystemExit(f"{base} did not come up within {timeout}s")


def dig(body: object, *path: str) -> object:
    node = body
    for key in path:
        if isinstance(node, dict):
            node = node.get(key)
        elif isinstance(node, list) and key.isdigit() and len(node) > int(key):
            node = node[int(key)]
        else:
            return None
    return node


def check(label: str, status: int, body: object, must: list[str], note: str = "") -> dict:
    present = [path for path in must if dig(body, *path.split(".")) is not None]
    missing = [path for path in must if path not in present]
    ok = status < 300 and not missing
    print(f"  {'PASS' if ok else 'FAIL'}  {label:<34} HTTP {status:<4} {('missing: ' + ', '.join(missing)) if missing else note}")
    return {"label": label, "status": status, "ok": ok, "missing": missing, "note": note}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--home", required=True)
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--root", default="")
    ap.add_argument("--keep", action="store_true")
    args = ap.parse_args()

    home = pathlib.Path(args.home).resolve()
    root = pathlib.Path(args.root) if args.root else pathlib.Path("D:/tmp/es-probe") / home.name
    root.mkdir(parents=True, exist_ok=True)
    major = int(home.name.split("-")[1].split(".")[0])

    proc = start(home, args.port, root, major)
    base = f"http://127.0.0.1:{args.port}"
    report: dict[str, object] = {"distribution": home.name, "port": args.port, "checks": []}
    try:
        info = wait_up(base)
        version = dig(info, "version", "number")
        print(f"\n{home.name} → version {version}\n")
        report["version"] = version

        checks = report["checks"]
        assert isinstance(checks, list)

        def add(*a, **kw):
            checks.append(check(*a, **kw))

        st, body = http("GET", base + "/_cluster/health")
        add("GET /_cluster/health", st, body, ["status", "number_of_nodes", "active_shards", "unassigned_shards"])

        st, body = http("GET", base + "/_cluster/stats")
        add("GET /_cluster/stats", st, body, ["indices.count", "nodes.count.total", "nodes.jvm.mem.heap_max_in_bytes", "nodes.fs.total_in_bytes"])

        st, body = http("GET", base + "/_cluster/settings?flat_settings=true")
        add("GET /_cluster/settings", st, body, ["persistent", "transient"])

        st, body = http("GET", base + "/_cat/nodes?format=json&h=name,node.role,master,heap.percent,cpu,disk.used_percent")
        role_ok = isinstance(body, list) and body and "node.role" in body[0]
        master_ok = isinstance(body, list) and body and "master" in body[0]
        add("GET /_cat/nodes (role+master)", st if role_ok and master_ok else 599, body if role_ok and master_ok else {},
            ["0.name"], "node.role/master columns present" if role_ok and master_ok else "column missing")

        st, body = http("GET", base + "/_cat/shards?format=json&h=index,shard,prirep,state,unassigned.reason")
        add("GET /_cat/shards (unassigned.reason)", st, body, [], "request accepted")

        st, body = http("PUT", base + "/probe-idx/_doc/1?refresh=true", {"msg": "hello", "n": 7, "when": "2026-01-01T00:00:00Z"})
        add("PUT /probe-idx/_doc/1", st, body, ["result"])

        st, body = http("GET", base + "/probe-idx/_mapping")
        mappings = dig(body, "probe-idx", "mappings") or {}
        typed = "properties" not in mappings and any(isinstance(v, dict) and "properties" in v for v in mappings.values())
        add("GET /probe-idx/_mapping", st, body, ["probe-idx.mappings"],
            "typed (_doc/…) layout" if typed else "typeless layout")
        report["mappingLayout"] = "typed" if typed else "typeless"

        st, body = http("GET", base + "/_data_stream")
        add("GET /_data_stream", st, body, ["data_streams"] if st < 300 else [],
            "empty list" if st < 300 else "endpoint absent on this version")

        st, body = http("GET", base + "/_index_template")
        add("GET /_index_template", st, body, ["index_templates"] if st < 300 else [],
            "composable templates" if st < 300 else "endpoint absent on this version")

        st, body = http("GET", base + "/_component_template")
        add("GET /_component_template", st, body, ["component_templates"] if st < 300 else [],
            "present" if st < 300 else "endpoint absent on this version")

        st, body = http("GET", base + "/_ilm/status")
        add("GET /_ilm/status", st, body, ["operation_mode"] if st < 300 else [],
            "present" if st < 300 else "endpoint absent on this version")

        st, body = http("GET", base + "/_snapshot")
        add("GET /_snapshot", st, body, [], "request accepted")

        st, body = http("GET", base + "/_tasks?group_by=none&detailed=true")
        shape = "flat tasks[]" if isinstance(dig(body, "tasks"), list) else "nodes{}.tasks{}" if dig(body, "nodes") else "unknown"
        add("GET /_tasks?group_by=none", st, body, ["tasks"] if isinstance(dig(body, "tasks"), list) else ["nodes"], shape)

        st, body = http("GET", base + "/_alias")
        add("GET /_alias", st, body, [], "request accepted")

        st, body = http("GET", base + "/_all/_settings?flat_settings=true&name=index.number_of_replicas,index.refresh_interval")
        add("GET /_all/_settings (name= filter)", st, body, [], "request accepted")

        st, body = http("POST", base + "/_analyze", {"text": "Quick brown fox", "analyzer": "standard"})
        add("POST /_analyze", st, body, ["tokens"])

        st, body = http("GET", base + "/_nodes/stats")
        add("GET /_nodes/stats", st, body, ["nodes"])

        # 9.x removed _knn_search; 8.x has it. Record, do not gate.
        st, body = http("POST", base + "/_knn_search", {"knn": {"field": "n", "query_vector": [1], "k": 1}})
        report["knnSearch"] = st

        # A rejected write (to a closed index) is how the UI sees 400s.
        http("PUT", base + "/probe-closed")
        http("POST", base + "/probe-closed/_close")
        st, body = http("PUT", base + "/probe-closed/_doc/1", {"x": 1})
        report["closedIndexWrite"] = {"status": st, "reason": dig(body, "error", "reason")}
        print(f"  INFO  write to closed index            HTTP {st} {dig(body, 'error', 'reason')}")
    finally:
        out = root / "probe.json"
        out.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"\nreport: {out}")
        if args.keep:
            print(f"left running on :{args.port} (pid {proc.pid})")
            return 0
        proc.terminate()
        try:
            proc.wait(timeout=40)
        except subprocess.TimeoutExpired:
            proc.kill()
        print("stopped")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
