#!/usr/bin/env python3
"""Create the GitHub Release for the current version and upload the store assets.

Idempotent: reuses an existing release and skips already-present assets.
Reads the PAT from GITHUB_TOKEN or the gitignored .dbx-dev/github-token.env.
"""
from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
API = "https://api.github.com"
SLUG = "Sanshi956/dbx-plugin-es-studio"


def get_token() -> str:
    token = os.environ.get("GITHUB_TOKEN", "").strip()
    if not token:
        path = ROOT / ".dbx-dev" / "github-token.env"
        for line in path.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line and not line.startswith("#"):
                token = line.split("=", 1)[-1].strip().strip('"').strip("'")
                break
    if not token:
        raise SystemExit("no token")
    return token


TOKEN = get_token()


def api(method: str, path: str, payload=None, allow_404: bool = False):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        API + path,
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "es-studio-release",
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
        raise SystemExit(f"HTTP {exc.code} {method} {path}: {exc.read()[:400]!r}") from exc


def main() -> None:
    manifest = json.loads((ROOT / "manifest.json").read_text(encoding="utf-8"))
    version = manifest["version"]
    plugin_id = manifest["id"]
    tag = f"v{version}"

    body = (ROOT / "dist" / f"marketplace-{version}" / "RELEASE-NOTES.md").read_text(encoding="utf-8").strip()
    full_body = f"""## ES Studio {version}

{body}

### 变更细节

- **查询剖析全 0**：ES 6.x/7.x 的 `profile.shards[i]` 只有复合 `id`（`[nodeId][index][shard]`），旧逻辑把它当 nodeId，导致 node 为空、耗时全 0。新增无依赖 `mapProfileShards()` 统一解析 6/7/8/9 四种结构，配真实响应 fixture 单测。
- **大集群（6039 分片实测）**：后端 HTTP 默认超时 10s → 60s（RPC 可传 `timeout_secs`）、invoke 超时 30s → 120s；Overview 去掉冗余 `GET /_cat/indices`；分片/索引列表与拓扑矩阵分页。
- **查询结果展示**：固定 `_id/score/_source` 三列改为 mapping 驱动的动态字段列，去掉 90 字符 + 460px 双重截断，逐行「详情」看完整 JSON。
- **控件视觉统一**：自定义 Combobox 对齐原生 `select`；`tokens.css` 单一真源定义焦点环与统一高度，删除 14 处页面级重复控件样式。
- **工程链**：README 补写操作/只读保护说明，新增控件样式审计与市场截图脚本。

### 质量基线（真实 ES 8.19.23）

主回归 23/23、UX 106/106、25 页渲染 0 错、写路径 7/7、只读拒绝 4/4、i18n 25+25、svelte-check 0/0、go test/vet 全绿、包内 smoke 125/125、MCP 客户端 32/32、verify 17/17。

### 资产

未签名审核候选包 + `artifact.json` + `release-candidates.json`。商店审核后用官方密钥集中签名；
本地私有安装请用仓库自签流程。"""

    release = api("GET", f"/repos/{SLUG}/releases/tags/{tag}", allow_404=True)
    if release.get("id"):
        print("release exists:", release["html_url"])
    else:
        release = api(
            "POST",
            f"/repos/{SLUG}/releases",
            {"tag_name": tag, "name": f"ES Studio {version}", "body": full_body, "draft": False, "prerelease": False},
        )
        print("created release:", release["html_url"])

    upload_url = release["upload_url"].split("{", 1)[0]
    assets = [
        ROOT / "dist" / f"{plugin_id}-{version}-windows-x64.dbxp",
        ROOT / "dist" / f"{plugin_id}-{version}-windows-x64.artifact.json",
        ROOT / "release-candidates.json",
    ]
    for asset in assets:
        req = urllib.request.Request(
            f"{upload_url}?name={asset.name}",
            data=asset.read_bytes(),
            method="POST",
            headers={
                "Authorization": f"Bearer {TOKEN}",
                "Accept": "application/vnd.github+json",
                "Content-Type": "application/octet-stream",
                "User-Agent": "es-studio-release",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                print("uploaded", asset.name, resp.status)
        except urllib.error.HTTPError as exc:
            if exc.code == 422:
                print("already present:", asset.name)
            else:
                raise SystemExit(f"upload failed {asset.name}: {exc.code} {exc.read()[:300]!r}") from exc

    print("release:", release["html_url"])


if __name__ == "__main__":
    main()
