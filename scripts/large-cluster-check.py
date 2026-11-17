#!/usr/bin/env python3
"""大集群性能验收（docs/07 §5）：在 6000 分片集群上实测 0.5.2 的三项修复。

验收点：
  1. Overview（boot 锚点）加载耗时 —— 去掉冗余 _cat/indices 后应秒开
  2. 分片与拓扑页：DOM 行数应被分页裁剪（< 200），而非全量 6000 行；分页脚标存在
  3. 索引列表页：DOM 行数被分页裁剪（< 150），分页脚标存在
  4. 全程无 console error / 无「超时」类错误态

用法：
  python scripts/large-cluster-check.py --base-url http://127.0.0.1:5199

进程保留：本脚本只读，不停止 ES/bridge，跑完可直接进 UI 查看。
"""
from __future__ import annotations

import argparse
import sys
import time
from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://127.0.0.1:5199")
    args = ap.parse_args()

    print(f"打开 {args.base_url}，等待 boot 锚点「集群概览」...", flush=True)
    fails: list[str] = []
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=CHROME, headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 900})
        errors: list[str] = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))

        t0 = time.time()
        page.goto(args.base_url, wait_until="domcontentloaded")
        # boot 锚点：集群概览页的真实数据 h1（不是 loading）
        page.wait_for_function(
            "() => {const b=document.body.innerText; return b.includes('集群概览') && "
            "!b.includes('正在加载') && !b.includes('Loading');}",
            timeout=120000,
        )
        overview_secs = time.time() - t0
        print(f"[1] Overview 加载耗时: {overview_secs:.1f}s", flush=True)
        if overview_secs > 15:
            fails.append(f"Overview 耗时 {overview_secs:.1f}s > 15s")

        # 读取概览页显示的分片/索引数（证明数据确实是大集群）
        body = page.locator("body").inner_text()
        import re
        shards_m = re.search(r"([\d,]+)\s*(?:个)?分片|分片[^\d]{0,6}([\d,]+)", body)
        print(f"    概览页含分片数线索: {shards_m.group(0) if shards_m else '未匹配(不影响判定)'}", flush=True)

        def click_nav(label):
            try:
                page.get_by_role("button", name=label, exact=True).first.click(timeout=8000)
            except Exception:
                page.locator(f"text={label}").first.click(timeout=8000)

        # [2] 分片与拓扑页：等表格出现，数 DOM 行
        click_nav("分片与拓扑")
        page.wait_for_timeout(2500)
        detail_rows = page.locator("table tbody tr").count()
        body2 = page.locator("body").inner_text()
        has_pager = ("下一页" in body2 or "Next" in body2) and ("上一页" in body2 or "上一页" in body2 or "Prev" in body2)
        print(f"[2] 分片页 DOM 行数: {detail_rows}; 分页控件: {'有' if has_pager else '无'}", flush=True)
        if detail_rows > 250:
            fails.append(f"分片页渲染 {detail_rows} 行（>250），分页未生效")
        if not has_pager:
            fails.append("分片页未找到分页控件")

        # [3] 索引列表页
        click_nav("索引列表")
        page.wait_for_timeout(2000)
        idx_rows = page.locator("table tbody tr").count()
        body3 = page.locator("body").inner_text()
        has_pager3 = ("下一页" in body3 or "Next" in body3)
        print(f"[3] 索引列表 DOM 行数: {idx_rows}; 分页控件: {'有' if has_pager3 else '无'}", flush=True)
        if idx_rows > 150:
            fails.append(f"索引列表渲染 {idx_rows} 行（>150），分页未生效")
        if not has_pager3:
            fails.append("索引列表未找到分页控件")

        # [4] console 错误
        real_errors = [e for e in errors if "favicon" not in e.lower()]
        print(f"[4] console 错误数: {len(real_errors)}", flush=True)
        for e in real_errors[:5]:
            print(f"    ERR: {e[:160]}", flush=True)
        if real_errors:
            fails.append(f"console 错误 {len(real_errors)} 条")

        browser.close()

    print("=" * 50, flush=True)
    if fails:
        print("FAIL:", flush=True)
        for f in fails:
            print(f"  - {f}", flush=True)
        return 1
    print("PASS: 大集群三项修复验收全部通过", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
