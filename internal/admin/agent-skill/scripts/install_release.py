#!/usr/bin/env python3
"""Install a checksum-verified MCP Manager GitHub release."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import shutil
import stat
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

DEFAULT_REPO = "online111111/mcp-manager"
USER_AGENT = "mcp-manager-deployer/1"


def normalize_os(value: str) -> str:
    v = value.lower()
    if v.startswith("linux"):
        return "linux"
    if v.startswith("darwin") or v.startswith("mac"):
        return "darwin"
    if v.startswith("win") or v in {"msys", "cygwin"}:
        return "windows"
    raise ValueError(f"unsupported operating system: {value}")


def normalize_arch(value: str) -> str:
    v = value.lower()
    if v in {"x86_64", "amd64", "x64"}:
        return "amd64"
    if v in {"aarch64", "arm64"}:
        return "arm64"
    raise ValueError(f"unsupported architecture: {value}")


def api_json(url: str) -> dict:
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT, "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)


def download(url: str, dest: Path) -> None:
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(req, timeout=60) as resp, dest.open("wb") as out:
        shutil.copyfileobj(resp, out)


def release_metadata(repo: str, version: str | None) -> tuple[str, dict]:
    base = f"https://api.github.com/repos/{repo}/releases"
    if version:
        tag = version if version.startswith("v") else f"v{version}"
        return repo, api_json(f"{base}/tags/{tag}")
    return repo, api_json(f"{base}/latest")

def parse_checksum(text: str, asset_name: str) -> str:
    for raw in text.splitlines():
        parts = raw.strip().split()
        if len(parts) >= 2 and parts[-1].lstrip("*") == asset_name:
            digest = parts[0].lower()
            if len(digest) == 64 and all(c in "0123456789abcdef" for c in digest):
                return digest
            raise ValueError(f"invalid SHA256 entry for {asset_name}")
    raise ValueError(f"{asset_name} not found in SHA256SUMS")


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def safe_extract_binary(archive: Path, workdir: Path, goos: str) -> Path:
    binary_name = "mcp-manager.exe" if goos == "windows" else "mcp-manager"
    wanted = {binary_name, "README.md"}
    workdir.mkdir(parents=True, exist_ok=True)

    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as zf:
            names = set(zf.namelist())
            if binary_name not in names:
                raise ValueError(f"archive does not contain {binary_name}")
            for member in names:
                p = Path(member)
                if p.is_absolute() or ".." in p.parts:
                    raise ValueError("unsafe path in release archive")
            for member in names & wanted:
                zf.extract(member, workdir)
    else:
        with tarfile.open(archive, "r:gz") as tf:
            members = tf.getmembers()
            names = {m.name for m in members if m.isfile()}
            if binary_name not in names:
                raise ValueError(f"archive does not contain {binary_name}")
            for member in members:
                p = Path(member.name)
                if p.is_absolute() or ".." in p.parts:
                    raise ValueError("unsafe path in release archive")
            selected = [m for m in members if m.isfile() and m.name in wanted]
            tf.extractall(workdir, members=selected, filter="data")

    binary = workdir / binary_name
    if not binary.is_file():
        raise ValueError(f"failed to extract {binary_name}")
    if goos != "windows":
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    return binary


def default_install_dir(goos: str) -> Path:
    if goos == "windows":
        base = os.environ.get("LOCALAPPDATA")
        return Path(base) / "Programs" / "mcp-manager" if base else Path.home() / "mcp-manager-bin"
    if hasattr(os, "geteuid") and os.geteuid() == 0:
        return Path("/usr/local/bin")
    return Path.home() / ".local" / "bin"


def main() -> int:
    parser = argparse.ArgumentParser(description="Install a checksum-verified MCP Manager GitHub release.")
    parser.add_argument("--repo", default=DEFAULT_REPO, help="GitHub repository owner/name")
    parser.add_argument("--version", help="Release version, e.g. 0.4.0 or v0.4.0; default: latest stable")
    parser.add_argument("--install-dir", help="Destination directory for the binary")
    parser.add_argument("--force", action="store_true", help="Replace an existing destination binary")
    parser.add_argument("--dry-run", action="store_true", help="Resolve version/asset but do not install")
    args = parser.parse_args()

    try:
        goos = normalize_os(platform.system())
        goarch = normalize_arch(platform.machine())
        resolved_repo, meta = release_metadata(args.repo, args.version)
        tag = meta.get("tag_name") or ""
        if not tag.startswith("v"):
            raise ValueError(f"unexpected release tag: {tag!r}")
        version = tag[1:]
        ext = ".zip" if goos == "windows" else ".tar.gz"
        asset_name = f"mcp-manager-{version}-{goos}-{goarch}{ext}"
        assets = {a.get("name"): a.get("browser_download_url") for a in meta.get("assets", [])}
        asset_url = assets.get(asset_name)
        sums_url = assets.get("SHA256SUMS")
        if not asset_url or not sums_url:
            raise ValueError(f"release {tag} in {resolved_repo} is missing {asset_name} or SHA256SUMS")

        install_dir = Path(args.install_dir).expanduser() if args.install_dir else default_install_dir(goos)
        binary_name = "mcp-manager.exe" if goos == "windows" else "mcp-manager"
        dest = install_dir / binary_name
        print(f"repository={resolved_repo}")
        print(f"release={tag}")
        print(f"asset={asset_name}")
        print(f"destination={dest}")
        if args.dry_run:
            return 0

        with tempfile.TemporaryDirectory(prefix="mcp-manager-install-") as td:
            tmp = Path(td)
            archive = tmp / asset_name
            sums = tmp / "SHA256SUMS"
            download(sums_url, sums)
            download(asset_url, archive)
            expected = parse_checksum(sums.read_text(encoding="utf-8"), asset_name)
            actual = sha256(archive)
            if actual != expected:
                raise ValueError(f"SHA256 mismatch for {asset_name}")
            binary = safe_extract_binary(archive, tmp / "extract", goos)

            install_dir.mkdir(parents=True, exist_ok=True)
            if dest.exists() and not args.force:
                raise FileExistsError(f"destination exists: {dest}; back it up and use --force to replace")
            staged = dest.with_name(dest.name + ".new")
            shutil.copy2(binary, staged)
            if goos != "windows":
                staged.chmod(0o755)
            os.replace(staged, dest)

        print(f"installed={dest}")
        return 0
    except urllib.error.HTTPError as exc:
        print(f"error: GitHub returned HTTP {exc.code}: {exc.reason}", file=sys.stderr)
    except (OSError, ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
