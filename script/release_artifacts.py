#!/usr/bin/env python3
"""Keep two accepted release archives; recoverably remove project build debris."""
import argparse
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile
import uuid
import zipfile

ROOT = Path(__file__).resolve().parent.parent


def archive_metadata(path):
    path = Path(path)
    with zipfile.ZipFile(path) as archive:
        if archive.testzip():
            raise ValueError(f"Corrupt archive: {path}")
        for name in archive.namelist():
            member = Path(name)
            if member.is_absolute() or ".." in member.parts:
                raise ValueError(f"Unsafe archive member: {name}")
        info = plistlib.loads(archive.read("GoConnect.app/Contents/Info.plist"))
    if info.get("CFBundleIdentifier") != "com.willhsu.GoConnect":
        raise ValueError(f"Unexpected bundle: {path}")
    return {
        "archive": path.name,
        "version": info["CFBundleShortVersionString"],
        "build": info["CFBundleVersion"],
        "bytes": path.stat().st_size,
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
    }


def check(root=ROOT):
    dist = root / "dist"
    if dist.is_symlink():
        raise ValueError("dist must not be a symlink")
    manifest = json.loads((dist / "releases.json").read_text())
    if manifest.get("format") != 1 or set(manifest) != {"format", "current", "rollback"}:
        raise ValueError("Unexpected release manifest")
    if {p.name for p in dist.iterdir()} != {"current", "rollback", "releases.json"}:
        raise ValueError("dist contains files outside the two-release boundary")
    for role in ("current", "rollback"):
        directory = dist / role
        metadata = manifest[role]
        name = metadata["archive"]
        if Path(name).name != name or not name.endswith(".zip") or directory.is_symlink():
            raise ValueError(f"Invalid {role} archive path")
        if {p.name for p in directory.iterdir()} != {name, "SHA256SUMS"}:
            raise ValueError(f"Unexpected files in {role}")
        if any(p.is_symlink() for p in directory.iterdir()):
            raise ValueError(f"Symlink in {role}")
        if archive_metadata(directory / name) != metadata:
            raise ValueError(f"{role} metadata or checksum mismatch")
        expected = f"{metadata['sha256']}  {name}\n"
        if (directory / "SHA256SUMS").read_text() != expected:
            raise ValueError(f"{role} SHA256SUMS mismatch")
    if manifest["current"]["sha256"] == manifest["rollback"]["sha256"]:
        raise ValueError("Current and rollback must be distinct releases")
    if int(manifest["rollback"]["build"]) >= int(manifest["current"]["build"]):
        raise ValueError("Rollback must be an earlier build")
    return manifest


def ensure_unused(paths):
    processes = subprocess.check_output(["/bin/ps", "-axo", "pid=,comm="], text=True)
    for line in processes.splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) == 2:
            for path in paths:
                if fields[1] == str(path) or fields[1].startswith(str(path) + "/"):
                    raise ValueError(f"Running process {fields[0]} uses {path}; cleanup refused")


def ensure_untracked(paths, root=ROOT):
    names = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).split(b"\0")
    tracked = [root / os.fsdecode(name) for name in names if name]
    for path in paths:
        if any(path == item or path in item.parents for item in tracked):
            raise ValueError(f"Refusing to move version-controlled files: {path}")


def trash_directory():
    directory = Path.home() / ".Trash" / (
        "GoConnect-project-cleanup-" + datetime.now().strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:8]
    )
    directory.mkdir(mode=0o700, parents=True)
    return directory


def unregister_apps(paths):
    tool = Path("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister")
    count = 0
    if tool.exists():
        for path in paths:
            if path.is_symlink() or not path.is_dir():
                continue
            for directory, children, _ in os.walk(path, followlinks=False):
                for name in list(children):
                    app = Path(directory) / name
                    if name.endswith(".app"):
                        children.remove(name)
                        if not app.is_symlink():
                            subprocess.run([str(tool), "-u", str(app)], check=False, capture_output=True)
                            count += 1
    return count


