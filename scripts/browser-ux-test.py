#!/usr/bin/env python3
"""UX regression for the things the user reported after the 0.1.0 acceptance.

Covers, against the REAL cluster (via scripts/browser-bridge.mjs + real Chrome):

  Shards & Topology   状态摘要（已就绪/分配中/迁移中/未分配）+ 未分配原因列 + 头部只留一行
  Aliases             筛选框、索引下拉、Filter 列 tooltip、头部只留一行
  Index List          按索引名过滤、关闭索引只给 Open / 打开索引只给 Close
  Templates           View 用弹层展示（不再需要滚到页尾），Esc 关闭
  Snapshots           索引用 chips 多选，选中态与计数正确
  Document Browser    "Search" 语义显式化（会执行什么查询）
  Query Builder       must/must_not/should 组合、按字段类型给操作符、切索引清条件、多 tab
  DSL Editor          索引下拉、Tab 插入缩进不跳焦点、Ctrl+Enter 运行

Usage:
  python scripts/browser-ux-test.py [--base-url http://127.0.0.1:5199] [--out screenshots/ux]
"""
import argparse
import json
import os
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"


def wait_text(page, text, timeout=20000):
    page.wait_for_function(
        "t => document.body && document.body.innerText.includes(t)",
        arg=text, timeout=timeout,
    )


def click_nav(page, label):
    page.click(f'nav button:has-text("{label}")')
    page.wait_for_timeout(350)


