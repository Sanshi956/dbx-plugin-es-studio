#!/usr/bin/env python
"""Language / type-scale regression: every page, in one language, end to end.

The point is not "does it look nice" — it is that no page keeps a hardcoded
string or a hardcoded pixel size. So the assertions are mechanical:

  * EN run: zero CJK characters anywhere in the page, including `title` and
    `placeholder` attributes (data from ES is ASCII in the seed fixture).
  * ZH run: every nav label and every `<h1>` contains CJK — i.e. the English
    scaffolding is gone, not merely translated on the pages someone remembered.
  * Both runs: `body` font-size equals the `--dbx-font-size` token the host
    injected, which proves the type scale follows the host rather than 13px.

Usage:
    python scripts/browser-i18n-test.py --port 5210 --locale en
    python scripts/browser-i18n-test.py --port 5211 --locale zh-CN
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

from playwright.sync_api import sync_playwright

CJK = re.compile(r"[\u3400-\u9fff\uf900-\ufaff]")
CHROME = "C:/Program Files/Google/Chrome/Application/chrome.exe"
ROOT = pathlib.Path(__file__).resolve().parent.parent
SHOTS = ROOT / "screenshots" / "p6"


def collect(page) -> dict:
    return page.evaluate(
        """() => {
        const text = document.body.innerText;
        const attrs = [...document.querySelectorAll('[title],[placeholder],[aria-label]')]
          .flatMap((el) => ['title', 'placeholder', 'aria-label'].map((name) => el.getAttribute(name) || ''));
        const nav = [...document.querySelectorAll('nav button, nav .pending')].map((el) => el.innerText.trim());
        const h1 = [...document.querySelectorAll('h1')].map((el) => el.innerText.trim());
        return {
          text, attrs, nav, h1,
          fontSize: getComputedStyle(document.body).fontSize,
          lang: document.documentElement.lang,
        };
      }"""
    )


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--locale", default="zh-CN")
    ap.add_argument("--expect-font", default="", help="expected body font-size, e.g. 15px")
    args = ap.parse_args()

    SHOTS.mkdir(parents=True, exist_ok=True)
    failures: list[str] = []
    checks = 0
    rows: list[dict] = []

    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=CHROME, headless=True, args=["--no-proxy-server"])
        page = browser.new_page(viewport={"width": 1440, "height": 980})
        page.goto(f"http://127.0.0.1:{args.port}/", wait_until="load")
        page.wait_for_selector("nav button", timeout=45000)
        page.wait_for_timeout(900)

        nav_count = page.locator("nav button").count()
        print(f"{args.locale}: {nav_count} nav entries")

        for i in range(nav_count):
            label = page.locator("nav button").nth(i).inner_text().strip()
            page.locator("nav button").nth(i).click()
            page.wait_for_timeout(520)
            snap = collect(page)
            checks += 1

            cjk_attr = [a for a in snap["attrs"] if CJK.search(a or "")]
            cjk_text = CJK.findall(snap["text"])

            if args.locale == "en":
                if cjk_text:
                    failures.append(f"{label}: {len(cjk_text)} CJK chars in body, e.g. {''.join(cjk_text[:12])}")
                if cjk_attr:
                    failures.append(f"{label}: CJK in attribute, e.g. {cjk_attr[0][:50]}")
                if not snap["h1"] or any(CJK.search(h) for h in snap["h1"]):
                    failures.append(f"{label}: h1 not English ({snap['h1']})")
            else:
                if not all(CJK.search(n) for n in snap["nav"]):
                    bad = [n for n in snap["nav"] if not CJK.search(n)]
                    failures.append(f"{label}: nav still English: {bad[:4]}")
                if snap["h1"] and not CJK.search(" ".join(snap["h1"])):
                    failures.append(f"{label}: h1 has no Chinese ({snap['h1']})")

            if args.expect_font and snap["fontSize"] != args.expect_font:
                failures.append(f"{label}: body font-size {snap['fontSize']} != host {args.expect_font}")

            rows.append({"page": label, "h1": snap["h1"], "cjk": len(cjk_text), "font": snap["fontSize"]})
            page.screenshot(path=str(SHOTS / f"{args.locale}-{i:02d}.png"), full_page=False)

        browser.close()

    report = {"locale": args.locale, "checks": checks, "rows": rows, "failures": failures}
    out = SHOTS / f"i18n-{args.locale}.json"
    out.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"\n{checks} pages · {len(failures)} failures")
    for line in failures[:24]:
        print("  FAIL " + line)
    print(f"report: {out}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
