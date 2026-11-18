#!/usr/bin/env python3
"""Capture the DBX Store listing screenshots for the current version.

Drives the standalone browser bridge (scripts/browser-bridge.mjs) with Playwright
and the system Chrome, at 1440x900 @2x as required by the store submission.

    python scripts/marketplace-shots.py --base-url http://127.0.0.1:5202

Output: dist/marketplace-<version>/screenshots/NN-name.png
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent
CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"

# (file stem, nav label, text that proves the page rendered)
PAGES = [
    ("01-overview", "集群概览", "集群概览"),
    ("02-nodes", "节点", "节点"),
    ("03-shards-topology", "分片与拓扑", "拓扑"),
    ("04-shard-settings", "分片设置", "副本"),
    ("05-shard-diagnosis", "分片诊断", "分片诊断"),
    ("06-indexes", "索引列表", "索引"),
    ("07-query-builder", "查询构建器", "查询"),
    ("08-ilm", "生命周期", "生命周期"),
    ("09-mcp", "MCP 工具服务", "MCP"),
]

# Pages that need staging before the shot: which index to preselect and which
# buttons to press. A store screenshot of "no matching documents" sells nothing.
STAGE = {
    "07-query-builder": {"select_index": "products", "clicks": ["执行查询"]},
}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://127.0.0.1:5202")
    ap.add_argument("--out", default=None)
    args = ap.parse_args()

    version = json.loads((ROOT / "manifest.json").read_text(encoding="utf-8"))["version"]
    out = Path(args.out) if args.out else ROOT / "dist" / f"marketplace-{version}" / "screenshots"
    out.mkdir(parents=True, exist_ok=True)

    errors: list[str] = []
    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME,
            headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 900}, device_scale_factor=2)
        page.on("pageerror", lambda e: errors.append(f"[pageerror] {e}"))
        page.on("console", lambda m: errors.append(f"[{m.type}] {m.text}") if m.type == "error" else None)

        page.goto(args.base_url, wait_until="domcontentloaded")
        page.wait_for_selector("nav button", timeout=30000)
        page.wait_for_timeout(1500)

        for stem, label, probe in PAGES:
            page.click(f'nav button:has-text("{label}")')
            page.wait_for_timeout(700)
            try:
                page.wait_for_selector(f"text={probe}", timeout=20000)
            except Exception:
                pass
            stage = STAGE.get(stem, {})
            index_name = stage.get("select_index")
            if index_name:
                try:
                    page.select_option(".bar select", label=index_name)
                    page.wait_for_timeout(700)
                except Exception as exc:  # staging is best-effort, never fail the shot
                    print(f"  warn: {stem} could not select {index_name!r}: {exc}")
            for action in stage.get("clicks", []):
                try:
                    page.click(f'button:has-text("{action}")')
                    page.wait_for_timeout(1800)
                except Exception as exc:  # staging is best-effort, never fail the shot
                    print(f"  warn: {stem} could not click {action!r}: {exc}")
            page.wait_for_timeout(900)
            path = out / f"{stem}.png"
            page.screenshot(path=str(path))
            print("shot", path.relative_to(ROOT), path.stat().st_size)

        browser.close()

    if errors:
        print("console errors:")
        for e in errors[:10]:
            print(" ", e)
    print(f"done: {len(PAGES)} pages -> {out}")


if __name__ == "__main__":
    main()
