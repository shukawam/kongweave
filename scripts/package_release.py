#!/usr/bin/env python3
import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64")
TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?")


def validate_tag(tag):
    match = TAG.fullmatch(tag)
    if not match:
        raise ValueError("tag must be vMAJOR.MINOR.PATCH with optional SemVer prerelease/build identifiers")
    prerelease = match[4]
    if prerelease and any(part.isdigit() and len(part) > 1 and part[0] == "0" for part in prerelease.split(".")):
        raise ValueError("numeric prerelease identifiers must not have leading zeros")
    return bool(prerelease)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT).decode().strip()


def payload_files():
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("release packaging requires a clean checkout")
    names = git("ls-files", "-z", "--", "LICENSE", "README.md", "README_ja.md", "docs", "examples").split("\0")
    payload = {}
    for name in sorted(filter(None, names)):
        path = Path(name)
        if name in ("docs/design.md", "docs/verification.md") or any(part.startswith(".") for part in path.parts):
            continue
        source = ROOT / path
        if source.is_symlink() or not source.is_file():
            raise ValueError(f"release payload must be a regular file: {name}")
        payload[name] = source.read_bytes()
    if "LICENSE" not in payload:
        raise ValueError("release payload is missing LICENSE")
    return payload


def write_archive(path, binary, payload):
    # Fixed metadata avoids machine paths, owner names, and wall-clock timestamps.
    with path.open("wb") as output:
        with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for name, data in [("kongweave", binary), *sorted(payload.items())]:
                    entry = tarfile.TarInfo(name)
                    entry.size = len(data)
                    entry.mode = 0o755 if name == "kongweave" else 0o644
                    archive.addfile(entry, io.BytesIO(data))


def verify_artifacts(directory):
    archives = {f"kongweave_{target}.tar.gz" for target in TARGETS}
    expected = archives | {"checksums.txt"}
    if {path.name for path in directory.iterdir()} != expected:
        raise ValueError("expected exactly four platform archives and checksums.txt")
    if any(not (directory / name).is_file() or (directory / name).is_symlink() for name in expected):
        raise ValueError("release assets must be regular files")
    checksums = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  (kongweave_[a-z0-9_]+\.tar\.gz)", line)
        if not match or match[2] in checksums:
            raise ValueError("invalid or duplicate checksum entry")
        checksums[match[2]] = match[1]
    if set(checksums) != archives:
        raise ValueError("checksums must cover every archive exactly once")
    for name, expected_hash in checksums.items():
        if hashlib.sha256((directory / name).read_bytes()).hexdigest() != expected_hash:
            raise ValueError(f"checksum mismatch: {name}")


def package(tag, output):
    validate_tag(tag)
    if git("rev-parse", "HEAD") != git("rev-parse", f"refs/tags/{tag}^{{commit}}"):
        raise ValueError("HEAD must match the release tag")
    if output.exists() or output.is_symlink():
        raise ValueError("output already exists; choose a new directory")
    payload = payload_files()
    output.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".release-", dir=output.parent))
    try:
        with tempfile.TemporaryDirectory(prefix="kongweave-binaries-") as work:
            for target in TARGETS:
                goos, goarch = target.split("_")
                binary = Path(work) / target / "kongweave"
                binary.parent.mkdir()
                env = dict(os.environ, CGO_ENABLED="0", GOOS=goos, GOARCH=goarch)
                subprocess.run(
                    ["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", f"-s -w -X main.version={tag}", "-o", str(binary), "./cmd/kongweave"],
                    cwd=ROOT, env=env, check=True,
                )
                write_archive(stage / f"kongweave_{target}.tar.gz", binary.read_bytes(), payload)
        checksums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in sorted(stage.glob("*.tar.gz")))
        (stage / "checksums.txt").write_text(checksums, encoding="utf-8")
        verify_artifacts(stage)
        if output.exists() or output.is_symlink():
            raise ValueError("output already exists; choose a new directory")
        stage.rename(output)
    finally:
        if stage.exists():
            shutil.rmtree(stage)


def main():
    parser = argparse.ArgumentParser(description="Package a clean, tagged checkout without publishing it.")
    parser.add_argument("--tag", required=True)
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "release")
    args = parser.parse_args()
    try:
        package(args.tag, args.output.absolute())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Error: {error}\n")


if __name__ == "__main__":
    main()
