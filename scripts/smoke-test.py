#!/usr/bin/env python3
"""ES Studio P1 protocol smoke test (docs/01-PLAN.md T12/T13/T16/T17).

Drives the REAL Sidecar executable over stdio-jsonl. A small in-process HTTP
server supplies deterministic upstream error shapes (400/401/403/404/500,
timeout, oversized body, auth echo). With --es-url it also runs a real-cluster
integration group against the local Elasticsearch 8.19.23 instance.

The mock server is a TEST DOUBLE for error paths only. It is never used as
product data; the Workbench reads real ES APIs through the Sidecar.

Usage:
  python scripts/smoke-test.py --sidecar bin/es-studio-sidecar.exe \
      --es-url http://127.0.0.1:19200
"""

import argparse
import concurrent.futures
import http.server
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time

# The plugin version is not hardcoded here: it must match what the Sidecar
# reports, and both come from manifest.json / backend/main.go.
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
with open(os.path.join(ROOT, "manifest.json"), encoding="utf-8") as fh:
    EXPECTED_VERSION = json.load(fh).get("version")
import urllib.request
from http.server import ThreadingHTTPServer

SECRET = "s3cr3t-smoke-value"
ROOT_DOC = {
    "name": "es-studio-smoke-node",
    "cluster_name": "es-studio-smoke",
    "cluster_uuid": "smoke-uuid",
    "version": {"number": "8.19.23", "distribution": "elasticsearch"},
    "tagline": "You Know, for Search",
}


