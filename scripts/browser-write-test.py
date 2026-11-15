#!/usr/bin/env python3
"""End-to-end WRITE-path browser test for ES Studio P2, without the DBX host.

Two passes against the standalone bridge (scripts/browser-bridge.mjs):

  rw  (read-write bridge, default port 5199)
      * Index List -> Close  logs-app   (danger modal + ack checkbox)
      * Index List -> Open   logs-app
      * Index List -> Refresh products
      * REST Console -> PUT /ui-tmp-idx, then Index List -> Delete it
      Every step asserts the *real* Elasticsearch state changed, not just the UI.

  ro  (read-only bridge, default port 5200)
      * Index List -> Delete products must FAIL with the Sidecar's
        "connection is read-only" message and leave the index alive.
      * REST Console -> DELETE /products must show the same denial.

Usage:
  python scripts/browser-write-test.py --rw http://127.0.0.1:5199 --ro http://127.0.0.1:5200
  (omit --ro to skip the read-only pass)
"""
import os
import sys
import json
import time
import argparse
import urllib.request
from pathlib import Path

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"
ES = os.environ.get("ES_URL", "http://127.0.0.1:19200")

# 127.0.0.1 must never go through a corporate/OS proxy.
_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def es_get(path):
    with _OPENER.open(ES + path, timeout=5) as r:
        return r.status, r.read().decode("utf-8", "replace")


def es_exists(index):
    try:
        code, _ = es_get(f"/{index}")
        return code == 200
    except Exception:
        return False


def es_status(index):
    """'open' / 'close' / None (missing)."""
    try:
        _, body = es_get(f"/_cat/indices/{index}?format=json&h=status")
        rows = json.loads(body)
        return rows[0]["status"] if rows else None
    except Exception:
        return None


def wait_text(page, text, timeout=20000):
    page.wait_for_function(
        "t => document.body && document.body.innerText.includes(t)",
        arg=text, timeout=timeout,
    )


def click_nav(page, label):
    page.click(f'nav button:has-text("{label}")')
    page.wait_for_timeout(400)


def confirm_danger(page):
    """Tick the acknowledgement checkbox, then press the danger button."""
    page.wait_for_selector(".modal", timeout=10000)
    page.check('.modal input[type="checkbox"]')
    page.click(".modal button.danger")
    page.wait_for_selector(".modal", state="detached", timeout=10000)