def run(base_url, out_dir):
    out = Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results = []
    console = []

    def check(step, cond, detail=""):
        results.append({"step": step, "ok": bool(cond), **({"error": str(detail)} if not cond and detail else {})})

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 1500})
        page.on("console", lambda m: console.append(f"[{m.type}] {m.text}") if m.type in ("error", "warning") else None)
        page.on("pageerror", lambda e: console.append(f"[pageerror] {e}"))

        def shot(name):
            page.screenshot(path=str(out / f"{name}.png"), full_page=True)

        page.goto(base_url + "/", wait_until="domcontentloaded", timeout=30000)
        wait_text(page, "Local ES (test)", timeout=30000)
        time.sleep(0.5)

        # ---------------------------------------------------------------- Shards
        try:
            click_nav(page, "Shards")
            wait_text(page, "分配中", timeout=20000)
            cards = page.locator(".summary .card")
            labels = page.locator(".summary .card .lbl").all_inner_texts()
            check("Shards 状态摘要含 已就绪/分配中/迁移中/未分配",
                  cards.count() == 4 and any("INITIALIZING" in t for t in labels)
                  and any("RELOCATING" in t for t in labels) and any("UNASSIGNED" in t for t in labels),
                  labels)
            header_text = page.inner_text("div.page header")
            check("Shards 头部只留一行接口说明（与其它页风格统一）",
                  len(header_text) < 220 and "副本明细" not in header_text, header_text)
            # 我们的夹具里 metrics-daily 有 4 个未分配副本（单节点 + replicas=2）
            unassigned_num = int(page.locator(".summary .card.unassigned .num").inner_text().strip())
            check("Shards 未分配计数来自真实集群 (>0)", unassigned_num > 0, unassigned_num)
            check("Shards 表含「未分配原因」列", "未分配原因" in page.inner_text("div.page"), "")
            reasons = page.locator("table tbody tr td:nth-child(8)").all_inner_texts()
            check("Shards 未分配行有真实原因值", any(r.strip() not in ("", "—") for r in reasons), reasons[:6])
            shot("ux-shards")
        except Exception as e:
            shot("ux-shards-FAIL")
            check("Shards 状态摘要含 已就绪/分配中/迁移中/未分配", False, e)

        # --------------------------------------------------------------- Aliases
        try:
            click_nav(page, "Aliases")
            wait_text(page, "prod-view", timeout=20000)
            check("Aliases 新增表单的索引是下拉", page.locator("section.add select").count() == 1)
            opts = page.locator("section.add select option").all_inner_texts()
            check("Aliases 索引下拉里有真实索引", any("products" in o for o in opts), opts[:4])
            alias_head = page.inner_text("div.page header")
            check("Aliases 头部只留一行接口说明（与其它页风格统一）",
                  len(alias_head) < 220 and "alias filter" not in alias_head, alias_head)
            filter_th = page.locator("table.ax thead th").nth(3)
            tip = filter_th.get_attribute("title") or ""
            check("Aliases Filter 列的语义放在表头 tooltip", "过滤查询" in tip, tip)
            total_before = page.locator("table.ax tbody tr").count()
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "prod")
            page.wait_for_timeout(300)
            after = page.locator("table.ax tbody tr").count()
            check("Aliases 筛选生效（prod -> 只剩 1 行）", after == 1 and total_before >= 2,
                  f"{total_before} -> {after}")
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "zzz-none")
            page.wait_for_timeout(300)
            check("Aliases 无匹配时给空态", page.locator("text=没有匹配的别名").count() >= 1)
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "")
            shot("ux-aliases")
        except Exception as e:
            shot("ux-aliases-FAIL")
            check("Aliases 筛选生效（prod -> 只剩 1 行）", False, e)

        # ------------------------------------------------------------ Index List
        try:
            import urllib.request
            es = os.environ.get("ES_URL", "http://127.0.0.1:19200")
            # 关闭状态必须由 ES 决定，页面不许自己猜：先真关一个索引再验按钮
            req = urllib.request.Request(f"{es}/archived-2023/_close", method="POST")
            urllib.request.urlopen(req, timeout=60).read()
            click_nav(page, "Index List")
            wait_text(page, "archived-2023", timeout=20000)
            page.fill('input[placeholder="按索引名过滤"]', "arch")
            page.wait_for_timeout(400)
            rows = page.locator("table.ix tbody tr")
            check("Index List 按索引名过滤生效（arch -> 1 行）", rows.count() == 1, rows.count())
            btns = rows.first.locator("button").all_inner_texts()
            check("关闭的索引只给 Open、不给 Close",
                  any("Open" in t for t in btns) and not any("Close" in t for t in btns), btns)
            rows.first.locator('button:has-text("Open")').click()
            page.wait_for_selector(".overlay .ack input", timeout=10000)
            page.locator(".overlay .ack input").check()
            page.locator('.overlay button:has-text("打开索引")').click()
            page.wait_for_function(
                "() => { const r = document.querySelector('table.ix tbody tr'); if (!r) return false;"
                " return [...r.querySelectorAll('button')].some(b => b.innerText.includes('Close')); }",
                timeout=30000,
            )
            btns2 = page.locator("table.ix tbody tr").first.locator("button").all_inner_texts()
            check("打开的索引只给 Close、不给 Open",
                  any("Close" in t for t in btns2) and not any("Open" in t for t in btns2), btns2)
            shot("ux-indexlist")
        except Exception as e:
            shot("ux-indexlist-FAIL")
            check("Index List 按索引名过滤生效（arch -> 1 行）", False, e)

        # --------------------------------------------------------------- Browser
        try:
            click_nav(page, "Browser")
            wait_text(page, "Search documents", timeout=20000)
            plan = page.locator("p.plan code").inner_text()
            check("Browser 显示将执行的查询（空关键字=match_all）", "match_all" in plan, plan)
            page.locator("section.bar select").first.select_option("products")
            page.fill('input[placeholder^="留空 = 列出全部文档"]', "mouse")
            page.wait_for_timeout(500)
            plan2 = page.locator("p.plan code").inner_text()
            check("Browser 关键字输入后计划变成 multi_match", "multi_match" in plan2, plan2)
            page.locator("section.bar select").nth(1).select_option("name")
            page.wait_for_timeout(300)
            plan3 = page.locator("p.plan code").inner_text()
            check("Browser 选字段后计划变成 match on name", "on name" in plan3, plan3)
            page.click('button.primary:has-text("Search documents")')
            wait_text(page, "total", timeout=20000)
            shot("ux-browser")
            check("Browser 关键字搜索有结果", "total" in page.inner_text(".meta"), "")
        except Exception as e:
            shot("ux-browser-FAIL")
            check("Browser 关键字搜索有结果", False, e)

        # --------------------------------------------------------- Query Builder
        try:
            click_nav(page, "Query Builder")
            wait_text(page, "CONDITIONS", timeout=20000)
            page.locator("section.bar select").first.select_option("products")
            page.wait_for_function(
                "() => { const el = document.querySelector('.fcount');"
                " return el && /[0-9]+ fields/.test(el.innerText); }",
                timeout=20000,
            )

            # 字段类型决定操作符：price 是 double，不该出现 match
            page.click('button:has-text("+ 加条件")')
            page.locator("div.clause select.field").first.select_option("price")
            page.wait_for_timeout(300)
            ops = page.locator("div.clause select.op").first.locator("option").all_inner_texts()
            check("Query Builder 数值字段只给 range/term/exists",
                  any("range" in o for o in ops) and not any("match" in o for o in ops), ops)
            # name 是 text，应该给 match
            page.locator("div.clause select.field").first.select_option("name")
            page.wait_for_timeout(300)
            ops2 = page.locator("div.clause select.op").first.locator("option").all_inner_texts()
            check("Query Builder text 字段给 match/match_phrase",
                  any("match（" in o for o in ops2) and any("match_phrase" in o for o in ops2), ops2)

            # must_not: category=peripheral -> 6-3=3
            page.locator("div.clause select.field").first.select_option("category")
            page.wait_for_timeout(200)
            page.locator("div.clause select.occur").first.select_option("must_not")
            page.locator("div.clause input.value").first.fill("peripheral")
            page.click('button:has-text("Run query")')
            wait_text(page, "total", timeout=20000)
            meta = page.locator("p.meta").inner_text()
            check("Query Builder must_not 生效（peripheral -> 3/6）", "total 3 hits" in meta, meta)

            # should x2 + msm=1 -> display(1) + accessory(2) = 3
            page.locator('div.clause button.mini:has-text("✕")').first.click()
            page.click('button:has-text("+ 加条件")')
            page.click('button:has-text("+ 加条件")')
            for i, value in enumerate(["display", "accessory"]):
                row = page.locator("div.clause").nth(i)
                row.locator("select.field").select_option("category")
                page.wait_for_timeout(150)
                row.locator("select.occur").select_option("should")
                row.locator("input.value").fill(value)
            page.wait_for_timeout(300)
            check("Query Builder 出现 minimum_should_match 输入", page.locator('input[placeholder="默认 1"]').count() == 1)
            page.click('button:has-text("Run query")')
            wait_text(page, "total", timeout=20000)
            meta2 = page.locator("p.meta").inner_text()
            check("Query Builder should 组合生效（display|accessory -> 3/6）", "total 3 hits" in meta2, meta2)
            dsl = page.locator("details.dsl pre").inner_text()
            check("Query Builder 生成的 DSL 含 bool.should", '"should"' in dsl, dsl[:300])
            shot("ux-querybuilder-should")

            # 多 tab：新 tab 独立索引，且切换后条件被清空
            page.click('button:has-text("+ 新 tab")')
            page.wait_for_timeout(500)
            check("Query Builder 新 tab 没有继承旧条件", page.locator("div.clause").count() == 0,
                  page.locator("div.clause").count())
            sel = page.locator("section.bar select").first
            first_opt = sel.locator("option").first.get_attribute("value")
            check("Query Builder 新 tab 默认选中第一个索引",
                  bool(sel.input_value()) and sel.input_value() == first_opt,
                  f"selected={sel.input_value()} first={first_opt}")
            page.locator("section.bar select").first.select_option("orders")
            page.wait_for_timeout(600)
            check("Query Builder 切索引后条件被清空", page.locator("div.clause").count() == 0)
            tabs = page.locator(".tab-name").all_inner_texts()
            check("Query Builder 两个 tab 分别是 products / orders",
                  any("products" in t for t in tabs) and any("orders" in t for t in tabs), tabs)
            shot("ux-querybuilder-tabs")
        except Exception as e:
            shot("ux-querybuilder-FAIL")
            check("Query Builder must_not 生效（peripheral -> 3/6）", False, e)

        # ------------------------------------------------------------- DSL Editor
        try:
            click_nav(page, "DSL Editor")
            wait_text(page, "Tab 插入 2 空格", timeout=20000)
            check("DSL Editor 有索引下拉", page.locator("section.bar select").count() == 1)
            page.locator("section.bar select").select_option("products")
            editor = page.locator("textarea.editor")
            editor.click()
            page.keyboard.press("Control+End")
            before = editor.input_value()
            page.keyboard.press("Tab")
            page.wait_for_timeout(200)
            after = editor.input_value()
            focused = page.evaluate("document.activeElement && document.activeElement.tagName")
            check("DSL Editor Tab 插入缩进", len(after) == len(before) + 2, f"{len(before)} -> {len(after)}")
            check("DSL Editor Tab 后光标仍在编辑器", focused == "TEXTAREA", focused)
            page.keyboard.press("Control+Enter")
            wait_text(page, "total", timeout=20000)
            meta = page.locator("p.meta").inner_text()
            check("DSL Editor Ctrl+Enter 运行并命中 products", "total 6 hits" in meta, meta)
            shot("ux-dsl")
        except Exception as e:
            shot("ux-dsl-FAIL")
            check("DSL Editor Tab 插入缩进", False, e)

        # ------------------------------------------- Analyzer / Snapshots 下拉
        try:
            click_nav(page, "Analyzer")
            wait_text(page, "Context index", timeout=20000)
            sel = page.locator("section.form select").first
            opts = sel.locator("option").all_inner_texts()
            check("Analyzer 索引是下拉且有真实索引", any("products" in o for o in opts), opts[:4])
            sel.select_option("products")
            page.wait_for_selector('section.form select option[value="name"]', state="attached", timeout=20000)
            fopts = page.locator("section.form select").nth(1).locator("option").all_inner_texts()
            check("Analyzer 字段下拉来自 mapping", any("name · text" in o for o in fopts), fopts[:4])
            page.locator("section.form select").nth(1).select_option("name")
            page.get_by_role("button", name="Analyze", exact=True).click()
            wait_text(page, "tokens", timeout=20000)
            shot("ux-analyzer")
        except Exception as e:
            shot("ux-analyzer-FAIL")
            check("Analyzer 索引是下拉且有真实索引", False, e)

        try:
            click_nav(page, "Snapshots")
            wait_text(page, "p3-fs-repo", timeout=20000)
            chips = page.locator(".ix-chips .chip")
            check("Snapshots 索引用 chips 多选而不是手填", chips.count() >= 5, chips.count())
            chip = page.locator('.ix-chips .chip:has-text("products")').first
            chip.click()
            page.wait_for_timeout(250)
            check("Snapshots chip 选中后高亮（class 带 on）",
                  "on" in (chip.get_attribute("class") or "").split(), chip.get_attribute("class"))
            check("Snapshots 选中计数更新为 1", "1 /" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            page.locator('.ix-head button:has-text("全选")').click()
            page.wait_for_timeout(250)
            total = chips.count()
            check("Snapshots 全选后计数等于索引总数",
                  f"{total} / {total}" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            page.locator('.ix-head button:has-text("清空")').click()
            page.wait_for_timeout(250)
            check("Snapshots 清空后计数归零", "0 /" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            shot("ux-snapshots")
        except Exception as e:
            shot("ux-snapshots-FAIL")
            check("Snapshots 索引用 chips 多选而不是手填", False, e)

        # ------------------------------------------ Templates / Component 弹层
        for nav, marker, shot_name in (
            ("Templates", "p3-demo-template", "ux-templates"),
            ("Component Templates", "p3-demo-component", "ux-component"),
        ):
            try:
                click_nav(page, nav)
                wait_text(page, marker, timeout=20000)
                rows_n = page.locator("table tbody tr").count()
                check(f"{nav} 列出了真实模板", rows_n > 5, rows_n)
                page.locator("table tbody tr").first.locator('button:has-text("View")').click()
                page.wait_for_selector(".overlay .panel", timeout=20000)
                check(f"{nav} 的 View 在弹层里展示（不用滚到页尾）",
                      page.locator(".overlay .panel pre.json").count() == 1)
                shot(f"{shot_name}-view")
                page.keyboard.press("Escape")
                page.wait_for_timeout(300)
                check(f"{nav} 弹层按 Esc 可关闭", page.locator(".overlay .panel").count() == 0)
            except Exception as e:
                shot(f"{shot_name}-FAIL")
                check(f"{nav} 的 View 在弹层里展示（不用滚到页尾）", False, e)

        # --------------------------------------------------------- REST Console
        try:
            click_nav(page, "REST Console")
            wait_text(page, "Tab 缩进", timeout=20000)
            page.fill("input.path", "/products/_search")
            ta = page.locator("textarea.body")
            ta.click()
            page.keyboard.type('{"size":1}')
            before = ta.input_value()
            page.keyboard.press("Tab")
            page.wait_for_timeout(250)
            after = ta.input_value()
            focused = page.evaluate("document.activeElement && document.activeElement.tagName")
            check("REST Console Tab 插入缩进", len(after) == len(before) + 2, f"{len(before)} -> {len(after)}")
            check("REST Console Tab 后光标仍在编辑器", focused == "TEXTAREA", focused)
            page.keyboard.press("Control+Enter")
            wait_text(page, "HTTP 200", timeout=20000)
            check("REST Console Ctrl+Enter 发送并拿到 200", "HTTP 200" in page.inner_text("div.page"), "")
            shot("ux-rest")
        except Exception as e:
            shot("ux-rest-FAIL")
            check("REST Console Tab 插入缩进", False, e)

        browser.close()

    report = {"base_url": base_url, "results": results, "console": console}
    (out / "ux-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    passed = sum(1 for r in results if r["ok"])
    print(f"\n=== UX regression @ {base_url} -> {out} ===")
    for r in results:
        status = "OK " if r["ok"] else "FAIL"
        extra = f"  ({str(r.get('error'))[:140]})" if not r["ok"] and r.get("error") else ""
        print(f"  [{status}] {r['step']}{extra}")
    print(f"  steps passed: {passed}/{len(results)}")
    print(f"  console errors/warnings: {len(console)}")
    for c in console[:20]:
        print("    - " + c[:200])
    return report


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://127.0.0.1:5199"))
    ap.add_argument("--out", default=os.environ.get("OUT", "screenshots/ux"))
    args = ap.parse_args()
    run(args.base_url, args.out)
