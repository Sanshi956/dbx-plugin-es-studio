#!/usr/bin/env python3
"""Post-signing regression on the installable .dbxp (PLAN T17).

Checks the archive itself, not the source tree: checksums, signature,
absence of private key material, byte-identical UI build, backend binary
path declared in the packaged manifest, and an independent Node crypto
Ed25519 cross-check. It also extracts the packaged sidecar so the protocol
smoke test can be re-run against exactly what DBX will install.
"""
import base64
import hashlib
import json
import os
import re
import subprocess
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DIST = ROOT / "dist"
EXTRACT = ROOT / ".dbx-dev" / "verify"
CHECKSUMS = "checksums.json"
SIGNATURE = "signature.json"
PRIVATE_MARKERS = (".env", ".pem", ".key", "signing-key", "id_ed25519", "private")

checks = []


def check(name, ok, detail=""):
    checks.append((name, bool(ok), detail))
    print(f"  {'PASS' if ok else 'FAIL'}  {name}" + (f" :: {detail}" if not ok and detail else ""))


def default_trust_store() -> Path:
    """Resolve DBX's trust store on any OS; DBX_TRUST_STORE overrides it."""
    override = os.environ.get("DBX_TRUST_STORE")
    if override:
        return Path(override)
    if sys.platform == "win32":
        base = Path(os.environ.get("APPDATA") or (Path.home() / "AppData" / "Roaming"))
    elif sys.platform == "darwin":
        base = Path.home() / "Library" / "Application Support"
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME") or (Path.home() / ".config"))
    return base / "com.dbx.app" / "plugins" / ".trust" / "keys.json"


def main():
    candidates = sorted(DIST.glob("*.signed.dbxp"))
    if not candidates:
        raise SystemExit("No signed package in dist/; run scripts/sign-package.py first")
    package = candidates[-1]
    print(f"Verifying {package.name} ({package.stat().st_size} bytes)")

    with zipfile.ZipFile(package) as archive:
        entries = {info.filename: archive.read(info.filename) for info in archive.infolist() if not info.is_dir()}

    check("signature.json present", SIGNATURE in entries)
    check("checksums.json present", CHECKSUMS in entries)
    checksums_raw = entries[CHECKSUMS]
    document = json.loads(checksums_raw)
    check("checksum algorithm is sha256", document.get("algorithm") == "sha256")
    declared = document.get("files") or {}

    mismatches = []
    for name, digest in declared.items():
        if name not in entries:
            mismatches.append(f"{name}: missing")
        elif hashlib.sha256(entries[name]).hexdigest() != digest:
            mismatches.append(f"{name}: digest differs")
    check(f"all {len(declared)} checksums match", not mismatches, "; ".join(mismatches))
    check("signature.json is not covered by checksums", SIGNATURE not in declared)
    check("checksums.json is not self-referential", CHECKSUMS not in declared)

    leaked = [name for name in entries if any(marker in name.lower() for marker in PRIVATE_MARKERS)]
    check("no private key material in package", not leaked, ", ".join(leaked))

    packaged_manifest = json.loads(entries["manifest.json"])
    executable = packaged_manifest.get("entrypoints", {}).get("backend", {}).get("executable", "")
    check("manifest declares a backend executable", bool(executable), executable)
    exe_name = executable if executable.endswith(".exe") else executable + ".exe"
    candidates_for_exe = [name for name in entries if name.endswith(".exe")]
    check("backend binary exists in package", exe_name in entries, f"declared={exe_name} present={candidates_for_exe}")
    source_manifest = json.loads((ROOT / "manifest.json").read_text(encoding="utf-8"))
    check("packaged manifest id/version matches source",
          packaged_manifest.get("id") == source_manifest.get("id")
          and packaged_manifest.get("version") == source_manifest.get("version"),
          f"package={packaged_manifest.get('id')} {packaged_manifest.get('version')} "
          f"source={source_manifest.get('id')} {source_manifest.get('version')}")

    local_ui = ROOT / "ui"
    ui_files = [name for name in entries if name.startswith("ui/")]
    identical = []
    different = []
    for name in ui_files:
        local = local_ui / name[len("ui/"):]
        if not local.exists():
            different.append(f"{name}: missing locally")
        elif local.read_bytes() == entries[name]:
            identical.append(name)
        else:
            different.append(f"{name}: bytes differ")
    check(f"all {len(ui_files)} UI assets byte-identical to build output", not different, "; ".join(different))
    ui_html = entries.get("ui/index.html", b"")
    check("UI entry is a self-contained single file", bool(ui_html)
          and not re.search(rb"<script\b[^>]*\ssrc=|<link\b[^>]*\shref=", ui_html, re.I)
          and b"<script>\n" in ui_html and b"<style>\n" in ui_html)
    check("no external UI assets in package", ui_files == ["ui/index.html"], ", ".join(ui_files))
    # A plain inline <script> in <head> executes before #app is parsed and the
    # workbench never mounts in DBX's iframe. Enforce end-of-body placement.
    body_at = re.search(rb"<body\b", ui_html, re.I)
    app_at = ui_html.find(b'id="app"')
    script_at = ui_html.find(b"<script>\n")
    body_end_at = re.search(rb"</body>", ui_html, re.I)
    check("inline script runs after #app exists (end of <body>)",
          bool(body_at) and bool(body_end_at)
          and body_at.start() < app_at < script_at < body_end_at.start())

    signature_doc = json.loads(entries[SIGNATURE])
    trust_store = default_trust_store()
    public_b64 = None
    if trust_store.exists():
        public_b64 = (json.loads(trust_store.read_text(encoding="utf-8")).get("keys") or {}).get(signature_doc.get("key_id"))
    check("trusted key registered for signature key_id", bool(public_b64), str(signature_doc.get("key_id")))

    # Independent cross-check with Node's crypto (not the Python signing code).
    payload = {
        "publicKey": public_b64 or "",
        "signature": signature_doc.get("signature", ""),
        "message": base64.b64encode(checksums_raw).decode(),
    }
    node_script = """
const crypto = require('crypto');
const input = JSON.parse(require('fs').readFileSync(0, 'utf8'));
const raw = Buffer.from(input.publicKey, 'base64');
const der = Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), raw]);
const key = crypto.createPublicKey({ key: der, format: 'der', type: 'spki' });
const ok = crypto.verify(null, Buffer.from(input.message, 'base64'), key, Buffer.from(input.signature, 'base64'));
console.log(ok ? 'VALID' : 'INVALID');
process.exit(ok ? 0 : 1);
"""
    result = subprocess.run(
        ["node", "-e", node_script],
        input=json.dumps(payload), text=True, capture_output=True,
    )
    check("Node crypto Ed25519 cross-verification", result.returncode == 0 and "VALID" in result.stdout,
          (result.stdout + result.stderr).strip()[:300])

    # Extract the exact binary DBX will install for the protocol re-run.
    EXTRACT.mkdir(parents=True, exist_ok=True)
    exe_target = EXTRACT / "es-studio-sidecar.exe"
    if exe_name in entries:
        exe_target.write_bytes(entries[exe_name])
        check("packaged sidecar extracted", exe_target.stat().st_size == len(entries[exe_name]))
        print(f"\nEXTRACTED_SIDECAR={exe_target}")

    failed = [name for name, ok, _ in checks if not ok]
    print(f"\n{len(checks) - len(failed)} passed, {len(failed)} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
