"""Render check across every nav page on a cluster that cannot hold the fixtures.

The browser suites are fixture-driven (`p3-demo-*` requires 7.8/7.9). On 6.8 /
7.0 / 7.7 those fixtures cannot exist, so their failures say nothing about
correctness. What this script asserts instead is behaviour that every version
must deliver: all 25 pages render an `h1`, no console error is logged, and the
version gate replaces a missing feature instead of leaving an empty table or a
blank screen.

    python scripts/browser-render-check.py --base-url http://127.0.0.1:5199 --expect-version 6.8

Exit code 1 on any page that fails those three properties.

`--gate-out DIR` additionally keeps full-page shots of the gated pages and prints
the exact sentence they show, so a version's gate evidence is one command away.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"

NAV = [
    "集群概览", "节点", "分片与分配", "索引列表", "别名",
    "文档浏览", "查询构建器", "DSL 编辑器", "分词分析", "REST 控制台",
    "集群设置", "索引模板", "组件模板", "数据流", "生命周期",
    "查询剖析", "快照", "任务",
    "集群指标", "节点指标", "历史", "集群诊断", "MCP 工具服务",
]

# Pages whose feature does not exist on 6.x: they must explain themselves.
# nav label -> API version that introduces the feature
GATED = {"数据流": "7.9", "组件模板": "7.8", "索引模板": "7.8"}
# ASCII file stems for the gate evidence shots
SLUG = {"数据流": "data-streams", "组件模板": "component-templates", "索引模板": "index-templates"}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://127.0.0.1:5199")
    ap.add_argument("--expect-version", default="6.8")
    ap.add_argument("--out", default="/tmp/render")
    ap.add_argument("--gate-out", default="")
    args = ap.parse_args()

    gate_dir = Path(args.gate_out) if args.gate_out else None
    if gate_dir:
        gate_dir.mkdir(parents=True, exist_ok=True)

    fails: list[str] = []
    rows: list[dict] = []

    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=CHROME, headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 900})
        errors: list[str] = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))

        page.goto(args.base_url, wait_until="domcontentloaded")
        page.wait_for_function("() => !!document.querySelector('h1')", timeout=30000)

        for label in NAV:
            before = len(errors)
            try:
                page.get_by_role("button", name=label, exact=True).first.click(timeout=8000)
            except Exception:
                page.locator(f"text={label}").first.click(timeout=8000)
            page.wait_for_timeout(700)
            h1 = page.locator("h1").first.inner_text().strip()
            body = page.locator("body").inner_text()
            gate = GATED.get(label)
            gate_ok = True
            gate_text = ""
            if gate:
                gate_ok = (gate in body) or ("需要" in body and args.expect_version in body)
                if not gate_ok:
                    fails.append(f"{label}: 没有出现 {gate}+ 的版本门控说明")
                # keep the sentence the page actually shows; evidence must be quotable
                for line in body.splitlines():
                    if "需要" in line or gate in line:
                        gate_text = line.strip()
                        if "需要" in line:
                            break
            errors_here = errors[before:]
            if errors_here:
                fails.append(f"{label}: 控制台报错 {errors_here[:2]}")
            if not h1:
                fails.append(f"{label}: h1 为空（可能白屏）")
            if len(body.strip()) < 40:
                fails.append(f"{label}: 正文几乎为空（{len(body)} 字符）")
            page.screenshot(path=f"{args.out}-{NAV.index(label):02d}.png")
            if gate and gate_dir:
                page.screenshot(path=str(gate_dir / f"{args.expect_version}-{SLUG[label]}.png"),
                                full_page=True)
            rows.append({"page": label, "h1": h1, "gated": bool(gate), "gateOk": gate_ok,
                         "gateText": gate_text, "errors": len(errors_here), "chars": len(body)})

        browser.close()

    for r in rows:
        flag = "OK  " if (r["errors"] == 0 and r["h1"] and r["chars"] >= 40) else "FAIL"
        print(f"  [{flag}] {r['page']:<8} h1={r['h1'][:24]:<26} chars={r['chars']:<5} errors={r['errors']}")
    print()
    gated = [r for r in rows if r["gated"]]
    if gated:
        print(f"版本门控原句（集群 {args.expect_version}）:")
        for r in gated:
            mark = "OK" if r["gateOk"] else "FAIL"
            print(f"  [{mark}] {r['page']}: {r['gateText'] or '(空)'}")
        print()
    if fails:
        print("FAILURES:")
        for f in fails:
            print("  -", f)
        return 1
    print(f"render ok: {len(rows)}/{len(rows)} pages, 0 console errors")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
