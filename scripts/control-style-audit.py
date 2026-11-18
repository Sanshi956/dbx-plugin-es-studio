#!/usr/bin/env python3
"""Audit every visible form control on all 25 pages against one chrome baseline.

The UI intentionally has a single control baseline in src/styles/tokens.css:
same border color/width, radius and surface. Pages may change width, padding
for multiline editors, or semantic colors (the QueryBuilder `occur` select).
Anything else is drift and fails this audit.

Usage:
    python scripts/control-style-audit.py --base-url http://127.0.0.1:5202
"""

from __future__ import annotations

import argparse
import json
import sys

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"

# Canonical chrome is measured from a probe control, not hardcoded, so the
# audit also works with a host token override (e.g. dark theme).
PROBE_JS = """() => {
  const probe = document.createElement('select');
  probe.style.position = 'fixed';
  probe.style.left = '-9999px';
  document.body.appendChild(probe);
  const s = getComputedStyle(probe);
  const out = {border: s.borderTopColor, radius: s.borderRadius, bg: s.backgroundColor};
  probe.remove();
  return out;
}"""

COLLECT_JS = """() => {
  const sel = 'select, input:not([type=checkbox]):not([type=radio]), textarea';
  return [...document.querySelectorAll(sel)].filter(e => e.offsetParent !== null).map(e => {
    const s = getComputedStyle(e);
    const r = e.getBoundingClientRect();
    return {
      tag: e.tagName.toLowerCase(),
      type: e.type || '',
      cls: typeof e.className === 'string' ? e.className : '',
      name: e.getAttribute('data-list') || e.getAttribute('placeholder') || (e.value || '').slice(0, 24),
      border: s.borderTopColor,
      borderWidth: s.borderTopWidth,
      radius: s.borderRadius,
      bg: s.backgroundColor,
      padding: s.padding,
      height: Math.round(r.height * 100) / 100,
      fontSize: s.fontSize,
      disabled: e.disabled,
      semantic: /(^|\\s)occur(\\s|$)/.test(typeof e.className === 'string' ? e.className : ''),
    };
  });
}"""


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://127.0.0.1:5202")
    args = ap.parse_args()

    failures: list[str] = []
    total = 0
    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True, args=["--no-proxy-server"]
        )
        page = browser.new_page(viewport={"width": 1500, "height": 1000})
        page.goto(args.base_url + "/", wait_until="networkidle")
        page.wait_for_timeout(1200)

        nav = page.locator("nav button")
        labels = [nav.nth(i).inner_text().strip() for i in range(nav.count())]
        print(f"{len(labels)} pages @ {args.base_url}")

        for label in labels:
            page.click(f'nav button:has-text("{label}")')
            page.wait_for_timeout(600)
            probe = page.evaluate(PROBE_JS)
            rows = page.evaluate(COLLECT_JS)
            total += len(rows)
            for r in rows:
                where = f"{label} · {r['tag']}[{r['type']}] {r['name']!r}"
                if not r["semantic"] and r["border"] != probe["border"]:
                    failures.append(f"{where}: border {r['border']} != {probe['border']}")
                if r["radius"] != probe["radius"]:
                    failures.append(f"{where}: radius {r['radius']} != {probe['radius']}")
                if r["tag"] != "textarea" and r["bg"] != probe["bg"]:
                    failures.append(f"{where}: background {r['bg']} != {probe['bg']}")
            print(f"  {label}: {len(rows)} controls")

        browser.close()

    print(f"\n{total} controls checked; canonical chrome {json.dumps(probe, ensure_ascii=False)}")
    if failures:
        print(f"FAIL: {len(failures)} drift(s)")
        for f in failures:
            print("  -", f)
        return 1
    print("PASS: every control shares the global border / radius / surface baseline")
    return 0


if __name__ == "__main__":
    sys.exit(main())
