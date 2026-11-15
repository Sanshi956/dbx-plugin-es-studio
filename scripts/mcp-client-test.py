#!/usr/bin/env python3
"""End-to-end test of the independent MCP server (docs/SPEC §7 P5).

Proves the real thing, not a mock:

  1. authorises the current connection through the Sidecar (`mcp/grant`)
  2. launches the packaged executable in `--mcp` mode as a real stdio process
  3. speaks newline-delimited JSON-RPC to it: initialize → tools/list → tools/call
  4. checks the tool answers against the live cluster, that a write is refused,
     that an unknown tool is a tool error, and that the audit log recorded it
  5. closes stdin and asserts a clean exit

Usage:
  python scripts/mcp-client-test.py --base-url http://127.0.0.1:5199 \
      --sidecar bin/es-studio-sidecar.exe --data-dir .dbx-dev/plugin-data
"""
import argparse
import json
import os
import subprocess
import sys
import urllib.request

REQUIRED_TOOLS = [
    "es_get_cluster_health",
    "es_list_indices",
    "es_get_index_mapping",
    "es_search_documents",
    "es_list_shards",
    "es_call_readonly_api",
]

results = []


def check(step, ok, detail=""):
    results.append({"step": step, "ok": bool(ok)})
    print(f"  {'[OK ]' if ok else '[FAIL]'} {step}" + (f"  ({detail})" if detail and not ok else ""))


def rpc(base_url, method, params):
    payload = json.dumps({"method": method, "params": params}).encode("utf-8")
    request = urllib.request.Request(
        base_url + "/rpc", data=payload, headers={"content-type": "application/json"}, method="POST"
    )
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=60) as response:
        return json.loads(response.read().decode("utf-8"))


class McpClient:
    """Minimal MCP client: newline-delimited JSON-RPC over a child process."""

    def __init__(self, executable, data_dir, token):
        env = dict(os.environ)
        env["ES_STUDIO_MCP_TOKEN"] = token
        env.setdefault("NO_PROXY", "127.0.0.1,localhost")
        self.proc = subprocess.Popen(
            [executable, "--mcp", "--data-dir", data_dir],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", errors="replace", bufsize=1, env=env,
        )
        self.next_id = 1

    def send(self, method, params=None, notify=False):
        message = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            message["params"] = params
        if not notify:
            message["id"] = self.next_id
            self.next_id += 1
        self.proc.stdin.write(json.dumps(message) + "\n")
        self.proc.stdin.flush()
        if notify:
            return None
        line = self.proc.stdout.readline()
        if not line:
            raise RuntimeError("server closed stdout: " + (self.proc.stderr.read() or "")[:400])
        return json.loads(line)

    def call_tool(self, name, arguments):
        return self.send("tools/call", {"name": name, "arguments": arguments})

    def close(self):
        try:
            self.proc.stdin.close()
        except Exception:
            pass
        try:
            return self.proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return -1

    def stderr_tail(self):
        try:
            return self.proc.stderr.read()[-400:]
        except Exception:
            return ""


