#!/usr/bin/env python3
"""Generate DBX Store submission metadata from the built unsigned candidate.

Usage:
    python scripts/prepare-store-submission.py --repo OWNER/NAME --tag v0.5.1 --license MIT

Inputs (read-only):
    manifest.json
    dist/<id>-<version>-<target>.artifact.json
    dist/<id>-<version>-<target>.dbxp        # sha256/size are re-verified

Outputs (written):
    .dbx-store.json                                   # plugin repo root, store listing metadata
    release-candidates.json                           # plugin repo root, attach to the GitHub Release
    dist/marketplace-0.5.1/store/candidates/<id>.json # manual PR to t8y2/dbx-store:main
    dist/marketplace-0.5.1/store/REPO-AND-PR.md       # concrete step-by-step with the given repo

The store synchronizer derives source/homepage from the registered repository
and tag when they are omitted from .dbx-store.json; we fill them explicitly so
a manual candidate PR carries the same values.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CANDIDATE_DIR = ROOT / "dist" / "marketplace-0.5.1" / "store"


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def dump_json(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
    print("wrote", path.relative_to(ROOT))


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True, help="GitHub repository slug, e.g. Sanshi956/dbx-plugin-es-studio")
    ap.add_argument("--tag", default=None, help="Source tag / release tag (default: v<manifest version>)")
    ap.add_argument("--license", default="MIT", help="SPDX license id for the store listing (default: MIT)")
    ap.add_argument("--release-notes", default=None, help="Override release notes")
    args = ap.parse_args()

    repo = args.repo.strip().strip("/")
    assert re.fullmatch(r"[A-Za-z0-9._-]+/[A-Za-z0-9._-]+", repo), f"invalid --repo: {args.repo!r}"

    manifest = load_json(ROOT / "manifest.json")
    version = manifest["version"]
    plugin_id = manifest["id"]
    target = "windows-x64"
    tag = args.tag or f"v{version}"

    artifact_path = ROOT / "dist" / f"{plugin_id}-{version}-{target}.artifact.json"
    artifact = load_json(artifact_path)
    assert artifact["target"] == target, f"unexpected target: {artifact['target']}"
    package = ROOT / "dist" / artifact["url"]
    actual = hashlib.sha256(package.read_bytes()).hexdigest()
    assert actual == artifact["sha256"], f"artifact sha mismatch: {actual} != {artifact['sha256']}"
    assert package.stat().st_size == artifact["size"], "artifact size mismatch"
    print(f"verified {package.name}: sha256={actual[:16]}... size={artifact['size']}")

    source = f"https://github.com/{repo}/tree/{tag}"
    homepage = f"https://github.com/{repo}"
    download = f"https://github.com/{repo}/releases/download/{tag}/{artifact['url']}"

    # 1. Store listing metadata at the plugin repository root (not packaged).
    store_meta = {
        "name": manifest["name"],
        "description": manifest["description"],
        "icon": manifest["icon"],
        "tags": ["elasticsearch", "database", "monitoring", "cluster", "developer-tools"],
        "permissions": manifest["permissions"],
        "source": source,
        "homepage": homepage,
        "license": args.license,
        "releaseNotes": args.release_notes
        or "0.5.1: standalone Shard Settings page; MCP contribution listed in DBX settings; "
        "localized allocation-explain results; fixed MCP table-header flex class collision "
        "and duplicate each-key crash on unassigned shards.",
        "localizations": {
            "zh-CN": {
                "name": manifest["localizations"]["zh-CN"]["name"],
                "description": manifest["localizations"]["zh-CN"]["description"],
            }
        },
    }
    dump_json(ROOT / ".dbx-store.json", store_meta)

    # 2. release-candidates.json — attached to the GitHub Release, read by dbx-store syncer.
    release_candidates = {
        "plugin": {
            "id": plugin_id,
            "publisher": manifest["publisher"],
            "version": version,
            "name": manifest["name"],
            "description": manifest["description"],
            "permissions": manifest["permissions"],
        },
        "artifacts": [
            {
                "target": artifact["target"],
                "url": artifact["url"],  # bare file name is accepted and resolved against repo+tag
                "sha256": artifact["sha256"],
                "size": artifact["size"],
            }
        ],
    }
    dump_json(ROOT / "release-candidates.json", release_candidates)

    # 3. Manual candidate PR file (t8y2/dbx-store:main) — new plugin requires name/source/license.
    candidate = {
        "schemaVersion": 1,
        "id": plugin_id,
        "publisher": manifest["publisher"],
        "version": version,
        "name": store_meta["name"],
        "description": store_meta["description"],
        "icon": f"https://raw.githubusercontent.com/{repo}/{tag}/{manifest['icon']}",
        "tags": store_meta["tags"],
        "permissions": store_meta["permissions"],
        "source": source,
        "homepage": homepage,
        "license": store_meta["license"],
        "releaseNotes": store_meta["releaseNotes"],
        "localizations": store_meta["localizations"],
        "targets": [
            {
                "target": target,
                "url": download,
                "sha256": artifact["sha256"],
                "size": artifact["size"],
            }
        ],
    }
    dump_json(CANDIDATE_DIR / "candidates" / f"{plugin_id}.json", candidate)

    steps = f"""# GitHub 仓库与 dbx-store PR（{repo} @ {tag}）

