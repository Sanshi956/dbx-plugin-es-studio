#!/usr/bin/env python3
"""Sign a DBX .dbxp candidate with a custom-repository Ed25519 key.

`dbx-plugin package .` always produces an UNSIGNED review candidate. DBX installs
local files with PluginInstallPolicy::LocalSigned unless the explicit
"allow unsigned development package" switch is enabled, so an unsigned candidate
is rejected with:

    Plugin package must have a trusted Ed25519 signature

This script implements DBX plugin signing v1 exactly as the upstream
`plugins/sdk/packager` does:

  * signature.json = {"algorithm":"ed25519","key_id":...,"signature":base64(sig)}
  * the signed payload is the RAW BYTES of checksums.json
  * signature.json is appended to the archive and is NOT covered by checksums.json
  * the public key is registered in <dbx plugins root>/.trust/keys.json

Official DBX Store authors must NOT use this script for Marketplace submissions:
the store signs approved candidates centrally. This is for local/private installs.

Usage:
    python scripts/sign-package.py
    python scripts/sign-package.py --verify
    python scripts/sign-package.py --trust-store "%APPDATA%/com.dbx.app/plugins/.trust/keys.json"
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import re
import sys
import zipfile
from pathlib import Path

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import (
        Ed25519PrivateKey,
        Ed25519PublicKey,
    )
except ImportError:  # pragma: no cover
    sys.exit("cryptography is required: pip install cryptography")

ROOT = Path(__file__).resolve().parent.parent
CHECKSUMS_FILE = "checksums.json"
SIGNATURE_FILE = "signature.json"
DEFAULT_KEY_ID = "local.es.head.release"
KEY_ENV_NAMES = (
    "DBX_PLUGIN_SIGNING_KEY",
    "DBX_PLUGIN_SIGNING_KEY_ID",
    "DBX_PLUGIN_SIGNING_PUBLIC_KEY",
)


def log(message: str) -> None:
    print(message, flush=True)


def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def validate_key_id(key_id: str) -> None:
    # Mirrors crates/dbx-plugin-runtime/src/plugins/installer.rs::validate_key_id
    if not key_id or len(key_id) > 128 or not re.fullmatch(r"[A-Za-z0-9._:-]+", key_id):
        raise ValueError(f"Invalid signing key id: {key_id!r}")


def read_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        match = re.match(r"^(?:export\s+)?([A-Z0-9_]+)=(.*)$", line.strip())
        if match and match.group(1) in KEY_ENV_NAMES:
            values[match.group(1)] = match.group(2).strip().strip('"').strip("'")
    return values


def load_or_create_key(key_file: Path, key_id: str) -> tuple[Ed25519PrivateKey, str, str]:
    """Return (private key, base64 seed, base64 public key)."""
    if key_file.exists():
        env = read_env_file(key_file)
        missing = [name for name in KEY_ENV_NAMES if not env.get(name)]
        if missing:
            raise ValueError(f"{key_file} is missing: {', '.join(missing)}")
        if env["DBX_PLUGIN_SIGNING_KEY_ID"] != key_id:
            raise ValueError(
                f"Key id mismatch: env has {env['DBX_PLUGIN_SIGNING_KEY_ID']!r}, requested {key_id!r}. "
                "Use the same key id or delete the key file to generate a new key."
            )
        seed = base64.b64decode(env["DBX_PLUGIN_SIGNING_KEY"], validate=True)
        private = Ed25519PrivateKey.from_private_bytes(seed)
        derived = base64.b64encode(public_bytes(private)).decode()
        if derived != env["DBX_PLUGIN_SIGNING_PUBLIC_KEY"]:
            raise ValueError("Public key in env file does not match the private seed")
        log(f"Loaded repository key from {key_file}")
        return private, env["DBX_PLUGIN_SIGNING_KEY"], derived

    private = Ed25519PrivateKey.generate()
    seed = private.private_bytes_raw()
    seed_b64 = base64.b64encode(seed).decode()
    pub_b64 = base64.b64encode(public_bytes(private)).decode()
    key_file.parent.mkdir(parents=True, exist_ok=True)
    key_file.write_text(
        "# DBX custom repository signing key - PRIVATE, never commit or ship.\n"
        f"export DBX_PLUGIN_SIGNING_KEY={seed_b64}\n"
        f"export DBX_PLUGIN_SIGNING_KEY_ID={key_id}\n"
        f"export DBX_PLUGIN_SIGNING_PUBLIC_KEY={pub_b64}\n",
        encoding="utf-8",
    )
    try:
        os.chmod(key_file, 0o600)
    except OSError:
        pass  # Windows ACLs are managed differently
    log(f"Generated NEW repository key at {key_file}")
    return private, seed_b64, pub_b64


def public_bytes(private: Ed25519PrivateKey) -> bytes:
    from cryptography.hazmat.primitives import serialization

    return private.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )


def read_package(path: Path) -> tuple[bytes, dict[str, bytes], bytes | None]:
    """Return (checksums.json raw bytes, all entries, signature.json raw bytes)."""
    if not path.is_file():
        raise FileNotFoundError(f"Package not found: {path}")
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if CHECKSUMS_FILE not in names:
            raise ValueError(f"{path.name} has no {CHECKSUMS_FILE}")
        if any(name != CHECKSUMS_FILE and name.lower().startswith("../") for name in names):
            raise ValueError("Unsafe path in package")
        entries = {
            info.filename: archive.read(info.filename)
            for info in archive.infolist()
            if not info.is_dir()
        }
        signature = entries.get(SIGNATURE_FILE)
        return entries[CHECKSUMS_FILE], entries, signature


def verify_checksums(checksums_raw: bytes, entries: dict[str, bytes]) -> None:
    """Replicate DBX verify_package_checksums()."""
    document = json.loads(checksums_raw)
    if document.get("algorithm") != "sha256":
        raise ValueError("Unsupported checksum algorithm")
    declared = document.get("files")
    if not isinstance(declared, dict):
        raise ValueError("checksums.json files must be an object")
    expected = {name for name in entries if name not in (CHECKSUMS_FILE, SIGNATURE_FILE)}
    if set(declared) != expected:
        raise ValueError(
            f"Checksums do not cover the package exactly; missing={sorted(expected - set(declared))}, "
            f"extra={sorted(set(declared) - expected)}"
        )
    for name, digest in declared.items():
        actual = sha256_hex(entries[name])
        if not isinstance(digest, str) or digest.lower() != actual:
            raise ValueError(f"Checksum mismatch for '{name}'")


def verify_signature(signature_raw: bytes, checksums_raw: bytes, key_id: str, public_b64: str) -> None:
    document = json.loads(signature_raw)
    if document.get("algorithm") != "ed25519":
        raise ValueError(f"Unsupported signature algorithm: {document.get('algorithm')}")
    if document.get("key_id") != key_id:
        raise ValueError(f"Unexpected key_id: {document.get('key_id')}")
    raw_key = base64.b64decode(public_b64, validate=True)
    if len(raw_key) != 32:
        raise ValueError("Public key must be 32 bytes")
    signature = base64.b64decode(document["signature"], validate=True)
    Ed25519PublicKey.from_public_bytes(raw_key).verify(signature, checksums_raw)
    #^ raises InvalidSignature on mismatch


def write_signed_package(source: Path, target: Path, checksums_raw: bytes, signature_raw: bytes) -> None:
    if source.resolve() == target.resolve():
        raise ValueError("Signed output must differ from the unsigned candidate")
    with zipfile.ZipFile(source) as zin, zipfile.ZipFile(target, "w") as zout:
        for info in zin.infolist():
            if info.is_dir():
                continue
            zout.writestr(info, zin.read(info.filename))
        zout.writestr(SIGNATURE_FILE, signature_raw, compress_type=zipfile.ZIP_DEFLATED)
    # Confirm the appended payload is byte-identical to what was signed.
    with zipfile.ZipFile(target) as check:
        if check.read(CHECKSUMS_FILE) != checksums_raw:
            raise ValueError("checksums.json changed while rewriting the package")
        if check.read(SIGNATURE_FILE) != signature_raw:
            raise ValueError("signature.json changed while rewriting the package")


def write_trust_store(path: Path, key_id: str, public_b64: str) -> None:
    """Merge a key into DBX's <plugins root>/.trust/keys.json."""
    document: dict = {"keys": {}}
    if path.exists():
        document = json.loads(path.read_text(encoding="utf-8")) or {}
    keys = document.setdefault("keys", {})
    existing = keys.get(key_id)
    if existing and existing.strip() != public_b64:
        raise ValueError(
            f"Trusted key '{key_id}' already exists with a different public key; "
            "remove it in Plugin Center before rotating"
        )
    keys[key_id] = public_b64
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + ".tmp")
    temp.write_text(json.dumps(document, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    os.replace(temp, path)
    log(f"Registered trusted key in {path}")


def default_trust_store() -> Path | None:
    if sys.platform == "win32":
        base = os.environ.get("APPDATA")
        return Path(base) / "com.dbx.app" / "plugins" / ".trust" / "keys.json" if base else None
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support" / "com.dbx.app" / "plugins" / ".trust" / "keys.json"
    return Path.home() / ".config" / "com.dbx.app" / "plugins" / ".trust" / "keys.json"


def default_package() -> Path:
    candidates = sorted((ROOT / "dist").glob("*.dbxp"))
    candidates = [p for p in candidates if ".signed." not in p.name]
    if not candidates:
        raise FileNotFoundError("No unsigned .dbxp in dist/; run: dbx-plugin package .")
    return candidates[-1]


def main() -> int:
    parser = argparse.ArgumentParser(description="Sign a DBX plugin candidate with a custom repository key")
    parser.add_argument("--package", type=Path, default=None, help="Unsigned .dbxp candidate")
    parser.add_argument("--out", type=Path, default=None, help="Signed .dbxp output path")
    parser.add_argument("--key-id", default=None, help="Signing key id (verify infers it from signature.json)")
    parser.add_argument("--key-file", type=Path, default=ROOT / ".dbx-repository-signing-key.env")
    parser.add_argument("--artifact-url", default=None, help="URL recorded in artifact metadata")
    parser.add_argument("--trust-store", type=Path, default=None, help="DBX .trust/keys.json path")
    parser.add_argument("--no-trust-store", action="store_true", help="Do not write any trust store")
    parser.add_argument("--verify", action="store_true", help="Verify an already signed package and exit")
    args = parser.parse_args()

    explicit_key_id = args.key_id is not None
    if not explicit_key_id:
        args.key_id = DEFAULT_KEY_ID
    validate_key_id(args.key_id)
    package = (args.package or default_package()).resolve()
    output = (args.out or package.with_name(package.stem + ".signed.dbxp")).resolve()
    if args.verify:
        package = output if output.exists() else package

    checksums_raw, entries, signature_raw = read_package(package)
    verify_checksums(checksums_raw, entries)
    log(f"checksums.json OK: {len(entries) - 1 - (1 if signature_raw else 0)} files covered")

    if args.verify:
        if signature_raw is None:
            raise ValueError(f"{package.name} is unsigned")
        # A signed package carries its own key id; use it unless the caller
        # explicitly overrides it, so verification works on another machine
        # without knowing the repository key name beforehand.
        if not explicit_key_id:
            signed_key_id = json.loads(signature_raw).get("key_id")
            if isinstance(signed_key_id, str) and signed_key_id:
                validate_key_id(signed_key_id)
                args.key_id = signed_key_id
        trust_path = args.trust_store or default_trust_store()
        env = read_env_file(args.key_file) if args.key_file.exists() else {}
        public_b64 = (
            env.get("DBX_PLUGIN_SIGNING_PUBLIC_KEY")
            if env.get("DBX_PLUGIN_SIGNING_KEY_ID") == args.key_id
            else None
        )
        if not public_b64 and trust_path and trust_path.exists():
            public_b64 = (json.loads(trust_path.read_text(encoding="utf-8")).get("keys") or {}).get(args.key_id)
        if not public_b64:
            raise ValueError("No public key available; pass --trust-store or keep the key env file")
        verify_signature(signature_raw, checksums_raw, args.key_id, public_b64)
        log(f"SIGNATURE VALID: {package.name} (key_id={args.key_id})")
        return 0

    if signature_raw is not None:
        raise ValueError(f"{package.name} is already signed; start from an unsigned candidate")

    private, _seed_b64, public_b64 = load_or_create_key(args.key_file, args.key_id)
    signature_b64 = base64.b64encode(private.sign(checksums_raw)).decode()
    signature_raw = json.dumps(
        {"algorithm": "ed25519", "key_id": args.key_id, "signature": signature_b64},
        indent=2,
    ).encode("utf-8")

    write_signed_package(package, output, checksums_raw, signature_raw)
    verify_signature(signature_raw, checksums_raw, args.key_id, public_b64)
    log(f"Signed package: {output} ({output.stat().st_size} bytes)")

    artifact_path = output.with_suffix(".artifact.json")
    old_artifact = package.with_suffix(".artifact.json")
    metadata: dict = {}
    if old_artifact.exists():
        metadata = json.loads(old_artifact.read_text(encoding="utf-8"))
    signed_bytes = output.read_bytes()
    metadata.update(
        {
            "target": metadata.get("target", "windows-x64"),
            "url": args.artifact_url or output.name,
            "sha256": sha256_hex(signed_bytes),
            "size": len(signed_bytes),
            "signingKeyId": args.key_id,
        }
    )
    artifact_path.write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    log(f"Artifact metadata: {artifact_path}")

    if not args.no_trust_store:
        trust_path = args.trust_store or default_trust_store()
        if trust_path:
            write_trust_store(trust_path, args.key_id, public_b64)

    log("")
    log(f"Key ID     : {args.key_id}")
    log(f"Public key : {public_b64}")
    log("")
    log("Install " + output.name + " in DBX Plugin Center (strict policy accepts it).")
    log("Other machines: run this script there, or add the key above under")
    log("Plugin Center -> Custom repository trust before installing.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:  # noqa: BLE001
        print(f"ERROR: {error}", file=sys.stderr)
        sys.exit(1)
