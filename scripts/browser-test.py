#!/usr/bin/env python3
# Browser test for ES Studio P2 UI WITHOUT the DBX host.
# Drives the standalone bridge (scripts/browser-bridge.mjs) with Playwright +
# the system Chrome, clicks through every P2 page against the REAL Elasticsearch
# cluster (seeded with sample data), and screenshots each one.
#
#   python scripts/browser-test.py [--base-url http://localhost:5199] [--out screenshots/rw]
#   set RO_DELETE=1 to additionally perform a DELETE on the REST Console and
#   assert the read-only rejection (-32003) is surfaced in the UI.
import os, sys, json, time, argparse
from pathlib import Path

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"

# (nav_label, expected_text, *fallback_text)
#
# The fallback is a version-gate marker. `p3-demo-*` fixtures require 7.8/7.9, so
# on an older cluster the three gated pages cannot contain them - but they must
# still render and explain themselves. Waiting only for the fixture turned a
# correct gate into a red "FAIL", which is how a real regression would hide.
PAGES = [
    ("集群概览", "es-studio-test"),
    ("节点", "es-studio-node-1"),
    ("分片", "include._name"),
    ("索引列表", "archived-2023"),
    ("别名", "prod-view"),
    # P3
    ("集群设置", "写入 / 移除设置"),
    ("索引模板", "p3-demo-template", "需要 Elasticsearch 7.8"),
    ("组件模板", "p3-demo-component", "需要 Elasticsearch 7.8"),
    ("数据流", "logs-p3-demo", "需要 Elasticsearch 7.9"),
    ("生命周期", "p3-demo-policy"),
    ("查询剖析", "运行剖析"),
    ("快照", "p3-fs-repo"),
    ("任务", "在途节点任务"),
]


def wait_text(page, text, timeout=20000):
    page.wait_for_function(
        "t => document.body && document.body.innerText.includes(t)",
        arg=text, timeout=timeout,
    )


def wait_any(page, texts, timeout=20000):
    """First of `texts` to appear, returned; raises on timeout."""
    page.wait_for_function(
        "(ts) => ts.some(t => document.body && document.body.innerText.includes(t))",
        arg=list(texts), timeout=timeout,
    )
    body = page.inner_text("body")
    return next(t for t in texts if t in body)


def click_nav(page, label):
    page.click(f'nav button:has-text("{label}")')
    page.wait_for_timeout(400)


