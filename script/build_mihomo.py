#!/usr/bin/env python3
"""Build GoConnect's pinned, narrowly patched Mihomo outside the project."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
from urllib.request import urlopen


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def load_lock(path):
    lock = json.loads(path.read_text())
    if (lock["version"] != "1.19.30" or lock["buildVersion"] != "v1.19.30-goconnect1"
            or lock["url"] != "https://codeload.github.com/MetaCubeX/mihomo/tar.gz/refs/tags/v1.19.30"
            or lock["tags"] != ["with_gvisor"]):
        raise ValueError("Unexpected Mihomo source/build identity")
    patch = (path.parent / lock["patch"]).resolve()
    if not patch.is_relative_to(path.parent.resolve()) or digest(patch) != lock["patchSHA256"]:
        raise ValueError("Mihomo patch identity mismatch")
    return lock, patch


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--lock", type=Path, default=Path(__file__).with_name("mihomo.lock.json"))
    args = parser.parse_args()
    project = Path(__file__).resolve().parent.parent
    work, output = args.work.resolve(), args.output.resolve()
    if work == project or work.is_relative_to(project) or output == work or not output.is_relative_to(work):
        parser.error("Mihomo source, scratch and output must stay in an external build directory")
    if work.exists() and any(work.iterdir()):
        parser.error("Mihomo build directory must be empty")
    work.mkdir(parents=True, exist_ok=True)
    lock, patch = load_lock(args.lock.resolve())
    archive = work / "mihomo-source.tar.gz"
    with urlopen(lock["url"], timeout=60) as response, archive.open("wb") as stream:
        shutil.copyfileobj(response, stream)
    if digest(archive) != lock["sha256"]:
        raise ValueError("Mihomo source archive SHA-256 mismatch")
    with tarfile.open(archive) as source:
        source.extractall(work, filter="data")
    source = work / ("mihomo-" + lock["version"])
    subprocess.run(["/usr/bin/patch", "-s", "-N", "-p1", "-i", str(patch)], cwd=source, check=True)
    subprocess.run(["go", "mod", "verify"], cwd=source, check=True)
    # These tests verify resolver selection and refusal of implicit OS fallback.
    subprocess.run(["go", "test", "-tags", "with_gvisor", "./tunnel", "-run", "^TestGoConnect", "-count=1"],
                   cwd=source, check=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    flags = "-s -w -X github.com/metacubex/mihomo/constant.Version=" + lock["buildVersion"]
    subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-tags", "with_gvisor", "-ldflags", flags,
                    "-o", str(output), "."], cwd=source, check=True)
    version = subprocess.check_output([str(output), "-v"], text=True).splitlines()[0]
    if lock["buildVersion"] not in version:
        raise ValueError("Built Mihomo capability/version mismatch")
    provenance = {"name": "Mihomo", "kind": "goconnect-patched-go", "version": lock["buildVersion"],
                  "url": lock["url"], "sha256": lock["sha256"], "patchSHA256": lock["patchSHA256"],
                  "unsignedBinarySHA256": digest(output), "tags": lock["tags"]}
    (work / "source.json").write_text(json.dumps(provenance, indent=2) + "\n")
    print(version)


if __name__ == "__main__":
    main()