def text_of(response):
    result = response.get("result") or {}
    content = result.get("content") or []
    return content[0].get("text", "") if content else ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://127.0.0.1:5199")
    ap.add_argument("--sidecar", default="bin/es-studio-sidecar.exe")
    ap.add_argument("--data-dir", default=".dbx-dev/plugin-data")
    ap.add_argument("--connection-id", default="local-es")
    a = ap.parse_args()

    print(f"=== MCP end-to-end: {a.sidecar} --mcp (data dir {a.data_dir}) ===")

    # ---------------------------------------------------------------- authorise
    granted = rpc(a.base_url, "mcp/grant", {"connection_id": a.connection_id})
    grant = granted.get("result") or {}
    token = grant.get("token", "")
    check("mcp/grant 返回一次性令牌", bool(token) and len(token) >= 32, str(granted)[:200])
    check("mcp/grant 回给了客户端配置", "--mcp" in (grant.get("snippet") or ""), str(grant)[:200])
    check("mcp/grant 不返回明文密码", "password" not in (grant.get("snippet") or "").lower())
    if not token:
        return 1
    public = grant.get("public") or {}
    check("公开视图只给指纹不给令牌",
          public.get("fingerprint") and token not in json.dumps(public), json.dumps(public, ensure_ascii=False)[:160])

    # ------------------------------------------------------ unauthorised client
    wrong = subprocess.run(
        [a.sidecar, "--mcp", "--data-dir", a.data_dir, "--check"],
        input="", capture_output=True, text=True, timeout=30,
        env={**os.environ, "ES_STUDIO_MCP_TOKEN": "00000000000000000000000000000000"},
    )
    check("未授权令牌被拒绝（退出码 2）", wrong.returncode == 2, f"rc={wrong.returncode} {wrong.stderr[:120]}")
    check("拒绝时给出可操作提示", "authorise" in wrong.stderr or "授权" in wrong.stderr, wrong.stderr[:160])

    ok_check = subprocess.run(
        [a.sidecar, "--mcp", "--data-dir", a.data_dir, "--check"],
        input="", capture_output=True, text=True, timeout=30,
        env={**os.environ, "ES_STUDIO_MCP_TOKEN": token},
    )
    check("--check 验证令牌通过", ok_check.returncode == 0, ok_check.stderr[:160])

    client = McpClient(a.sidecar, a.data_dir, token)
    try:
        handshake = client.send("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "es-studio-test", "version": "1.0"},
        })
        result = handshake.get("result") or {}
        check("initialize 协商协议版本", result.get("protocolVersion") == "2024-11-05", json.dumps(result)[:160])
        check("serverInfo 正确",
              (result.get("serverInfo") or {}).get("name") == "es-studio-mcp",
              json.dumps(result.get("serverInfo"), ensure_ascii=False))
        client.send("notifications/initialized", notify=True)

        listing = client.send("tools/list")
        tools = (listing.get("result") or {}).get("tools") or []
        names = [tool.get("name") for tool in tools]
        check("tools/list 返回 40+ 工具", len(tools) >= 40, len(tools))
        check("SPEC 要求的 6 个工具都在", all(name in names for name in REQUIRED_TOOLS),
              [name for name in REQUIRED_TOOLS if name not in names])
        check("每个工具都标注 readOnlyHint",
              all((tool.get("annotations") or {}).get("readOnlyHint") is True for tool in tools))
        check("每个工具都有 inputSchema", all(tool.get("inputSchema", {}).get("type") == "object" for tool in tools))

        health = client.call_tool("es_get_cluster_health", {})
        check("es_get_cluster_health 读真实集群",
              (health.get("result") or {}).get("isError") is False
              and '"status"' in text_of(health), text_of(health)[:120])
        check("返回体带 HTTP 状态与请求摘要",
              (health.get("result") or {}).get("structuredContent", {}).get("status") == 200,
              json.dumps((health.get("result") or {}).get("structuredContent"))[:160])

        indices = client.call_tool("es_list_indices", {})
        check("es_list_indices 列出真实索引", "products" in text_of(indices), text_of(indices)[:120])

        mapping = client.call_tool("es_get_index_mapping", {"index": "products"})
        check("es_get_index_mapping 返回字段映射", '"mappings"' in text_of(mapping), text_of(mapping)[:120])

        search = client.call_tool("es_search_documents", {
            "index": "products", "query": {"match_all": {}}, "size": 2,
        })
        check("es_search_documents 返回命中", '"hits"' in text_of(search), text_of(search)[:120])

        shards = client.call_tool("es_list_shards", {})
        check("es_list_shards 返回分片", '"shard"' in text_of(shards) or '"prirep"' in text_of(shards),
              text_of(shards)[:120])

        raw = client.call_tool("es_call_readonly_api", {"path": "/_cluster/health"})
        check("es_call_readonly_api 透传只读端点", (raw.get("result") or {}).get("isError") is False,
              text_of(raw)[:120])

        write = client.call_tool("es_call_readonly_api", {"path": "/products/_doc/1", "method": "POST"})
        check("写方法被参数校验拒绝", (write.get("result") or {}).get("isError") is True, text_of(write)[:160])

        unknown = client.call_tool("es_drop_everything", {})
        check("未知工具返回工具错误而不是崩溃",
              (unknown.get("result") or {}).get("isError") is True, text_of(unknown)[:160])

        badargs = client.call_tool("es_get_index", {"index": "../etc/passwd"})
        check("路径穿越的索引名被拒绝", (badargs.get("result") or {}).get("isError") is True, text_of(badargs)[:160])

        noargs = client.call_tool("es_get_index_mapping", {"indexx": "products"})
        check("拼错的参数名被拒绝（不会被静默忽略）",
              (noargs.get("result") or {}).get("isError") is True, text_of(noargs)[:160])

        ping = client.send("ping")
        check("ping 有响应", "result" in ping, str(ping)[:120])
    finally:
        code = client.close()

    check("stdin 关闭后服务正常退出", code == 0, f"rc={code} {client.stderr_tail()[:200]}")

    # --------------------------------------------------------------- audit log
    logged = rpc(a.base_url, "mcp/log", {"limit": 100}).get("result") or {}
    calls = logged.get("calls") or []
    tools_logged = {row.get("tool") for row in calls}
    check("调用日志记录了外部客户端的调用",
          {"es_get_cluster_health", "es_search_documents"} <= tools_logged, sorted(tools_logged))
    check("调用日志落盘在插件数据目录", (logged.get("file") or "").endswith(".jsonl"), logged.get("file"))
    serialized = json.dumps(calls, ensure_ascii=False)
    check("日志只记参数名不记参数值（无 match_all）", "match_all" not in serialized, serialized[:160])
    check("日志记录失败调用", any(row.get("ok") is False for row in calls), len(calls))

    # ---------------------------------------------------------------- revocation
    revoked = rpc(a.base_url, "mcp/revoke", {"connection_id": a.connection_id}).get("result") or {}
    check("mcp/revoke 撤销授权", revoked.get("revoked") is True, str(revoked))
    after = subprocess.run(
        [a.sidecar, "--mcp", "--data-dir", a.data_dir, "--check"],
        input="", capture_output=True, text=True, timeout=30,
        env={**os.environ, "ES_STUDIO_MCP_TOKEN": token},
    )
    check("撤销后的令牌立即失效", after.returncode == 2, f"rc={after.returncode}")

    passed = sum(1 for r in results if r["ok"])
    print(f"\nRESULT: {passed} passed, {len(results) - passed} failed")
    return 0 if passed == len(results) else 1


if __name__ == "__main__":
    sys.exit(main())