def run(base_url, out_dir, ro_delete=False):
    out = Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results = []
    console_errors = []

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 1500})
        page.on("console", lambda m: console_errors.append(f"[{m.type}] {m.text}") if m.type in ("error", "warning") else None)
        page.on("pageerror", lambda e: console_errors.append(f"[pageerror] {e}"))

        def shot(name):
            page.screenshot(path=str(out / f"{name}.png"), full_page=True)

        # ---- boot ----
        rec = {"step": "boot", "ok": False}
        try:
            page.goto(base_url + "/", wait_until="domcontentloaded", timeout=30000)
            # wait for the first page to render (the picker that used to carry the
            # connection name is gone; the workbench tab title carries it now)
            wait_text(page, "集群概览", timeout=30000)
            title = page.title()
            if title != "Local ES (test)":
                raise AssertionError(f"workbench tab title should be the connection name, got {title!r}")
            time.sleep(0.6)
            shot("00-boot")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("00-boot-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- display pages ----
        for label, expect, *fallback in PAGES:
            rec = {"step": f"nav:{label}", "expect": expect, "ok": False}
            try:
                click_nav(page, label)
                matched = wait_any(page, [expect, *fallback], timeout=20000)
                if matched != expect:
                    # Gated page: the fixture cannot exist on this version. Keep
                    # the evidence in the report instead of silently passing.
                    rec["gated_by"] = matched
                time.sleep(0.3)
                shot("pg-" + label.split()[0].lower())
                rec["ok"] = True
            except Exception as e:
                rec["error"] = str(e)
                try: shot("pg-" + label.split()[0].lower() + "-FAIL")
                except Exception: pass
            results.append(rec)

        # ---- Browser: field list from mapping + keyword search + field scoping ----
        rec = {"step": "interact:browser", "expect": "field list + keyword search", "ok": False}
        try:
            click_nav(page, "文档浏览")
            page.locator('label:has-text("索引") select').select_option("products")

            # The field dropdown must be fed by GET /products/_mapping, so the
            # user never has to type a field name by hand.
            # <option> inside a closed <select> has no layout box, so it never
            # counts as "visible" — wait for attachment instead.
            page.wait_for_selector(
                'label:has-text("搜索字段") select option[value="name"]',
                state="attached", timeout=20000,
            )
            opts = page.locator('label:has-text("搜索字段") select option').all_inner_texts()
            assert any(o.startswith("全部字段") for o in opts), opts
            for expect_field in ("name", "name.kw", "category", "price"):
                assert any(o.startswith(expect_field + " ·") for o in opts), (expect_field, opts)
            rec["fields"] = opts

            # 1) empty keyword -> match_all
            page.get_by_role("button", name="搜索文档", exact=True).click()
            wait_text(page, "match_all", timeout=20000)

            # 2) keyword with no field -> multi_match over all fields
            page.locator("label.grow input").fill("Mouse")
            page.get_by_role("button", name="搜索文档", exact=True).click()
            wait_text(page, "Wireless Mouse", timeout=20000)
            shot("ix-browser-search-all")

            # 3) scope to a concrete field -> metadata says which field matched
            page.locator('label:has-text("搜索字段") select').select_option("name")
            page.get_by_role("button", name="搜索文档", exact=True).click()
            wait_text(page, 'name ~ "Mouse"', timeout=20000)
            shot("ix-browser-search-field")

            # 4) scoping must actually narrow: "Mouse" is not in category
            page.locator('label:has-text("搜索字段") select').select_option("category")
            page.get_by_role("button", name="搜索文档", exact=True).click()
            wait_text(page, "没有匹配的文档", timeout=20000)
            shot("ix-browser-scoped-empty")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-browser-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Query Builder (match name=Mouse) ----
        rec = {"step": "interact:querybuilder", "expect": "Wireless Mouse", "ok": False}
        try:
            click_nav(page, "查询构建器")
            page.locator("section.bar select").first.select_option("products")
            page.wait_for_function(
                "() => { const el = document.querySelector('.fcount');"
                " return el && /[0-9]+ fields/.test(el.innerText); }",
                timeout=20000,
            )
            page.click('button:has-text("+ 加条件")')
            page.locator("div.clause select.field").first.select_option("name")
            page.wait_for_timeout(200)
            page.locator("div.clause input.value").first.fill("Mouse")
            page.get_by_role("button", name="执行查询", exact=True).click()
            wait_text(page, "Wireless Mouse", timeout=20000)
            time.sleep(0.3)
            shot("ix-querybuilder-match")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-querybuilder-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- DSL Editor (default match_all) ----
        rec = {"step": "interact:dsleditor", "expect": "total", "ok": False}
        try:
            click_nav(page, "DSL 编辑器")
            page.get_by_role("button", name="运行", exact=True).click()
            wait_text(page, "total", timeout=20000)
            time.sleep(0.3)
            shot("ix-dsleditor-run")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-dsleditor-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Analyzer ----
        rec = {"step": "interact:analyzer", "expect": "词元", "ok": False}
        try:
            click_nav(page, "分词分析")
            page.locator("section.form select").first.select_option("products")
            page.wait_for_selector(
                'section.form select option[value="name"]', state="attached", timeout=20000
            )
            page.locator("section.form select").nth(1).select_option("name")
            page.get_by_role("button", name="分析", exact=True).click()
            wait_text(page, "词元", timeout=20000)
            time.sleep(0.3)
            shot("ix-analyzer")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-analyzer-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- REST Console (GET /_cluster/health) ----
        rec = {"step": "interact:restconsole", "expect": "cluster_name", "ok": False}
        try:
            click_nav(page, "REST 控制台")
            page.get_by_role("button", name="发送", exact=True).click()
            wait_text(page, "cluster_name", timeout=20000)
            time.sleep(0.3)
            shot("ix-restconsole-health")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-restconsole-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- optional read-only rejection demo ----
        if ro_delete:
            rec = {"step": "readonly:delete-rejected", "expect": "-32003", "ok": False}
            try:
                click_nav(page, "REST 控制台")
                # Two <select> exist on the page (connection picker + method):
                # target the request bar explicitly.
                page.locator("section.bar select").select_option("DELETE")
                page.locator('input.path').fill("/products")
                page.get_by_role("button", name="发送", exact=True).click()
                wait_text(page, "-32003", timeout=20000)
                time.sleep(0.3)
                shot("ro-restconsole-delete-rejected")
                rec["ok"] = True
            except Exception as e:
                rec["error"] = str(e)
                try: shot("ro-restconsole-FAIL")
                except Exception: pass
            results.append(rec)

        # ---- Index List danger-confirm modal (open, then cancel) ----
        rec = {"step": "interact:indexlist-danger-modal", "expect": "ES REST", "ok": False}
        try:
            click_nav(page, "索引列表")
            page.locator('table.ix tbody tr:has-text("products") button:has-text("关闭索引")').click()
            wait_text(page, "ES REST", timeout=10000)
            time.sleep(0.3)
            shot("ix-indexlist-danger")
            # dismiss without confirming
            try:
                page.get_by_role("button", name="取消", exact=True).click()
            except Exception:
                page.keyboard.press("Escape")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-indexlist-danger-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Allocation explain on the merged Shards page: expand, then collapse ----
        rec = {"step": "interact:allocation-explain", "expect": "explain panel toggles", "ok": False}
        try:
            click_nav(page, "分片与分配")
            page.get_by_role("button", name="解析未分配分片").click()
            page.wait_for_selector(".explain", timeout=20000)
            text = page.locator(".explain").inner_text()
            assert ("allocate" in text.lower()) or ("shard" in text.lower()), text
            page.get_by_role("button", name="收起解析").first.click()
            page.wait_for_selector(".explain", state="detached", timeout=10000)
            time.sleep(0.3)
            shot("ix-allocation-explain")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-allocation-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Query Profiler: real profile tree on products ----
        rec = {"step": "interact:profiler", "expect": "查询树", "ok": False}
        try:
            click_nav(page, "查询剖析")
            page.locator("section.bar select").select_option("products")
            page.get_by_role("button", name="运行剖析").click()
            # the <h2> is text-transformed to uppercase, and innerText follows CSS
            wait_text(page, "查询树", timeout=20000)
            time.sleep(0.3)
            shot("ix-profiler")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-profiler-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Tasks: needs a REAL in-flight task (scripts/es-task-demo.sh start) ----
        if os.environ.get("TASK_DEMO") == "1":
            rec = {"step": "interact:tasks-live", "expect": "indices:data/write/reindex", "ok": False}
            try:
                click_nav(page, "任务")
                wait_text(page, "indices:data/write/reindex", timeout=20000)
                row = page.locator("tr", has_text="indices:data/write/reindex").first
                assert row.locator("button", has_text="取消").is_enabled(), "Cancel should be enabled"
                time.sleep(0.3)
                shot("ix-tasks-live")
                rec["ok"] = True
            except Exception as e:
                rec["error"] = str(e)
                try: shot("ix-tasks-live-FAIL")
                except Exception: pass
            results.append(rec)

        browser.close()

    report = {"base_url": base_url, "results": results, "console_errors": console_errors}
    (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    # human summary
    passed = sum(1 for r in results if r.get("ok"))
    print(f"\n=== Browser test @ {base_url} -> {out} ===")
    for r in results:
        status = "OK " if r.get("ok") else "FAIL"
        extra = ""
        if not r.get("ok") and r.get("error"):
            extra = f"  ({r['error'][:120]})"
        print(f"  [{status}] {r['step']}{extra}")
    print(f"  steps passed: {passed}/{len(results)}")
    print(f"  console errors/warnings captured: {len(console_errors)}")
    for c in console_errors[:40]:
        print("    - " + c[:200])
    return report


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:5199"))
    ap.add_argument("--out", default=os.environ.get("OUT", "screenshots/rw"))
    ap.add_argument("--ro-delete", action="store_true", default=os.environ.get("RO_DELETE") == "1")
    args = ap.parse_args()
    run(args.base_url, args.out, ro_delete=args.ro_delete)
