#!/usr/bin/env python3
"""Preflight a DBX manifest against the official schema + runtime rules.

`dbx-plugin package .` validates packaging, but DBX performs additional checks at
install time (after the signature check). Failures there are confusing because the
package already looks valid. This script reproduces the important ones so they are
caught before packaging/signing.

Checks:
  * official plugins/manifest.schema.json (vendored copy in scripts/)
  * identifier / locale / permission / menu-group / enum whitelists
  * contribution cross references (workbench, command, context menu)
  * icon and backend executable presence (inside a .dbxp when --package is used)

Usage:
    python scripts/check-manifest.py
    python scripts/check-manifest.py --package dist/local.es.head-1.0.0-windows-x64.signed.dbxp
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SCHEMA_PATH = Path(__file__).resolve().parent / "manifest.schema.json"

SUPPORTED_MANIFEST_VERSION = 1
SUPPORTED_PROTOCOL_VERSION = 1
SUPPORTED_HOST_API = "1.4.0"
PERMISSIONS = {
    "host.events", "host.binary", "host.workbench", "host.filesystem",
    "host.plans:read", "host.schema:read", "host.storage", "host.ai",
    "host.clipboard:read", "host.data:read", "host.connections:read",
    "host.connections::read",
}
MENU_GROUPS = {"navigation", "primary", "secondary", "destructive"}
MENU_LOCATIONS = {"commandPalette", "appToolbar", "appSidebar"}
CONTRIBUTION_TYPES = {
    "connection-provider", "workbench", "filesystem-provider", "context-menu",
    "result-view", "command", "menus", "mcp",
}
PRESENTATIONS = {"tab", "panel"}
CONTEXT_MENUS = {"connection", "table"}
IDENTIFIER = re.compile(r"^[a-z0-9][a-z0-9._-]*$")


def valid_identifier(value: str) -> bool:
    return bool(value) and len(value) <= 128 and bool(IDENTIFIER.match(value))


def valid_locale(value: str) -> bool:
    parts = value.split("-")
    return (
        2 <= len(parts[0]) <= 3
        and parts[0].isalpha()
        and all(2 <= len(part) <= 8 and part.isalnum() for part in parts[1:])
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="DBX manifest preflight")
    parser.add_argument("--package", type=Path, help="Also validate the manifest inside a .dbxp")
    parser.add_argument("--manifest", type=Path, default=ROOT / "manifest.json")
    args = parser.parse_args()

    errors: list[str] = []
    checks = 0

    def check(condition: bool, message: str) -> None:
        nonlocal checks
        checks += 1
        if not condition:
            errors.append(message)

    source = json.loads(args.manifest.read_text(encoding="utf-8"))
    documents: list[tuple[str, dict, set[str]]] = [("source", source, set())]

    if args.package:
        with zipfile.ZipFile(args.package) as archive:
            names = {n for n in archive.namelist() if not n.endswith("/")}
            packaged = json.loads(archive.read("manifest.json"))
            documents.append((args.package.name, packaged, names))

    for label, manifest, files in documents:
        prefix = f"[{label}] "
        if SCHEMA_PATH.exists():
            try:
                from jsonschema import Draft202012Validator

                schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
                schema_errors = sorted(
                    Draft202012Validator(schema).iter_errors(manifest),
                    key=lambda error: list(error.absolute_path),
                )
                for error in schema_errors:
                    errors.append(f"{prefix}schema {list(error.absolute_path)}: {error.message}")
                checks += 1
            except ImportError:
                print("note: jsonschema not installed; schema check skipped", file=sys.stderr)

        check(0 < manifest.get("manifest_version", 0) <= SUPPORTED_MANIFEST_VERSION,
              f"{prefix}unsupported manifest_version")
        check(valid_identifier(manifest.get("id", "")), f"{prefix}invalid plugin id")
        check(bool(manifest.get("name", "").strip()), f"{prefix}empty name")
        check(bool(manifest.get("publisher", "").strip()), f"{prefix}empty publisher")
        check(bool(re.match(r"^\d+\.\d+\.\d+", manifest.get("version", ""))),
              f"{prefix}version is not semver")
        check(manifest.get("protocol_version") in (None, SUPPORTED_PROTOCOL_VERSION),
              f"{prefix}manifest v1 must not override protocol_version")
        check("executable" not in manifest and not manifest.get("drivers"),
              f"{prefix}manifest v1 cannot use legacy executable/drivers")
        engines = manifest.get("engines") or {}
        check(bool(engines.get("host_api", "").strip()), f"{prefix}engines.host_api is required")
        check(engines.get("dbx", "").strip().startswith((">=", "^", "~", "")),
              f"{prefix}engines.dbx must be a version requirement")

        for permission in manifest.get("permissions", []):
            valid = (
                permission in PERMISSIONS
                or permission.startswith("host.network:")
                or permission.startswith("host.openUrl:")
            )
            check(valid, f"{prefix}unsupported permission '{permission}'")
        check(len(manifest.get("permissions", [])) == len(set(manifest.get("permissions", []))),
              f"{prefix}duplicate permissions")

        for locale in manifest.get("localizations", {}):
            check(valid_locale(locale), f"{prefix}invalid localization locale '{locale}'")

        # The Sidecar reports its own constant; a stale one stays invisible until
        # initialize/plugin.version is compared (the 0.2.3 bump did exactly that).
        sidecar_main = ROOT / "backend" / "main.go"
        declaration = (
            re.search(r'pluginVersion\s*=\s*"([^"]+)"', sidecar_main.read_text(encoding="utf-8"))
            if sidecar_main.is_file() else None
        )
        check(
            declaration is not None and declaration.group(1) == manifest.get("version"),
            f"{prefix}backend/main.go pluginVersion must equal manifest version "
            f"'{manifest.get('version')}' (got {declaration.group(1) if declaration else 'missing'})",
        )

        contributions = manifest.get("contributions", [])
        ids = [c.get("id", "") for c in contributions]
        check(len(ids) == len(set(ids)), f"{prefix}duplicate contribution ids")
        workbenches = {c["id"] for c in contributions if c.get("type") == "workbench"}
        commands = {c["id"] for c in contributions if c.get("type") == "command"}

        def file_exists(path: str) -> bool:
            if files:
                return path in files
            return (ROOT / path).is_file()

        for icon in [manifest.get("icon")] + [c.get("icon") for c in contributions]:
            if icon:
                check(file_exists(icon), f"{prefix}declared icon missing: {icon}")

        backend = (manifest.get("entrypoints") or {}).get("backend")
        if backend:
            check(SUPPORTED_PROTOCOL_VERSION in backend.get("protocol_versions", [SUPPORTED_PROTOCOL_VERSION]),
                  f"{prefix}backend does not support protocol version {SUPPORTED_PROTOCOL_VERSION}")
            executable = backend.get("executable", "")
            if files:  # packaged manifest uses the rewritten platform path
                check(executable in files, f"{prefix}backend executable missing: {executable}")

        for contribution in contributions:
            cid, ctype = contribution.get("id", ""), contribution.get("type", "")
            check(valid_identifier(cid), f"{prefix}invalid contribution id '{cid}'")
            check(ctype in CONTRIBUTION_TYPES, f"{prefix}unknown contribution type '{ctype}'")
            if ctype in ("workbench", "context-menu", "command", "result-view", "filesystem-provider"):
                check(bool(contribution.get("label", "").strip()), f"{prefix}{ctype} '{cid}' needs a label")
            if ctype == "workbench":
                check(bool(files or (manifest.get("entrypoints") or {}).get("ui")),
                      f"{prefix}workbench '{cid}' requires a UI entrypoint")
            if ctype == "connection-provider":
                check(valid_identifier(contribution.get("database_type", "")),
                      f"{prefix}connection-provider '{cid}' invalid database_type")
                if contribution.get("workbench"):
                    check(contribution["workbench"] in workbenches,
                          f"{prefix}connection-provider '{cid}' references missing workbench")
            if ctype == "context-menu":
                check(contribution.get("menu") in CONTEXT_MENUS,
                      f"{prefix}context-menu '{cid}' unsupported menu '{contribution.get('menu')}'")
                action = contribution.get("action") or {}
                if action.get("type") == "open-workbench":
                    check(action.get("workbench") in workbenches,
                          f"{prefix}context-menu '{cid}' references missing workbench")
            if ctype == "command":
                action = contribution.get("action") or {}
                check(action.get("type") == "open-workbench", f"{prefix}command '{cid}' unsupported action")
                check(action.get("presentation", "tab") in PRESENTATIONS,
                      f"{prefix}command '{cid}' unsupported presentation")
                check(action.get("workbench") in workbenches,
                      f"{prefix}command '{cid}' references missing workbench")
            if ctype == "menus":
                for item in contribution.get("items", []):
                    check(item.get("location") in MENU_LOCATIONS,
                          f"{prefix}menus '{cid}' invalid location '{item.get('location')}'")
                    check(item.get("group") in MENU_GROUPS,
                          f"{prefix}menus '{cid}' group '{item.get('group')}' outside {sorted(MENU_GROUPS)}")
                    check(item.get("command") in commands,
                          f"{prefix}menus '{cid}' references missing command '{item.get('command')}'")
                    check(not (item.get("location") == "commandPalette" and "default_visible" in item),
                          f"{prefix}menus '{cid}' cannot set default_visible on commandPalette")

    if errors:
        for error in errors:
            print("ERROR", error)
        print(f"\nFAILED: {len(errors)} error(s) across {len(documents)} manifest(s)")
        return 1
    print(f"PASS: {checks + len(errors)} runtime checks + schema validation across {len(documents)} manifest(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