## A. 发布作者仓库的 Release（审核输入）

仓库：`{homepage}`
源码 Tag：`{tag}`（与 manifest version `{version}` 一致）
Release 资产（4 个）：

1. `dist/{artifact['url']}`（未签名候选包，**不要**提交到 Git 历史）
2. `dist/{artifact['url'].replace('.dbxp', '.artifact.json')}`（url 为裸文件名）
3. 仓库根目录 `release-candidates.json`（本脚本已生成）
4. Release Notes（可用 `dist/marketplace-0.5.1/SUBMISSION.md` 的变更日志段）

```bash
git remote add origin {homepage}.git
git add -A && git commit -m "release: {version} store submission metadata"
git tag {tag} && git push origin HEAD --tags
# GitHub 网页上创建 Release，上传上面 4 项资产
```

注意：**商店不从这个 tag 源码打包**，同步器下载 Release 里的未签名 `.dbxp` 并按
SHA-256（`{artifact['sha256']}`）与大小（`{artifact['size']}`）校验。`source` tag 仅用于审核溯源。

## B. 向 dbx-store 提交 PR

- PR 目标：`t8y2/dbx-store:main`
- 首次上架：fork 后提交 `candidates/{plugin_id}.json`（本脚本已生成于 `dist/marketplace-0.5.1/store/candidates/`）
- 发布者 `xihonglei` 已在 `publishers/xihonglei.json` 注册，**无需**再提交 publisher 文件
- 不要提交 `.dbxp`、源码树、私钥或自签的 `verified: true`
- PR 模板要填：capabilities / permissions / data access / network access / native sidecar 行为
- CI 在签名前会**故意红**（`open candidate(s) awaiting DBX Store signing`），这是预期行为
- 维护者审核后在 PR 评论 `/sign`，商店用官方密钥签名并把最终 `plugins/{plugin_id}.json` /
  `catalog/index.json` 写回同一 PR，检查转绿后合并

## C. 自动同步（可选，建议同时申请）

向 `t8y2/dbx-store` 的 `automation/plugin-sources.json` 追加一条：

```json
{{ "repository": "{repo}", "metadataPath": ".dbx-store.json", "autoUpdate": true }}
```

注册后，后续版本只需打新 tag + 发 Release（含 `release-candidates.json`），
同步器会自动创建/更新候选 PR，无需每次手工提 PR。

## D. 校验命令

发布前本地确认：

```bash
python scripts/check-manifest.py
python scripts/verify-package.py --package dist/{plugin_id}-{version}-{target}.signed.dbxp
```

商店侧：`node scripts/validate.mjs`（在 dbx-store 仓库中运行）。
"""
    out = CANDIDATE_DIR / "REPO-AND-PR.md"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(steps, encoding="utf-8", newline="\n")
    print("wrote", out.relative_to(ROOT))


if __name__ == "__main__":
    main()
