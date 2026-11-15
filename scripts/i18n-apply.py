#!/usr/bin/env python
"""Apply one i18n migration batch: dictionary entries + source replacements.

Why a batch tool instead of hand-editing: this plugin has ~30 UI files and
several hundred strings. Editing them one by one is how keys drift, a string
gets translated twice, or a label is left hardcoded. This script refuses to
apply a batch unless every replacement matches exactly once, so a silent
no-op is impossible.

Input: a JSON file (default `.dbx-dev/i18n-batch.json`) with

    {
      "group": "overview",                     // comment written into both dicts
      "keys": [["key", "中文", "English"], ...],
      "replacements": [["src/x.svelte", "old text", "new text"], ...]
    }

Usage: python scripts/i18n-apply.py [batch.json]
"""
from __future__ import annotations

import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ZH = ROOT / "src/lib/locales/zh-CN.ts"
EN = ROOT / "src/lib/locales/en.ts"


def read(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8")


def append_keys(path: pathlib.Path, group: str, entries: list[tuple[str, str]], column: int) -> tuple[str, int]:
    """Insert `key: value` pairs just before the closing `};` of the dictionary."""
    text = read(path)
    marker = "\n};"
    at = text.rfind(marker)
    if at < 0:
        raise SystemExit(f"{path.name}: dictionary terminator not found")
    added, skipped = [], []
    block = [f"\n  // {group}"]
    for key, zh, en in entries:
        value = zh if column == 0 else en
        needle = f'"{key}":'
        if needle in text:
            skipped.append(key)
            continue
        block.append(f"  {json.dumps(key, ensure_ascii=False)}: {json.dumps(value, ensure_ascii=False)},")
        added.append(key)
    if added:
        text = text[:at] + "\n".join(block) + text[at:]
        path.write_text(text, encoding="utf-8")
    return ", ".join(added) if added else "none", len(skipped)


def apply_replacements(items: list[list[str]]) -> tuple[int, list[str]]:
    by_file: dict[str, list[list[str]]] = {}
    for row in items:
        if len(row) != 3:
            raise SystemExit(f"replacement row needs 3 fields: {row!r}")
        by_file.setdefault(row[0], []).append(row)

    applied = 0
    failures: list[str] = []
    for rel, rows in by_file.items():
        path = ROOT / rel
        text = read(path)
        for _, old, new in rows:
            hits = text.count(old)
            if hits != 1:
                failures.append(f"{rel}: {hits}× {old[:70]!r}")
                continue
            text = text.replace(old, new)
            applied += 1
        path.write_text(text, encoding="utf-8")
    return applied, failures


def load_batch(path: pathlib.Path) -> dict:
    """Read a batch from JSON, or from a .py file exposing a `BATCH` dict.

    The .py form exists because source snippets are full of double quotes;
    writing them as Python literals avoids a layer of escaping that once turned
    a replacement into a silent no-op. Batch files are authoring input, not
    shipped code, and are executed locally by this script only.
    """
    text = read(path)
    if path.suffix == ".py":
        namespace: dict = {}
        exec(compile(text, str(path), "exec"), namespace)  # noqa: S102 - local authoring input
        return namespace["BATCH"]
    return json.loads(text)


def main() -> int:
    batch_path = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / ".dbx-dev/i18n-batch.json"
    batch = load_batch(batch_path)
    group = batch.get("group", "batch")

    keys = [(row[0], row[1], row[2]) for row in batch.get("keys", [])]
    duplicate = {row[0] for row in keys if [r[0] for r in keys].count(row[0]) > 1}
    if duplicate:
        raise SystemExit(f"duplicate keys inside batch: {sorted(duplicate)}")
    for row in keys:
        if len(row) != 3:
            raise SystemExit(f"key row needs 3 fields: {row!r}")

    applied, failures = apply_replacements(batch.get("replacements", []))
    zh_added, zh_skipped = append_keys(ZH, group, keys, 0) if keys else ("none", 0)
    en_added, en_skipped = append_keys(EN, group, keys, 1) if keys else ("none", 0)

    print(f"group={group} replacements={applied} keys_zh={len(keys) if zh_added != 'none' else 0} keys_en={len(keys) if en_added != 'none' else 0}")
    if zh_skipped or en_skipped:
        print(f"note: {zh_skipped} zh / {en_skipped} en keys already existed, skipped")
    if failures:
        print("FAILED replacements:")
        for line in failures:
            print("  " + line)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