class MockES(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def _send(self, code, payload, raw=None):
        body = raw if raw is not None else json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.command != "HEAD":
            try:
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionAbortedError, ConnectionResetError):
                # Expected when the Sidecar deliberately times out / aborts.
                pass

    def do_GET(self):
        self._dispatch()

    def do_HEAD(self):
        self._dispatch()

    def do_POST(self):
        self._read_body()
        self._dispatch()

    def do_PUT(self):
        self._read_body()
        self._dispatch()

    def do_DELETE(self):
        self._read_body()
        self._dispatch()

    def _read_body(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        if length:
            self.rfile.read(length)

    def _dispatch(self):
        path = self.path
        clean = path.split("?", 1)[0]
        if clean == "/":
            return self._send(200, ROOT_DOC)
        if clean == "/_cluster/health":
            return self._send(200, {"cluster_name": "es-studio-smoke", "status": "green", "number_of_nodes": 1})
        if clean == "/echo":
            query = path.split("?", 1)[1] if "?" in path else ""
            return self._send(200, {
                "method": self.command, "path": clean, "query": query,
                "has_auth": bool(self.headers.get("Authorization")),
            })
        if clean.startswith("/status/"):
            try:
                code = int(clean.rsplit("/", 1)[1])
            except ValueError:
                code = 400
            return self._send(code, {"status": code, "message": "upstream-error"})
        if clean == "/slow":
            time.sleep(2)
            return self._send(200, {"late": True})
        if clean == "/large":
            return self._send(200, None, raw=b"x" * 9_000_000)
        if clean.endswith(("/_search", "/_count", "/_analyze", "/_msearch")):
            return self._send(200, {"took": 1, "hits": {"total": {"value": 0}, "hits": []}})
        return self._send(404, {"error": "not_found", "path": clean})


class Sidecar:
    """JSON-RPC client with a reader thread so out-of-order / concurrent
    responses are matched by id (the Go SDK dispatches each call in a goroutine)."""

    def __init__(self, executable, env_extra=None):
        env = dict(os.environ)
        if env_extra:
            env.update(env_extra)
        self.proc = subprocess.Popen(
            [executable], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, encoding="utf-8", errors="replace", bufsize=1,
            env=env,
        )
        self.responses = {}
        self.cv = threading.Condition()
        self.write_lock = threading.Lock()
        self.next_id = 1
        self.stderr_lines = []
        self.all_responses = []
        threading.Thread(target=self._read_stdout, daemon=True).start()
        threading.Thread(target=self._read_stderr, daemon=True).start()

    def _read_stdout(self):
        for line in self.proc.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            self.all_responses.append(obj)
            with self.cv:
                self.responses[obj.get("id")] = obj
                self.cv.notify_all()

    def _read_stderr(self):
        for line in self.proc.stderr:
            self.stderr_lines.append(line.rstrip())

    def call(self, method, params=None, timeout=20):
        with self.cv:
            rid = self.next_id
            self.next_id += 1
        frame = {"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}}
        with self.write_lock:
            self.proc.stdin.write(json.dumps(frame) + "\n")
            self.proc.stdin.flush()
        deadline = time.time() + timeout
        with self.cv:
            while rid not in self.responses:
                remaining = deadline - time.time()
                if remaining <= 0:
                    raise TimeoutError(f"no response for {method} (id={rid})")
                self.cv.wait(remaining)
            return self.responses.pop(rid)

    def raw(self, line):
        with self.write_lock:
            self.proc.stdin.write(line + "\n")
            self.proc.stdin.flush()

    def close(self):
        try:
            self.proc.terminate()
            self.proc.wait(timeout=5)
        except Exception:
            self.proc.kill()


class Checks:
    def __init__(self):
        self.passed = 0
        self.failed = []

    def section(self, name):
        print(f"\n--- {name} ---")

    def check(self, name, condition, detail=""):
        if condition:
            self.passed += 1
            print(f"  PASS  {name}")
        else:
            self.failed.append(name)
            print(f"  FAIL  {name}  {detail}")

    def result(self):
        print(f"\nRESULT: {self.passed} passed, {len(self.failed)} failed")
        if self.failed:
            for name in self.failed:
                print("  - " + name)
        return 1 if self.failed else 0


def err_code(response):
    return (response.get("error") or {}).get("code")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sidecar", default="bin/es-studio-sidecar.exe")
    ap.add_argument("--es-url", default="http://127.0.0.1:19200")
    ap.add_argument("--skip-real-es", action="store_true")
    args = ap.parse_args()

    if not os.path.isfile(args.sidecar):
        print(f"ERROR: sidecar not found: {args.sidecar}")
        return 2

    mock = ThreadingHTTPServer(("127.0.0.1", 0), MockES)
    mock.daemon_threads = True
    threading.Thread(target=mock.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{mock.server_address[1]}"
    print(f"mock upstream: {base}")
    print(f"sidecar: {args.sidecar}")

    c = Checks()
    # Keep the collector's JSONL series out of the shared temp dir: browser tests
    # run against the same default location and would see smoke samples mixed in.
    data_dir = tempfile.mkdtemp(prefix="es-studio-smoke-")
    s = Sidecar(args.sidecar, env_extra={"DBX_PLUGIN_DATA_DIR": data_dir})
    try:
        c.section("SDK protocol")
        r = s.call("plugin/initialize", {"host": {"protocolVersions": [1]}})
        res = r.get("result", {})
        c.check("initialize protocolVersion=1", res.get("protocolVersion") == 1)
        c.check("initialize plugin.id", (res.get("plugin") or {}).get("id") == "local.es.studio")
        c.check("initialize plugin.version", (res.get("plugin") or {}).get("version") == EXPECTED_VERSION,
                f"got {(res.get('plugin') or {}).get('version')}, manifest says {EXPECTED_VERSION}")
        c.check("es/ping", (s.call("es/ping").get("result") or {}).get("pong") is True)
        c.check("unknown method -> -32601", err_code(s.call("no/such/method")) == -32601)

        c.section("connection lifecycle (mock upstream)")
        t = s.call("connection/test", {"direct": {"url": base}}).get("result", {})
        c.check("connection/test reads real version", t.get("version") == "8.19.23", str(t))
        c.check("connection/test reads cluster_name", t.get("cluster_name") == "es-studio-smoke", str(t))
        c.check("connection/test detects engine", t.get("engine") == "elasticsearch", str(t))
        c.check("connection/test capabilities", (t.get("capabilities") or {}).get("dataStreams") is True, str(t))
        for i in range(2):
            r = s.call("connection/connect", {"connection_id": "smoke", "direct": {"url": base}})
            c.check(f"connect idempotent #{i + 1}", (r.get("result") or {}).get("connected") is True, str(r))

        c.section("es/request")
        def req(path, method="GET", extra=None, direct=None):
            params = {"connection_id": "smoke", "method": method, "path": path}
            if direct:
                params["direct"] = direct
            if extra:
                params.update(extra)
            return s.call("es/request", params)

        r = req("/_cluster/health").get("result", {})
        c.check("GET health -> 200", r.get("status") == 200, str(r))
        c.check("GET health body preserved", (r.get("body") or {}).get("status") == "green", str(r))
        c.check("duration measured", isinstance(r.get("duration_ms"), (int, float)), str(r))
        r = req("/echo?level=indices").get("result", {})
        c.check("query string preserved", (r.get("body") or {}).get("query") == "level=indices", str(r))
        r = req("/echo", method="HEAD").get("result", {})
        c.check("HEAD -> 200", r.get("status") == 200, str(r))
        r = req("/echo", direct={"url": base, "username": "admin", "password": SECRET}).get("result", {})
        c.check("Basic auth header injected", (r.get("body") or {}).get("has_auth") is True, str(r))
        for code in (400, 401, 403, 404, 500):
            r = req(f"/status/{code}").get("result", {})
            c.check(f"upstream {code} passed through", r.get("status") == code and
                    (r.get("body") or {}).get("message") == "upstream-error", str(r))
        c.check("default 404 passthrough", req("/missing").get("result", {}).get("status") == 404)

        c.section("security guards")
        c.check("unsafe path -> -32004", err_code(req("/_cat/../_cluster/settings")) == -32004)
        c.check("absolute URL -> -32004", err_code(req("http://evil.example/_search")) == -32004)
        ro = {"url": base, "read_only": True}
        c.check("read-only rejects POST /_doc -> -32003",
                err_code(req("/orders/_doc", method="POST", direct=ro)) == -32003)
        c.check("read-only allows GET", req("/_cluster/health", direct=ro).get("result", {}).get("status") == 200)
        c.check("read-only allows safe POST /_search",
                req("/orders/_search", method="POST", direct=ro).get("result", {}).get("status") == 200)
        c.check("timeout -> -32010",
                err_code(req("/slow", direct={"url": base, "timeout_secs": 1})) == -32010)
        c.check("oversized body -> -32011", err_code(req("/large")) == -32011)
        c.check("no secret in any response",
                SECRET not in json.dumps(s.all_responses))
        c.check("no secret on stderr", SECRET not in "\n".join(s.stderr_lines))

        c.section("batch / malformed input / concurrency")
        b = s.call("es/batch", {"connection_id": "smoke", "requests": [
            {"method": "GET", "path": "/_cluster/health"},
            {"method": "GET", "path": "/echo"},
            {"method": "GET", "path": "/status/404"},
        ]}).get("result", {})
        rows = b.get("results", [])
        c.check("batch returns 3 results", len(rows) == 3, str(b))
        c.check("batch statuses 200/200/404",
                [x.get("status") for x in rows if isinstance(x, dict)] == [200, 200, 404], str(rows))
        s.raw("this is not json")
        c.check("sidecar survives malformed frame",
                (s.call("es/ping").get("result") or {}).get("pong") is True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            futures = [pool.submit(s.call, "es/ping") for _ in range(8)]
            c.check("8 concurrent requests all answered",
                    all((f.result().get("result") or {}).get("pong") is True for f in futures))
        for i in range(2):
            r = s.call("connection/disconnect", {"connection_id": "smoke"})
            c.check(f"disconnect idempotent #{i + 1}", "error" not in r, str(r))

        if not args.skip_real_es:
            c.section("REAL Elasticsearch integration (requirement 13.2)")
            try:
                with urllib.request.urlopen(args.es_url + "/", timeout=4) as resp:
                    real_root = json.loads(resp.read().decode("utf-8"))
            except Exception as exc:
                print(f"  SKIP real ES unavailable: {exc}")
            else:
                version = (real_root.get("version") or {}).get("number")
                c.check(f"real cluster reachable ({version})", bool(version))
                rd = {"url": args.es_url}
                t = s.call("connection/test", {"direct": rd}).get("result", {})
                c.check("real version == 8.19.23", t.get("version") == "8.19.23", str(t))
                c.check("real cluster_name", t.get("cluster_name") == "es-studio-test", str(t))
                s.call("connection/connect", {"connection_id": "real-es", "direct": rd})

                def real(path, method="GET", body=None):
                    p = {"connection_id": "real-es", "method": method, "path": path}
                    if body is not None:
                        p["body"] = body
                    return s.call("es/request", p).get("result", {})

                health = real("/_cluster/health")
                c.check("real /_cluster/health -> 200", health.get("status") == 200, str(health))
                c.check("real health status", (health.get("body") or {}).get("status") in ("green", "yellow", "red"), str(health))
                c.check("real /_cat/indices -> 200", real("/_cat/indices?format=json").get("status") == 200)
                c.check("real /_nodes -> 200", real("/_nodes").get("status") == 200)
                index = "es-studio-smoke-" + str(int(time.time()))
                created = real("/" + index, "PUT", {"settings": {"number_of_shards": 1, "number_of_replicas": 0}})
                c.check("real create index", created.get("status") == 200, str(created))
                indexed = real("/" + index + "/_doc?refresh=true", "POST", {"title": "smoke", "ok": True})
                c.check("real index document", indexed.get("status") in (200, 201), str(indexed))
                found = real("/" + index + "/_search", "POST", {"query": {"match_all": {}}})
                hits = (((found.get("body") or {}).get("hits") or {}).get("total") or {}).get("value", 0)
                c.check("real search finds document", hits >= 1, str(found))
                deleted = real("/" + index, "DELETE")
                c.check("real delete index (cleanup)", deleted.get("status") == 200, str(deleted))

                # ---- P2 endpoint coverage: the Workbench pages read the same APIs ----
                nodes = real("/_cat/nodes?format=json")
                c.check("P2 /_cat/nodes -> 200", nodes.get("status") == 200, str(nodes))
                node_list = nodes.get("body") or []
                c.check("P2 /_cat/nodes has >=1 entry", isinstance(node_list, list) and len(node_list) >= 1, str(nodes))
                c.check("P2 /_nodes detail -> 200", real("/_nodes").get("status") == 200)
                shards = real("/_cat/shards?format=json&bytes=b")
                c.check("P2 /_cat/shards -> 200", shards.get("status") == 200, str(shards))
                c.check("P2 /_cat/shards is a list", isinstance(shards.get("body"), list), str(shards))
                aliases = real("/_cat/aliases?format=json")
                c.check("P2 /_cat/aliases -> 200", aliases.get("status") == 200, str(aliases))
                analyze = real("/_analyze", "POST", {"text": "the quick brown fox", "analyzer": "standard"})
                c.check("P2 /_analyze -> 200", analyze.get("status") == 200, str(analyze))
                tokens = (analyze.get("body") or {}).get("tokens") or []
                c.check("P2 /_analyze returns tokens", isinstance(tokens, list) and len(tokens) >= 1, str(analyze))

                # ---- P3 endpoint coverage: governance / ops pages read these ----
                settings = real("/_cluster/settings?flat_settings=true")
                c.check("P3 /_cluster/settings -> 200", settings.get("status") == 200, str(settings))
                c.check("P3 settings body has persistent key",
                        "persistent" in (settings.get("body") or {}), str(settings))
                ilm_status = real("/_ilm/status")
                c.check("P3 /_ilm/status -> 200", ilm_status.get("status") == 200, str(ilm_status))
                c.check("P3 ILM status has operation_mode",
                        bool((ilm_status.get("body") or {}).get("operation_mode")), str(ilm_status))
                policies = real("/_ilm/policy")
                c.check("P3 /_ilm/policy -> 200", policies.get("status") == 200, str(policies))
                # ES returns the policies as a top-level map: {name: {version, policy}}
                c.check("P3 /_ilm/policy body is a name->policy map",
                        isinstance(policies.get("body"), dict), str(policies)[:200])
                explain = real("/_all/_ilm/explain")
                c.check("P3 /_all/_ilm/explain -> 200", explain.get("status") == 200, str(explain))
                c.check("P3 ILM explain has indices map",
                        isinstance((explain.get("body") or {}).get("indices"), dict), str(explain))
                index_tpl = real("/_index_template")
                c.check("P3 /_index_template -> 200", index_tpl.get("status") == 200, str(index_tpl))
                c.check("P3 /_index_template has index_templates list",
                        isinstance((index_tpl.get("body") or {}).get("index_templates"), list), str(index_tpl))
                comp_tpl = real("/_component_template")
                c.check("P3 /_component_template -> 200", comp_tpl.get("status") == 200, str(comp_tpl))
                c.check("P3 /_component_template has component_templates list",
                        isinstance((comp_tpl.get("body") or {}).get("component_templates"), list), str(comp_tpl))
                streams = real("/_data_stream")
                c.check("P3 /_data_stream -> 200", streams.get("status") == 200, str(streams))
                c.check("P3 /_data_stream has data_streams list",
                        isinstance((streams.get("body") or {}).get("data_streams"), list), str(streams))
                # ES 8.x answers `group_by=none` with a FLAT `tasks` array (the
                # Workbench parser depends on that shape, so guard it here).
                tasks = real("/_tasks?group_by=none&detailed=true")
                c.check("P3 /_tasks?group_by=none -> 200", tasks.get("status") == 200, str(tasks))
                c.check("P3 /_tasks?group_by=none returns flat tasks array",
                        isinstance((tasks.get("body") or {}).get("tasks"), list), str(tasks)[:200])
                alloc = real("/_cat/allocation?format=json")
                c.check("P3 /_cat/allocation -> 200", alloc.get("status") == 200, str(alloc))
                # A fully assigned cluster legitimately answers 400 here, so the
                # Workbench treats "no unassigned shard" as an explained outcome.
                explain_alloc = real("/_cluster/allocation/explain")
                c.check("P3 /_cluster/allocation/explain -> 200|400",
                        explain_alloc.get("status") in (200, 400), str(explain_alloc)[:200])
                repos = real("/_snapshot")
                c.check("P3 /_snapshot -> 200", repos.get("status") == 200, str(repos)[:200])

                # ---- P2 write gate: read-only connection must be rejected at the service layer ----
                s.call("connection/connect", {"connection_id": "real-es-ro", "direct": {"url": args.es_url, "read_only": True}})

                def real_ro(path, method="GET", body=None):
                    p = {"connection_id": "real-es-ro", "method": method, "path": path}
                    if body is not None:
                        p["body"] = body
                    # Rejected writes come back as a top-level JSON-RPC error with
                    # no "result", so return the full response (like the `req`
                    # helper) and let err_code() read error.code.
                    return s.call("es/request", p)

                c.check("P2 read-only rejects DELETE index -> -32003",
                        err_code(real_ro("/es-studio-smoke-p2", "DELETE")) == -32003)
                c.check("P2 read-only rejects POST /_aliases -> -32003",
                        err_code(real_ro("/_aliases", "POST", {"actions": [{"add": {"index": "x", "alias": "y"}}]})) == -32003)
                c.check("P2 read-only rejects POST /_close -> -32003",
                        err_code(real_ro("/es-studio-smoke-p2/_close", "POST")) == -32003)
                c.check("P2 read-only allows GET /_cluster/health",
                        (real_ro("/_cluster/health").get("result") or {}).get("status") == 200)

                # ---- P3 write gate: every P3 destructive path is refused too ----
                # The permission layer rejects before any HTTP call, so the target
                # resources do not have to exist for these assertions.
                c.check("P3 read-only rejects POST /_tasks/{id}/_cancel -> -32003",
                        err_code(real_ro("/_tasks/node:1/_cancel", "POST")) == -32003)
                c.check("P3 read-only rejects POST /_data_stream/{name}/_rollover -> -32003",
                        err_code(real_ro("/_data_stream/es-studio-smoke-absent/_rollover", "POST")) == -32003)
                c.check("P3 read-only rejects PUT /_ilm/policy/{name} -> -32003",
                        err_code(real_ro("/_ilm/policy/es-studio-smoke-policy", "PUT", {"policy": {"phases": {}}})) == -32003)
                c.check("P3 read-only rejects DELETE /_index_template/{name} -> -32003",
                        err_code(real_ro("/_index_template/es-studio-smoke-tpl", "DELETE")) == -32003)
                c.check("P3 read-only rejects PUT /_component_template/{name} -> -32003",
                        err_code(real_ro("/_component_template/es-studio-smoke-comp", "PUT", {"template": {}})) == -32003)
                c.check("P3 read-only rejects PUT /_cluster/settings -> -32003",
                        err_code(real_ro("/_cluster/settings", "PUT", {"persistent": {"cluster.routing.allocation.enable": "all"}})) == -32003)
                c.check("P3 read-only rejects PUT /_snapshot/{repo} -> -32003",
                        err_code(real_ro("/_snapshot/es-studio-smoke-repo", "PUT", {"type": "fs", "settings": {"location": "x"}})) == -32003)
                c.check("P3 read-only rejects DELETE /_snapshot/{repo}/{snap} -> -32003",
                        err_code(real_ro("/_snapshot/es-studio-smoke-repo/snap", "DELETE")) == -32003)
                c.check("P3 read-only allows GET /_tasks",
                        (real_ro("/_tasks?group_by=none&detailed=true").get("result") or {}).get("status") == 200)
                s.call("connection/disconnect", {"connection_id": "real-es-ro"})

                # ---- P4 monitoring: the sidecar owns a series, it does not just proxy ES ----
                # These are the first RPCs that keep server-side state (a goroutine plus
                # a JSONL file), so assert on real cluster numbers and on the file itself.
                mon = s.call("mon/start", {"connection_id": "real-es", "interval_sec": 10})
                mstart = mon.get("result") or {}
                c.check("P4 mon/start reports running", mstart.get("running") is True, str(mon)[:200])
                c.check("P4 mon/start honours interval_sec", mstart.get("interval_sec") == 10, str(mstart)[:200])
                c.check("P4 mon/start owns a series file",
                        str(mstart.get("file") or "").endswith(".jsonl"), str(mstart.get("file"))[:200])

                # The first tick lives in the goroutine, so collect once synchronously:
                # the assertions below must not depend on scheduler timing.
                sample = s.call("mon/sample", {"connection_id": "real-es"})
                sres = (sample.get("result") or {}).get("status") or {}
                c.check("P4 mon/sample collects synchronously", sres.get("collected", 0) >= 1, str(sample)[:200])
                c.check("P4 mon/sample leaves no error behind", not sres.get("last_error"), str(sres)[:200])

                hist = s.call("mon/history", {"connection_id": "real-es", "limit": 5}).get("result") or {}
                c.check("P4 mon/history returns samples", hist.get("count", 0) >= 1, str(hist)[:200])
                first = (hist.get("samples") or [{}])[0]
                cluster = first.get("cluster") or {}
                c.check("P4 mon/history sample carries a real health status",
                        cluster.get("status") in ("green", "yellow", "red"), str(cluster)[:200])
                c.check("P4 mon/history sample carries real node counts",
                        cluster.get("nodes", 0) >= 1 and cluster.get("active", 0) >= 0, str(cluster)[:200])
                c.check("P4 mon/history sample carries per-node metrics",
                        len(first.get("nodes") or {}) >= 1, str(first.get("nodes"))[:200])
                c.check("P4 mon/history reports the stored series",
                        str(hist.get("file") or "").endswith(".jsonl") and hist.get("stored", 0) >= 1,
                        str(hist.get("file"))[:200])

                # An over-eager interval must be clamped, not trusted.
                clamped = s.call("mon/start", {"connection_id": "real-es", "interval_sec": 1}).get("result") or {}
                c.check("P4 mon/start clamps interval_sec to 5", clamped.get("interval_sec") == 5, str(clamped)[:200])
                c.check("P4 mon/start rejects a missing connection_id -> -32602",
                        err_code(s.call("mon/start", {"interval_sec": 10})) == -32602)

                status = (s.call("mon/status", {"connection_id": "real-es"}).get("result") or {}).get("status") or {}
                c.check("P4 mon/status reports running", status.get("running") is True, str(status)[:200])

                stop = s.call("mon/stop", {"connection_id": "real-es"}).get("result") or {}
                c.check("P4 mon/stop stops the loop", stop.get("stopped") is True, str(stop)[:200])
                after = (s.call("mon/status", {"connection_id": "real-es"}).get("result") or {}).get("status") or {}
                c.check("P4 mon/status is stopped afterwards", after.get("running") is False, str(after)[:200])
                c.check("P4 samples survive a stop",
                        (s.call("mon/history", {"connection_id": "real-es", "limit": 5}).get("result") or {}).get("count", 0) >= 1)

                s.call("connection/disconnect", {"connection_id": "real-es"})
    finally:
        s.close()
        mock.shutdown()
        shutil.rmtree(data_dir, ignore_errors=True)

    return c.result()


if __name__ == "__main__":
    sys.exit(main())
