#!/usr/bin/env python3
"""造大集群测试数据，验证 0.5.2 的大集群性能修复（docs/07 §5 验收标准）。

目标规模：300 索引 × 20 主分片 = 6000 分片（逼近用户实测的 6037 分片），
0 副本以适配单节点 512MB heap。索引名按业务类型分组，便于在 Shards/IndexList
页观察分页效果；分片数足以触发 _cat/shards / _cat/indices 的大响应体与全量渲染瓶颈。

用法（可复现，跨机器一致）：
  ES_URL=http://127.0.0.1:19200 \
  ES_STUDIO_TEST_ROOT=D:/tmp/es-studio \
  python scripts/seed-large-cluster.py --count 300 --shards 20

  # 先小规模验证脚本正确性：
  python scripts/seed-large-cluster.py --count 30 --shards 20

  # 清理（只删 big-* 前缀，不碰小集群 seed 数据）：
  python scripts/seed-large-cluster.py --clean

依赖：只依赖 Python 标准库（urllib）。本机 HTTP 代理已禁用（--noproxy 等价）。
"""

import argparse
import json
import os
import sys
import time
import urllib.request
from pathlib import Path

BASE = os.environ.get("ES_URL", "http://127.0.0.1:19200")
PREFIX = "big-"
# 业务类型分组，让索引名有真实感；每组分到 count 个索引
KINDS = [
    "app-logs", "metrics", "traces", "audit", "events",
    "sessions", "orders", "users", "payments", "notifications",
]
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def req(method, path, body=None, timeout=60):
    data = json.dumps(body).encode("utf-8") if body is not None else None
    r = urllib.request.Request(
        BASE + path, data=data, method=method,
        headers={"Content-Type": "application/json"},
    )
    with OPENER.open(r, timeout=timeout) as resp:
        return resp.status, json.loads(resp.read().decode("utf-8"))


def index_name(i):
    """big-<kind>-NNN，10 类轮转，编号三位补零。"""
    kind = KINDS[i % len(KINDS)]
    seq = i // len(KINDS) + 1
    return f"{PREFIX}{kind}-{seq:03d}"


def stats():
    _, s = req("GET", "/_cluster/stats")
    shard = s.get("indices", {}).get("shards", {}).get("total", "?")
    idx = s.get("indices", {}).get("count", "?")
    _, h = req("GET", "/_cluster/health")
    return idx, shard, h.get("status", "?")


def cmd_clean():
    print(f"cleaning {PREFIX}* indices on {BASE} ...", flush=True)
    t0 = time.time()
    _, r = req("DELETE", f"/{PREFIX}*", timeout=120)
    print(f"  acknowledged={r.get('acknowledged')} in {time.time()-t0:.1f}s", flush=True)
    idx, shard, status = stats()
    print(f"after clean: indices={idx} shards={shard} status={status}", flush=True)


def cmd_seed(count, shards, batch):
    print(f"seeding {count} indices x {shards} shards = {count*shards} shards on {BASE}", flush=True)
    # 模板兜底：即使换集群忘记建模板，也用显式 settings 保证分片数与压缩
    settings = {
        "settings": {
            "number_of_shards": shards,
            "number_of_replicas": 0,
            "refresh_interval": "60s",
            "codec": "best_compression",
        }
    }
    t0 = time.time()
    created = 0
    skipped = 0
    for i in range(count):
        name = index_name(i)
        try:
            req("PUT", f"/{name}", body=settings, timeout=60)
            created += 1
        except urllib.error.HTTPError as e:
            if e.code == 400:
                skipped += 1  # resource_already_exists：幂等重跑
            else:
                raise
        if (i + 1) % batch == 0 or i + 1 == count:
            idx, shard, status = stats()
            print(
                f"  [{i+1}/{count}] created={created} skipped={skipped} "
                f"| cluster: indices={idx} shards={shard} status={status} "
                f"| {time.time()-t0:.0f}s",
                flush=True,
            )
    print(f"DONE: created={created} skipped={skipped} in {time.time()-t0:.1f}s", flush=True)
    idx, shard, status = stats()
    print(f"FINAL: indices={idx} shards={shard} status={status}", flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=300, help="索引数（默认 300）")
    ap.add_argument("--shards", type=int, default=20, help="每索引主分片数（默认 20）")
    ap.add_argument("--batch", type=int, default=25, help="每多少索引打印一次进度")
    ap.add_argument("--clean", action="store_true", help="只删除 big-* 索引后退出")
    a = ap.parse_args()
    print(f"instance base={BASE}", flush=True)
    if a.clean:
        cmd_clean()
    else:
        cmd_seed(a.count, a.shards, a.batch)
    return 0


if __name__ == "__main__":
    sys.exit(main())
