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

PAGES = [
    # (nav_label, expected_text_in_rendered_page)
    ("Cluster Overview", "es-studio-test"),
    ("Nodes", "es-studio-node-1"),
    ("Shards", "products"),
    ("Index List", "archived-2023"),
    ("Aliases", "prod-view"),
    # P3
    ("Allocation", "include._name"),
    ("Cluster Settings", "Persistent"),
    ("Templates", "p3-demo-template"),
    ("Component Templates", "p3-demo-component"),
    ("Data Streams", "logs-p3-demo"),
    ("Lifecycle", "p3-demo-policy"),
    ("Query Profiler", "Run profile"),
    ("Snapshots", "p3-fs-repo"),
    ("Tasks", "In-flight node tasks"),
]


def wait_text(page, text, timeout=20000):
    page.wait_for_function(
        "t => document.body && document.body.innerText.includes(t)",
        arg=text, timeout=timeout,
    )


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
            # wait for the bridge to boot + first data (connection name in picker)
            wait_text(page, "Local ES (test)", timeout=30000)
            time.sleep(0.6)
            shot("00-boot")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("00-boot-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- display pages ----
        for label, expect in PAGES:
            rec = {"step": f"nav:{label}", "expect": expect, "ok": False}
            try:
                click_nav(page, label)
                wait_text(page, expect, timeout=20000)
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
            click_nav(page, "Browser")
            page.locator('label:has-text("Index") select').select_option("products")

            # The field dropdown must be fed by GET /products/_mapping, so the
            # user never has to type a field name by hand.
            # <option> inside a closed <select> has no layout box, so it never
            # counts as "visible" — wait for attachment instead.
            page.wait_for_selector(
                'label:has-text("Field") select option[value="name"]',
                state="attached", timeout=20000,
            )
            opts = page.locator('label:has-text("Field") select option').all_inner_texts()
            assert any(o.startswith("All fields") for o in opts), opts
            for expect_field in ("name", "name.kw", "category", "price"):
                assert any(o.startswith(expect_field + " ·") for o in opts), (expect_field, opts)
            rec["fields"] = opts

            # 1) empty keyword -> match_all
            page.get_by_role("button", name="Search", exact=True).click()
            wait_text(page, "match_all", timeout=20000)

            # 2) keyword with no field -> multi_match over all fields
            page.locator("label.grow input").fill("Mouse")
            page.get_by_role("button", name="Search", exact=True).click()
            wait_text(page, "Wireless Mouse", timeout=20000)
            shot("ix-browser-search-all")

            # 3) scope to a concrete field -> metadata says which field matched
            page.locator('label:has-text("Field") select').select_option("name")
            page.get_by_role("button", name="Search", exact=True).click()
            wait_text(page, 'name ~ "Mouse"', timeout=20000)
            shot("ix-browser-search-field")

            # 4) scoping must actually narrow: "Mouse" is not in category
            page.locator('label:has-text("Field") select').select_option("category")
            page.get_by_role("button", name="Search", exact=True).click()
            wait_text(page, "No documents matched", timeout=20000)
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
            click_nav(page, "Query Builder")
            page.locator('label:has-text("Index") select').select_option("products")
            page.locator(".clause select").first.select_option("match")
            page.locator(".clause .field").first.fill("name")
            page.locator(".clause .value").first.fill("Mouse")
            page.get_by_role("button", name="Run query", exact=True).click()
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
            click_nav(page, "DSL Editor")
            page.get_by_role("button", name="Run", exact=True).click()
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
        rec = {"step": "interact:analyzer", "expect": "tokens", "ok": False}
        try:
            click_nav(page, "Analyzer")
            page.get_by_role("button", name="Analyze", exact=True).click()
            wait_text(page, "tokens", timeout=20000)
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
            click_nav(page, "REST Console")
            page.get_by_role("button", name="Send", exact=True).click()
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
                click_nav(page, "REST Console")
                # Two <select> exist on the page (connection picker + method):
                # target the request bar explicitly.
                page.locator("section.bar select").select_option("DELETE")
                page.locator('input.path').fill("/products")
                page.get_by_role("button", name="Send", exact=True).click()
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
            click_nav(page, "Index List")
            page.locator('table.ix tbody tr:has-text("products") button:has-text("Close")').click()
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

        # ---- Allocation: explain an unassigned shard (read-only, real reason) ----
        rec = {"step": "interact:allocation-explain", "expect": "explain panel", "ok": False}
        try:
            click_nav(page, "Allocation")
            page.get_by_role("button", name="Explain unassigned shard").click()
            page.wait_for_selector(".explain", timeout=20000)
            text = page.locator(".explain").inner_text()
            assert ("allocate" in text.lower()) or ("shard" in text.lower()), text
            time.sleep(0.3)
            shot("ix-allocation-explain")
            rec["ok"] = True
        except Exception as e:
            rec["error"] = str(e)
            try: shot("ix-allocation-FAIL")
            except Exception: pass
        results.append(rec)

        # ---- Query Profiler: real profile tree on products ----
        rec = {"step": "interact:profiler", "expect": "Query tree", "ok": False}
        try:
            click_nav(page, "Query Profiler")
            page.locator("section.bar select").select_option("products")
            page.get_by_role("button", name="Run profile").click()
            # the <h2> is text-transformed to uppercase, and innerText follows CSS
            wait_text(page, "QUERY TREE", timeout=20000)
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
                click_nav(page, "Tasks")
                wait_text(page, "indices:data/write/reindex", timeout=20000)
                row = page.locator("tr", has_text="indices:data/write/reindex").first
                assert row.locator("button", has_text="Cancel").is_enabled(), "Cancel should be enabled"
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
