#!/usr/bin/env python3
"""Generate DBX Store submission metadata from built unsigned candidates.

Usage:
    python scripts/prepare-store-submission.py --repo OWNER/REPO --tag v0.5.1 --license MIT
    python scripts/prepare-store-submission.py --repo OWNER/REPO --targets windows-x64,darwin-arm64

Inputs (read-only):
    manifest.json
    dist/<id>-<version>-<target>.artifact.json   (one per built platform)
    dist/<id>-<version>-<target>.dbxp            (sha256/size re-verified)

Outputs (written):
    .dbx-store.json                                          # plugin repo root, store listing metadata
    release-candidates.json                                  # plugin repo root, attach to the GitHub Release
    dist/marketplace-<version>/store/candidates/<id>.json    # manual PR to t8y2/dbx-store:main
    dist/marketplace-<version>/store/REPO-AND-PR.md          # concrete step-by-step with the given repo

The DBX CLI builds a native Go/Rust sidecar and rejects a --target that differs
from the build host ("run this package command on the target platform"), so each
target must be packaged on its own native runner. See .github/workflows/release.yml.
Targets are discovered from dist/: a local single-platform build still works and
produces a one-target submission, preserving the previous behaviour.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Targets understood by @dbx-app/plugin-cli's current_target() mapping. Used as
# discovery order only; --targets can restrict or extend the set.
DEFAULT_TARGETS = ("windows-x64", "darwin-x64", "darwin-arm64", "linux-x64", "linux-arm64")
TARGET_RE = re.compile(r"^[a-z0-9-]{1,64}$")  # matches dbx-store validate.mjs


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def dump_json(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
    print("wrote", path.relative_to(ROOT))


def parse_targets(value: str | None) -> list[str]:
    """Return the target discovery order from --targets (default: all known)."""
    if not value or value.strip().lower() in ("auto", "all"):
        return list(DEFAULT_TARGETS)
    targets = [item.strip() for item in value.split(",") if item.strip()]
    for target in targets:
        if not TARGET_RE.fullmatch(target):
            raise SystemExit(f"invalid --targets entry: {target!r}")
    if len(set(targets)) != len(targets):
        raise SystemExit("duplicate targets in --targets")
    return targets


def discover_artifacts(plugin_id: str, version: str, targets: list[str]) -> list[dict]:
    """Load and verify every <id>-<version>-<target> artifact present in dist/."""
    found: list[dict] = []
    for target in targets:
        artifact_path = ROOT / "dist" / f"{plugin_id}-{version}-{target}.artifact.json"
        if not artifact_path.exists():
            print(f"skip   {target}: no artifact metadata in dist/")
            continue
        artifact = load_json(artifact_path)
        if artifact.get("target") != target:
            raise SystemExit(
                f"unexpected target in {artifact_path.name}: {artifact.get('target')!r} != {target!r}"
            )
        package = ROOT / "dist" / artifact["url"]
        if not package.exists():
            raise SystemExit(f"missing package for {target}: {package}")
        actual = hashlib.sha256(package.read_bytes()).hexdigest()
        if actual != artifact["sha256"]:
            raise SystemExit(f"artifact sha mismatch for {target}: {actual} != {artifact['sha256']}")
        if package.stat().st_size != artifact["size"]:
            raise SystemExit(f"artifact size mismatch for {target}")
        print(f"verified {package.name}: sha256={actual[:16]}... size={artifact['size']}")
        found.append(artifact)
    if not found:
        raise SystemExit(
            "No artifacts found in dist/. Build at least one target first, e.g. "
            "`dbx-plugin package . --target windows-x64` on a matching native host."
        )
    found.sort(key=lambda item: item["target"])
    return found


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True, help="GitHub repository slug, e.g. Sanshi956/dbx-plugin-es-studio")
    ap.add_argument("--tag", default=None, help="Source tag / release tag (default: v<manifest version>)")
    ap.add_argument("--license", default="MIT", help="SPDX license id for the store listing (default: MIT)")
    ap.add_argument("--release-notes", default=None, help="Override release notes")
    ap.add_argument(
        "--targets",
        default=None,
        help="Comma-separated targets to include (default: auto-discover all built targets in dist/)",
    )
    args = ap.parse_args()

    repo = args.repo.strip().strip("/")
    assert re.fullmatch(r"[A-Za-z0-9._-]+/[A-Za-z0-9._-]+", repo), f"invalid --repo: {args.repo!r}"

    manifest = load_json(ROOT / "manifest.json")
    version = manifest["version"]
    plugin_id = manifest["id"]
    tag = args.tag or f"v{version}"
    artifacts = discover_artifacts(plugin_id, version, parse_targets(args.targets))
    candidate_dir = ROOT / "dist" / f"marketplace-{version}" / "store"

    source = f"https://github.com/{repo}/tree/{tag}"
    homepage = f"https://github.com/{repo}"
    release_notes = args.release_notes or (
        "0.5.1: standalone Shard Settings page; MCP contribution listed in DBX settings; "
        "localized allocation-explain results; fixed MCP table-header flex class collision "
        "and duplicate each-key crash on unassigned shards."
    )

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
        "releaseNotes": release_notes,
        "localizations": {
            "zh-CN": {
                "name": manifest["localizations"]["zh-CN"]["name"],
                "description": manifest["localizations"]["zh-CN"]["description"],
            }
        },
    }
    dump_json(ROOT / ".dbx-store.json", store_meta)

    # 2. release-candidates.json — attached to the GitHub Release, read by dbx-store syncer.
    #    dbx-store/scripts/sync-release-candidate.mjs maps every artifact to a candidate target.
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
                "url": artifact["url"],  # bare file name is resolved against repo+tag
                "sha256": artifact["sha256"],
                "size": artifact["size"],
            }
            for artifact in artifacts
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
                "target": artifact["target"],
                "url": f"https://github.com/{repo}/releases/download/{tag}/{artifact['url']}",
                "sha256": artifact["sha256"],
                "size": artifact["size"],
            }
            for artifact in artifacts
        ],
    }
    dump_json(candidate_dir / "candidates" / f"{plugin_id}.json", candidate)

    asset_rows = "\n".join(
        f"| `{a['url']}` | `{a['target']}` | `{a['sha256']}` | {a['size']} |" for a in artifacts
    )
    asset_list = "\n".join(f"{i}. `dist/{a['url']}`" for i, a in enumerate(artifacts, 1))
    verify_rows = "\n".join(
        f"python scripts/verify-package.py --package dist/{a['url'].replace('.dbxp', '.signed.dbxp')}"
        for a in artifacts
    )
    steps = f"""# GitHub 仓库与 dbx-store PR（{repo} @ {tag}）