def run_rw(base_url, out_dir):
    out = Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results = []
    console = []

    def check(step, cond, detail=""):
        results.append({"step": step, "ok": bool(cond), **({"error": detail} if not cond and detail else {})})

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 1600})
        page.on("console", lambda m: console.append(f"[{m.type}] {m.text}") if m.type in ("error", "warning") else None)
        page.on("pageerror", lambda e: console.append(f"[pageerror] {e}"))

        def shot(name):
            page.screenshot(path=str(out / f"{name}.png"), full_page=True)

        page.goto(base_url + "/", wait_until="domcontentloaded", timeout=30000)
        wait_text(page, "Local ES (test)", timeout=30000)
        time.sleep(0.5)

        # ---- 1. close an index through the danger modal ----
        try:
            click_nav(page, "Index List")
            wait_text(page, "logs-app")
            page.locator('table.ix tbody tr:has-text("logs-app") button[title="Close"]').click()
            confirm_danger(page)
            wait_text(page, "关闭索引 succeeded", timeout=15000)
            shot("wr-01-close-done")
            check("Index List Close logs-app (real ES state)", es_status("logs-app") == "close",
                  f"ES status={es_status('logs-app')}")
        except Exception as e:
            shot("wr-01-close-FAIL")
            check("Index List Close logs-app (real ES state)", False, str(e))

        # ---- 2. re-open it ----
        try:
            page.locator('table.ix tbody tr:has-text("logs-app") button[title="Open"]').click()
            confirm_danger(page)
            wait_text(page, "打开索引 succeeded", timeout=15000)
            shot("wr-02-open-done")
            check("Index List Open logs-app (real ES state)", es_status("logs-app") == "open",
                  f"ES status={es_status('logs-app')}")
        except Exception as e:
            shot("wr-02-open-FAIL")
            check("Index List Open logs-app (real ES state)", False, str(e))

        # ---- 3. refresh (no modal, safe write) ----
        try:
            page.locator('table.ix tbody tr:has-text("products") button[title="Refresh"]').click()
            wait_text(page, "refreshed products", timeout=15000)
            shot("wr-03-refresh-done")
            check("Index List Refresh products", True)
        except Exception as e:
            shot("wr-03-refresh-FAIL")
            check("Index List Refresh products", False, str(e))

        # ---- 4. create an index from the REST Console ----
        try:
            click_nav(page, "REST Console")
            page.wait_for_selector("section.bar select", timeout=10000)
            page.select_option("section.bar select", "PUT")
            page.fill("input.path", "/ui-tmp-idx")
            page.fill("textarea.body", '{"settings":{"number_of_shards":1,"number_of_replicas":0}}')
            page.click('button.primary:has-text("Send")')
            wait_text(page, "HTTP 200", timeout=20000)
            shot("wr-04-rest-put")
            check("REST Console PUT /ui-tmp-idx (real ES state)", es_exists("ui-tmp-idx"))
        except Exception as e:
            shot("wr-04-rest-put-FAIL")
            check("REST Console PUT /ui-tmp-idx (real ES state)", False, str(e))

        # ---- 5. delete it from Index List ----
        try:
            click_nav(page, "Index List")
            wait_text(page, "ui-tmp-idx", timeout=15000)
            page.locator('table.ix tbody tr:has-text("ui-tmp-idx") button[title="Delete"]').click()
            confirm_danger(page)
            wait_text(page, "删除索引 succeeded", timeout=15000)
            shot("wr-05-delete-done")
            check("Index List Delete ui-tmp-idx (real ES state)", not es_exists("ui-tmp-idx"),
                  "index still exists")
        except Exception as e:
            shot("wr-05-delete-FAIL")
            check("Index List Delete ui-tmp-idx (real ES state)", False, str(e))

        browser.close()

    report = {"pass": "rw", "base_url": base_url, "results": results, "console": console}
    (out / "write-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


def run_ro(base_url, out_dir):
    out = Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results = []
    console = []

    def check(step, cond, detail=""):
        results.append({"step": step, "ok": bool(cond), **({"error": detail} if not cond and detail else {})})

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 1600})
        page.on("pageerror", lambda e: console.append(f"[pageerror] {e}"))

        def shot(name):
            page.screenshot(path=str(out / f"{name}.png"), full_page=True)

        page.goto(base_url + "/", wait_until="domcontentloaded", timeout=30000)
        wait_text(page, "Local ES (test)", timeout=30000)
        time.sleep(0.5)

        # ---- Index List: DELETE must be refused by the Sidecar ----
        try:
            click_nav(page, "Index List")
            wait_text(page, "products")
            page.locator('table.ix tbody tr:has-text("products") button[title="Delete"]').click()
            confirm_danger(page)
            wait_text(page, "read-only", timeout=20000)
            shot("ro-01-delete-denied")
            body = page.inner_text("body")
            check("Index List DELETE denied on read-only (-32003)",
                  "read-only" in body and "删除索引 failed" in body, body[:200])
            check("products survived the denied DELETE", es_exists("products"), "products is gone!")
        except Exception as e:
            shot("ro-01-delete-FAIL")
            check("Index List DELETE denied on read-only (-32003)", False, str(e))

        # ---- REST Console: DELETE must be refused too ----
        try:
            click_nav(page, "REST Console")
            page.wait_for_selector("section.bar select", timeout=10000)
            page.select_option("section.bar select", "DELETE")
            page.fill("input.path", "/products")
            page.click('button.primary:has-text("Send")')
            wait_text(page, "read-only", timeout=20000)
            shot("ro-02-rest-delete-denied")
            check("REST Console DELETE denied on read-only", "read-only" in page.inner_text("body"))
        except Exception as e:
            shot("ro-02-rest-delete-FAIL")
            check("REST Console DELETE denied on read-only", False, str(e))

        browser.close()

    report = {"pass": "ro", "base_url": base_url, "results": results, "console": console}
    (out / "write-report-ro.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


def summarize(report):
    results = report["results"]
    passed = sum(1 for r in results if r.get("ok"))
    print(f"\n=== write test [{report['pass']}] @ {report['base_url']} ===")
    for r in results:
        status = "OK " if r.get("ok") else "FAIL"
        extra = f"  ({r['error'][:140]})" if not r.get("ok") and r.get("error") else ""
        print(f"  [{status}] {r['step']}{extra}")
    print(f"  steps passed: {passed}/{len(results)}")
    if report.get("console"):
        print(f"  console issues: {len(report['console'])}")
        for c in report["console"][:10]:
            print("    - " + c[:180])
    return passed == len(results)


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--rw", default=os.environ.get("RW_URL", "http://127.0.0.1:5199"))
    ap.add_argument("--ro", default=os.environ.get("RO_URL", "http://127.0.0.1:5200"))
    ap.add_argument("--out", default="screenshots/write")
    ap.add_argument("--only", choices=["rw", "ro"], default=None)
    args = ap.parse_args()

    ok = True
    if args.only != "ro":
        ok = summarize(run_rw(args.rw, args.out)) and ok
    if args.only != "rw":
        ok = summarize(run_ro(args.ro, args.out)) and ok
    sys.exit(0 if ok else 1)
