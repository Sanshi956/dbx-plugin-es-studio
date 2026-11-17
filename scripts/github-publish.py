#!/usr/bin/env python3
"""Publish ES Studio to GitHub: create a Release with all platform store assets.

Requires a GitHub token in the GITHUB_TOKEN environment variable (or the gitignored
.dbx-dev/github-token.env). Classic token needs `repo` scope; fine-grained token
needs Contents + Metadata (read/write) plus repository creation permission (not
required with --release-only). The token is never written to disk or git config.

Usage:
    GITHUB_TOKEN=ghp_xxx python scripts/github-publish.py --repo-name dbx-plugin-es-studio
    GITHUB_TOKEN=ghp_xxx python scripts/github-publish.py --repo OWNER/REPO --release-only

What it does (idempotent where possible):
  1. Resolve token owner via GET /user, or take the full slug from --repo
  2. Create the public repository if it does not exist (skipped by --release-only)
  3. Generate store metadata with the real repo slug (scripts/prepare-store-submission.py)
  4. Commit .dbx-store.json + release-candidates.json, tag v<version> (skipped by --release-only)
  5. Push branch as main and push the tag (skipped by --release-only)
  6. Create the GitHub Release and upload every platform .dbxp + .artifact.json
     listed in release-candidates.json, plus release-candidates.json itself
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
API = "https://api.github.com"
_TOKEN = ""


def safe(text: str) -> str:
    """Never echo the PAT into logs or error messages."""
    return text.replace(_TOKEN, "***") if _TOKEN else text


def api(method: str, path: str, token: str, payload=None, host: str = API, allow_404: bool = False):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        host + path,
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "es-studio-publish",
            **({"Content-Type": "application/json"} if payload is not None else {}),
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        if exc.code == 404 and allow_404:
            return {}
        body = exc.read().decode("utf-8", errors="replace")
        raise SystemExit(f"GitHub API {method} {path} -> HTTP {exc.code}: {body[:500]}") from exc


def run(cmd: list[str], *, check: bool = True) -> subprocess.CompletedProcess:
    print("+", safe(" ".join(cmd)))
    proc = subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True)
    if check and proc.returncode:
        raise SystemExit(f"command failed: {safe(str(cmd))}\n{safe(proc.stdout)}\n{safe(proc.stderr)}")
    if proc.stdout.strip():
        print(safe(proc.stdout.strip()[-2000:]))
    return proc


def ensure_repository(token: str, slug: str, repo_name: str) -> None:
    """Create the public repository if it does not exist yet (idempotent)."""
    existing = api("GET", f"/repos/{slug}", token, allow_404=True)
    if existing.get("full_name"):
        print("repository already exists:", existing["html_url"])
        return
    created = api(
        "POST",
        "/user/repos",
        token,
        {
            "name": repo_name,
            "description": "ES Studio - professional Elasticsearch workbench plugin for DBX",
            "homepage": "https://dbxio.com",
            "private": False,
            "has_issues": True,
            "has_wiki": False,
            "auto_init": False,
        },
    )
    print("created:", created["html_url"])


def main() -> None:
    global _TOKEN
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-name", default="dbx-plugin-es-studio")
    ap.add_argument("--owner", default=None, help="defaults to the token owner")
    ap.add_argument("--repo", default=None, help="full OWNER/REPO slug; overrides --owner/--repo-name")
    ap.add_argument("--tag", default=None, help="release tag (default: v<manifest version>)")
    ap.add_argument("--license", default="MIT")
    ap.add_argument(
        "--release-only",
        action="store_true",
        help="only create the Release and upload assets from dist/ (used by CI on a tag)",
    )
    args = ap.parse_args()

    token = os.environ.get("GITHUB_TOKEN", "").strip()
    if not token:
        # Prefer env var; fall back to a gitignored local file so the token never
        # has to appear in a chat transcript or shell history.
        token_file = ROOT / ".dbx-dev" / "github-token.env"
        if token_file.exists():
            for line in token_file.read_text(encoding="utf-8").splitlines():
                line = line.strip()
                if line and not line.startswith("#"):
                    token = line.split("=", 1)[-1].strip().strip('"').strip("'")
                    break
    if not token:
        raise SystemExit(
            "No GitHub token. Either export GITHUB_TOKEN, or put the PAT in "
            ".dbx-dev/github-token.env (gitignored, one line)."
        )
    _TOKEN = token

    if args.repo:
        slug = args.repo.strip().strip("/")
        owner, separator, repo_name = slug.partition("/")
        if not separator or not owner or not repo_name:
            raise SystemExit(f"invalid --repo: {args.repo!r} (expected OWNER/REPO)")
    else:
        me = api("GET", "/user", token)
        owner = args.owner or me["login"]
        slug = f"{owner}/{args.repo_name}"
    print(f"GitHub owner: {owner}, repository: {slug}")

    if not args.release_only:
        ensure_repository(token, slug, args.repo_name)

    manifest = json.loads((ROOT / "manifest.json").read_text(encoding="utf-8"))
    version = manifest["version"]
    tag = args.tag or f"v{version}"
    plugin_id = manifest["id"]

    if args.release_only:
        # CI already prepared the metadata from the built artifacts; never
        # create tags or push from a detached HEAD in a workflow run.
        assert (ROOT / "release-candidates.json").exists(), "release-candidates.json is missing"
        print("release-only: using existing release-candidates.json")
    else:
        # Metadata must reference the final repository before the tag is cut.
        py = sys.executable
        run([py, "scripts/prepare-store-submission.py", "--repo", slug, "--tag", tag, "--license", args.license])
        run(["git", "add", ".dbx-store.json", "release-candidates.json"])
        has_staged = run(["git", "diff", "--cached", "--quiet"], check=False).returncode != 0
        if has_staged:
            run(["git", "commit", "-m", f"chore(store): submission metadata for {slug} @ {tag}"])
        else:
            print("store metadata already committed")

        if run(["git", "rev-parse", "-q", "--verify", f"refs/tags/{tag}"], check=False).returncode:
            run(["git", "tag", "-a", tag, "-m", f"ES Studio {version}"])
        else:
            print("tag already exists:", tag)

        remote = f"https://x-access-token:{token}@github.com/{slug}.git"
        run(["git", "push", remote, "HEAD:refs/heads/main"])
        run(["git", "push", remote, tag])

    # Enumerate every platform artifact from release-candidates.json, verify each
    # package against its pinned digest, then upload .dbxp + .artifact.json pairs.
    candidates = json.loads((ROOT / "release-candidates.json").read_text(encoding="utf-8"))
    artifacts = candidates["artifacts"]
    assert isinstance(artifacts, list) and artifacts, "release-candidates.json has no artifacts"
    targets = [item["target"] for item in artifacts]
    print(f"Release targets ({len(targets)}): {', '.join(targets)}")

    notes = (
        f"ES Studio {version} — unsigned review candidates for DBX Store.\n\n"
        "Highlights: standalone Shard Settings page; MCP contribution in DBX settings; "
        "localized allocation explain; fixed MCP table-header class collision and duplicate "
        "each-key crash. See SUBMISSION.md in the marketplace bundle for the full checklist.\n\n"
        f"Platforms ({len(targets)}): {', '.join(targets)}. Each target ships an unsigned "
        ".dbxp + .artifact.json. DBX Store syncs release-candidates.json, then signs "
        "centrally after review. Built natively per target by .github/workflows/release.yml."
    )
    releases = api("GET", f"/repos/{slug}/releases/tags/{tag}", token, allow_404=True)
    if releases.get("id"):
        release = releases
        print("release already exists:", release["html_url"])
    else:
        release = api(
            "POST",
            f"/repos/{slug}/releases",
            token,
            {
                "tag_name": tag,
                "name": f"ES Studio {version}",
                "body": notes,
                "draft": False,
                "prerelease": False,
            },
        )
        print("created release:", release["html_url"])

    upload_url = release["upload_url"].split("{", 1)[0]
    assets: list[Path] = []
    seen_targets: set[str] = set()
    for artifact in artifacts:
        target = artifact["target"]
        assert target not in seen_targets, f"duplicate target in release-candidates.json: {target}"
        seen_targets.add(target)
        package = ROOT / "dist" / artifact["url"]
        assert package.exists(), f"missing asset: {package}"
        digest = hashlib.sha256(package.read_bytes()).hexdigest()
        assert digest == artifact["sha256"], f"sha256 mismatch for {target}: {digest}"
        assert package.stat().st_size == artifact["size"], f"size mismatch for {target}"
        assets.append(package)
        metadata = package.with_name(package.name.replace(".dbxp", ".artifact.json"))
        assert metadata.exists(), f"missing asset: {metadata}"
        assets.append(metadata)
    assets.append(ROOT / "release-candidates.json")

    for asset in assets:
        req = urllib.request.Request(
            f"{upload_url}?name={asset.name}",
            data=asset.read_bytes(),
            method="POST",
            headers={
                "Authorization": f"Bearer {token}",
                "Accept": "application/vnd.github+json",
                "Content-Type": "application/octet-stream",
                "User-Agent": "es-studio-publish",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                print("uploaded", asset.name, "HTTP", resp.status)
        except urllib.error.HTTPError as exc:
            if exc.code == 422:
                print("asset already present:", asset.name)
            else:
                raise SystemExit(f"upload failed {asset.name}: HTTP {exc.code} {exc.read()[:300]}") from exc

    print("\nDONE. Next: open a PR against t8y2/dbx-store:main with")
    print(f"  dist/marketplace-{version}/store/candidates/{plugin_id}.json")
    print(f"Full checklist: dist/marketplace-{version}/store/REPO-AND-PR.md")


if __name__ == "__main__":
    main()