## A. 发布作者仓库的 Release（审核输入）

仓库：`{homepage}`
源码 Tag：`{tag}`（与 manifest version `{version}` 一致）
Release 资产（{len(artifacts) + 2} 项：{len(artifacts)} 个平台各含 .dbxp + .artifact.json，外加 release-candidates.json）：

{asset_list}
{len(artifacts) + 1}. 仓库根目录 `release-candidates.json`（本脚本已生成）
{len(artifacts) + 2}. Release Notes（可用 `dist/marketplace-{version}/SUBMISSION.md` 的变更日志段）

| 包 | target | SHA-256 | size |
|---|---|---|---|
{asset_rows}

```bash
git remote add origin {homepage}.git
git add -A && git commit -m "release: {version} store submission metadata"
git tag {tag} && git push origin HEAD --tags
# GitHub 网页上创建 Release，上传上面全部资产
```

也可以直接跑一键发布脚本，它会枚举 `dist/` 下的所有平台资产：

```bash
GITHUB_TOKEN=ghp_xxx python scripts/github-publish.py --repo-name dbx-plugin-es-studio
```

多平台产物由 `.github/workflows/release.yml` 在**各目标平台的原生 runner**上构建
（DBX CLI 拒绝 host 与 `--target` 不一致的带后端打包）。打 `v*` tag 或手动触发
workflow 即可自动构建、汇总并发布全部目标平台的资产。

注意：**商店不从这个 tag 源码打包**，同步器下载 Release 里的未签名 `.dbxp` 并按
上表的 SHA-256 与大小校验。`source` tag 仅用于审核溯源。

## B. 向 dbx-store 提交 PR

- PR 目标：`t8y2/dbx-store:main`
- 首次上架：fork 后提交 `candidates/{plugin_id}.json`（本脚本已生成于 `dist/marketplace-{version}/store/candidates/`）
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
同步器会自动创建/更新候选 PR，无需每次手工提 PR；所有平台 target 都会随
`release-candidates.json` 的 `artifacts` 数组一起同步。

## D. 校验命令

发布前本地确认：

```bash
python scripts/check-manifest.py
{verify_rows}
```

商店侧：`node scripts/validate.mjs`（在 dbx-store 仓库中运行）。
"""
    out = candidate_dir / "REPO-AND-PR.md"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(steps, encoding="utf-8", newline="\n")
    print("wrote", out.relative_to(ROOT))
    print(f"\nPrepared {len(artifacts)} target(s): {', '.join(a['target'] for a in artifacts)}")


if __name__ == "__main__":
    main()