def retain(current, rollback, root=ROOT):
    """Record explicitly selected, already accepted packages; do not install them."""
    dist = root / "dist"
    ensure_unused([dist])
    ensure_untracked([dist], root)
    if dist.is_symlink():
        raise ValueError("dist must not be a symlink")
    metadata = {role: archive_metadata(source) for role, source in (("current", current), ("rollback", rollback))}
    if metadata["current"]["sha256"] == metadata["rollback"]["sha256"]:
        raise ValueError("Current and rollback must be distinct releases")
    if int(metadata["rollback"]["build"]) >= int(metadata["current"]["build"]):
        raise ValueError("Rollback must be an earlier build")
    # Build and verify the complete pair before moving the previous directory.
    with tempfile.TemporaryDirectory(prefix=".release-stage-", dir=root) as temporary:
        stage_root = Path(temporary)
        for role, source in (("current", current), ("rollback", rollback)):
            directory = stage_root / "dist" / role
            directory.mkdir(parents=True)
            shutil.copy2(source, directory / metadata[role]["archive"])
            (directory / "SHA256SUMS").write_text(
                f"{metadata[role]['sha256']}  {metadata[role]['archive']}\n"
            )
        (stage_root / "dist/releases.json").write_text(
            json.dumps({"format": 1, **metadata}, ensure_ascii=False, indent=2) + "\n"
        )
        check(stage_root)
        ensure_unused([dist])
        old = None
        if dist.exists():
            old = trash_directory() / "dist"
            unregister_apps([dist])
            dist.rename(old)
        try:
            (stage_root / "dist").rename(dist)
            check(root)
        except BaseException:
            if dist.exists():
                shutil.rmtree(dist)
            if old:
                old.rename(dist)
            raise
    return check(root)


def cleanup_targets(root=ROOT):
    paths = [p for p in (root / ".build", root / ".swiftpm", root / "Transport/bin") if p.exists() or p.is_symlink()]
    for directory, children, files in os.walk(root, followlinks=False):
        parent = Path(directory)
        # Preserved releases and Git are never traversed, even with nested .git trees.
        children[:] = [name for name in children if name != ".git" and parent / name != root / "dist"]
        for name in list(children):
            path = parent / name
            if any(path == target or target in path.parents for target in paths):
                children.remove(name)
            elif name == "__pycache__":
                paths.append(path)
                children.remove(name)
        for name in files:
            if name == ".DS_Store" or name.lower().endswith((".zip", ".pyc", ".pyo")):
                paths.append(parent / name)
    return sorted(set(paths))


def clean(apply=False, root=ROOT):
    check(root)  # Never discard the old build trees without a verified retained pair.
    paths = cleanup_targets(root)
    # Filter tracked standalone files; refuse any generated tree containing tracked files.
    tracked = {root / os.fsdecode(n) for n in subprocess.check_output(["git", "ls-files", "-z"], cwd=root).split(b"\0") if n}
    paths = [p for p in paths if p not in tracked]
    ensure_untracked(paths, root)
    ensure_unused(paths)
    result = {"apply": apply, "targets": [str(p.relative_to(root)) for p in paths]}
    if apply and paths:
        destination = trash_directory()
        result["unregistered_app_paths"] = unregister_apps(paths)
        for path in paths:
            target = destination / path.relative_to(root)
            target.parent.mkdir(parents=True, exist_ok=True)
            # Move the link itself, never its target.
            path.rename(target)
        result["trash"] = str(destination)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("check", help="Validate the retained release pair")
    cleanup = commands.add_parser("clean", help="List generated debris; add --apply to move it to Trash")
    cleanup.add_argument("--apply", action="store_true")
    retention = commands.add_parser("retain", help="Retain two explicitly selected, already accepted ZIPs")
    retention.add_argument("--current", type=Path, required=True)
    retention.add_argument("--rollback", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "check":
            result = check()
        elif args.command == "clean":
            result = clean(args.apply)
        else:
            result = retain(args.current.resolve(), args.rollback.resolve())
        print(json.dumps(result, ensure_ascii=False, indent=2))
    except (OSError, ValueError, KeyError, zipfile.BadZipFile) as error:
        parser.exit(1, f"{error}\n")


if __name__ == "__main__":
    main()
