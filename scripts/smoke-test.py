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
import subprocess
import sys
import threading
import time
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

    def __init__(self, executable):
        self.proc = subprocess.Popen(
            [executable], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, encoding="utf-8", errors="replace", bufsize=1,
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
    s = Sidecar(args.sidecar)
    try:
        c.section("SDK protocol")
        r = s.call("plugin/initialize", {"host": {"protocolVersions": [1]}})
        res = r.get("result", {})
        c.check("initialize protocolVersion=1", res.get("protocolVersion") == 1)
        c.check("initialize plugin.id", (res.get("plugin") or {}).get("id") == "local.es.studio")
        c.check("initialize plugin.version", (res.get("plugin") or {}).get("version") == "0.1.0")
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
                s.call("connection/disconnect", {"connection_id": "real-es-ro"})

                s.call("connection/disconnect", {"connection_id": "real-es"})
    finally:
        s.close()
        mock.shutdown()

    return c.result()


if __name__ == "__main__":
    sys.exit(main())
