#!/usr/bin/env python3
"""Collect the exact bundled components' sources outside the project."""
import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import time
from urllib.parse import urlsplit


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def dependencies(binary):
    values = []
    for line in subprocess.check_output(["go", "version", "-m", str(binary)], text=True).splitlines():
        fields = line.split()
        if fields and fields[0] == "dep":
            values.append(fields[1:])
        elif fields and fields[0] == "=>":
            values[-1] = fields[1:]
    return values


def collect_patched_mihomo(directory, output, resume=False):
    from build_mihomo import load_lock
    source = json.loads((directory / "source.json").read_text())
    lock, patch = load_lock(directory / "mihomo.lock.json")
    if (source.get("kind") != "goconnect-patched-go" or source["url"] != lock["url"]
            or source["version"] != lock["buildVersion"] or source["tags"] != lock["tags"]
            or source["patchSHA256"] != lock["patchSHA256"] or source["sha256"] != lock["sha256"]):
        raise ValueError("Mihomo source/build provenance mismatch")
    destination = output / "components" / directory.name
    shutil.copytree(directory, destination, dirs_exist_ok=resume)
    return {"component": directory.name, "url": source["url"], "sha256": source["sha256"],
            "patchSHA256": source["patchSHA256"], "file": str((destination / "upstream.tar.gz").relative_to(output))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--resume", action="store_true", help="Reuse verified downloads in an existing source directory")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve()
    if output == root or root in output.parents or (output.exists() and not args.resume):
        parser.error("Output must be a new directory outside the project")
    output.mkdir(parents=True, exist_ok=args.resume)
    third_party = args.app / "Contents/Resources/ThirdParty"
    manifest, downloads = [], []
    for directory in sorted(third_party.iterdir()):
        source_metadata = directory / "source.json"
        if source_metadata.is_file() and directory.name.startswith("mihomo-"):
            record = collect_patched_mihomo(directory, output, args.resume)
            manifest.append(record); downloads.append(record)
            continue
        if source_metadata.is_file() and directory.name.startswith("Sparkle-"):
            source = json.loads(source_metadata.read_text())
            if source["name"] != "Sparkle" or not source["url"].startswith("https://github.com/sparkle-project/Sparkle/archive/refs/tags/"):
                raise ValueError("Unexpected Sparkle source provenance")
            destination = output / "components" / directory.name
            destination.mkdir(parents=True, exist_ok=args.resume)
            for item in directory.iterdir():
                if item.is_file(): shutil.copy2(item, destination / item.name)
            record = {"component": directory.name, "url": source["url"], "sha256": source["sha256"],
                      "file": str((destination / Path(urlsplit(source["url"]).path).name).relative_to(output))}
            manifest.append(record); downloads.append(record)
            continue
        sbom = directory / "sbom.spdx.json"
        if not sbom.is_file():
            continue
        destination = output / "components" / directory.name
        destination.mkdir(parents=True, exist_ok=args.resume)
        for item in directory.iterdir():
            if item.is_file():
                shutil.copy2(item, destination / item.name)
        name, version = directory.name.rsplit("-", 1)
        formula = Path("/opt/homebrew/Cellar") / name / version / ".brew" / (name + ".rb")
        if not formula.is_file():
            raise ValueError("Missing exact build formula: " + directory.name)
        shutil.copy2(formula, destination / formula.name)
        for index, package in enumerate(json.loads(sbom.read_text())["packages"]):
            url = package.get("downloadLocation", "")
            sums = [v["checksumValue"] for v in package.get("checksums", []) if v["algorithm"] == "SHA256"]
            if not url.startswith("https://") or urlsplit(url).hostname == "ghcr.io" or not sums:
                continue  # SBOM bottle references are binaries, not source.
            filename = str(index) + "-" + Path(urlsplit(url).path).name
            record = {"component": directory.name, "url": url, "sha256": sums[0],
                      "file": str((destination / filename).relative_to(output))}
            manifest.append(record)
            downloads.append(record)

    def download(record):
        target = output / record["file"]
        if target.is_file() and digest(target) == record["sha256"]:
            return record["component"]
        original = record["url"]
        urls = [original]
        if original.startswith("https://ftpmirror.gnu.org/gnu/"):
            relative = original.removeprefix("https://ftpmirror.gnu.org/gnu/")
            urls = ["https://mirrors.ustc.edu.cn/gnu/" + relative,
                    "https://mirrors.kernel.org/gnu/" + relative, original]
        elif original.startswith("https://www.infradead.org/openconnect/download/"):
            urls.insert(0, "https://distfiles.macports.org/openconnect/" + Path(urlsplit(original).path).name)
        for url in urls:
            result = subprocess.run(["curl", "--fail", "--location", "--silent", "--show-error", "--retry", "1",
                                     "--retry-all-errors", "--retry-delay", "1", "--connect-timeout", "10", "--max-time", "90",
                                     "--continue-at", "-", "--proto", "=https", "--proto-redir", "=https",
                                     "--output", str(target), url])
            if result.returncode == 0:
                if digest(target) == record["sha256"]:
                    return record["component"]
                target.unlink()  # A complete but incorrect file cannot be resumed.
        raise ValueError("No verified source download: " + record["component"])

    with ThreadPoolExecutor(max_workers=6) as pool:
        for future in as_completed([pool.submit(download, record) for record in downloads]):
            print("Verified component source:", future.result(), flush=True)

    modules = {}
    for executable in ("mihomo", "GoConnectTransport"):
        for fields in dependencies(args.app / "Contents/Resources/Runtime/bin" / executable):
            path, version, expected = fields
            modules[(path, version)] = expected
    module_dir = output / "go-modules"
    module_dir.mkdir(exist_ok=args.resume)

    def module_source(item):
        (path, version), expected = item
        for attempt in range(3):
            value = subprocess.run(["go", "mod", "download", "-json", path + "@" + version],
                                   cwd=output, capture_output=True, text=True, timeout=180)
            if value.returncode == 0:
                break
            if attempt == 2:
                raise ValueError("Module unavailable: " + path + "@" + version)
            time.sleep(1)
        data = json.loads(value.stdout)
        if data.get("Sum") != expected:
            raise ValueError("Go source checksum mismatch: " + path)
        escaped = "".join("!" + c.lower() if c.isupper() else c for c in path)
        relative = Path("go-modules") / (escaped.replace("/", "_") + "@" + version + ".zip")
        shutil.copy2(data["Zip"], output / relative)
        return {"module": path, "version": version, "sum": expected,
                "url": "https://proxy.golang.org/" + escaped + "/@v/" + version + ".zip",
                "file": str(relative), "sha256": digest(output / relative)}

    with ThreadPoolExecutor(max_workers=6) as pool:
        for future in as_completed([pool.submit(module_source, item) for item in modules.items()]):
            manifest.append(future.result())
    shutil.copy2(third_party / "components.json", output / "components.json")
    shutil.copy2(root / "THIRD_PARTY.md", output / "THIRD_PARTY.md")
    (output / "sources.json").write_text(json.dumps(manifest, indent=2) + "\n")
    (output / "README.txt").write_text(
        "GoConnect bundled component sources\n\n"
        "components/ contains source archives, applicable patches, licenses, Homebrew formulas and SBOMs.\n"
        "go-modules/ contains the exact Go dependencies linked into Mihomo and GoConnectTransport,\n"
        "verified against the h1 checksums recorded in those binaries. sources.json lists provenance\n"
        "and SHA-256 checksums. The project source and build scripts accompany the same GitHub tag.\n"
        "Use the recorded formulas for external libraries. Patched Mihomo includes its pinned lock,\n"
        "build_mihomo.py and patches/ recipe alongside upstream.tar.gz. Build GoConnect with\n"
        "script/build_and_run.sh. Replacing bundled binaries requires re-signing the local bundle.\n")
    archive = output.parent / (output.name + ".tar.gz")
    with tarfile.open(archive, "w:gz") as tar:
        tar.add(output, arcname=output.name)
    print(json.dumps({"archive": str(archive), "sourceFiles": len(manifest), "sha256": digest(archive)}), flush=True)


if __name__ == "__main__":
    main()
