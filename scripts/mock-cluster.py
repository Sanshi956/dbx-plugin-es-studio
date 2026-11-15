#!/usr/bin/env python3
"""A second, deliberately different cluster for Multi-Cluster Compare tests.

This box can only hold one Elasticsearch (memory guard), so the compare page's
"right" side is served by this tiny double. It is a TEST FIXTURE, not product
data: the page has no idea whether the URL belongs to a real cluster, it just
renders what the endpoint returns, so this exercises the exact same code path.

Served endpoints (all read-only):
  GET /                      -> version 8.15.0
  GET /_cluster/health       -> green, 2 nodes, 0 unassigned
  GET /_cluster/stats        -> different counts/sizes than the local cluster
  GET /_cat/nodes            -> 2 nodes
  GET /_cluster/settings     -> non-default watermarks + max_shards_per_node

Usage:
  python scripts/mock-cluster.py [--port 19921]
"""
import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

HEALTH = {
    "cluster_name": "es-mirror-remote",
    "status": "green",
    "timed_out": False,
    "number_of_nodes": 2,
    "number_of_data_nodes": 2,
    "active_primary_shards": 20,
    "active_shards": 40,
    "relocating_shards": 0,
    "initializing_shards": 0,
    "unassigned_shards": 0,
    "delayed_unassigned_shards": 0,
}

STATS = {
    "cluster_name": "es-mirror-remote",
    "status": "green",
    "indices": {
        "count": 9,
        "docs": {"count": 1_234_567, "deleted": 10},
        "store": {"size_in_bytes": 13_107_200},
    },
    "nodes": {
        "count": {"total": 2, "data": 2, "master": 1},
        "versions": ["8.15.0"],
        "jvm": {"mem": {"heap_used_in_bytes": 2_147_483_648, "heap_max_in_bytes": 4_294_967_296}},
        "fs": {"total_in_bytes": 2_147_483_648_000, "available_in_bytes": 1_073_741_824_000},
    },
}

NODES = [
    {"name": "mirror-a", "node.role": "cdfhilmrstw", "master": "*", "heap.percent": "41"},
    {"name": "mirror-b", "node.role": "cdfhilrstw", "master": "-", "heap.percent": "38"},
]

SETTINGS = {
    "persistent": {
        "cluster.routing.allocation.disk.watermark.low": "80%",
        "cluster.max_shards_per_node": "500",
    },
    "transient": {},
}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, payload, status=200):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = urlparse(self.path).path.rstrip("/") or "/"
        if path == "/":
            self._send({
                "name": "mirror-a",
                "cluster_name": "es-mirror-remote",
                "version": {"number": "8.15.0", "build_flavor": "default"},
                "tagline": "You Know, for Search",
            })
        elif path == "/_cluster/health":
            self._send(HEALTH)
        elif path == "/_cluster/stats":
            self._send(STATS)
        elif path.startswith("/_cat/nodes"):
            self._send(NODES)
        elif path == "/_cluster/settings":
            self._send(SETTINGS)
        else:
            self._send({"error": {"type": "not_found", "reason": f"mock has no {path}"}}, 404)

    def do_POST(self):
        self._send({"error": {"type": "read_only", "reason": "mock is read-only"}}, 403)

    do_PUT = do_POST
    do_DELETE = do_POST

    def log_message(self, fmt, *args):  # keep test output clean
        pass


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=19921)
    args = ap.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    print(f"mock cluster on http://127.0.0.1:{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
