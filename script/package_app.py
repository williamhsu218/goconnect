#!/usr/bin/env python3
"""Stage a relocatable local .app with only the runtime dependencies it uses."""
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
DIST = Path(os.environ.get("GOCONNECT_OUTPUT_DIR", ROOT / "dist")).resolve()
STAGE = DIST / ".GoConnect.stage.app"
APP = DIST / "GoConnect.app"
OTOOL = shutil.which("otool") or "/usr/bin/otool"
INSTALL_NAME_TOOL = shutil.which("install_name_tool") or "/usr/bin/install_name_tool"

def run(*arguments, **options):
    return subprocess.check_output([str(a) for a in arguments], text=True, **options).strip()

DIST.mkdir(parents=True, exist_ok=True)
if STAGE.exists():
    shutil.rmtree(STAGE)
bin_dir = STAGE / "Contents/Resources/Runtime/bin"
lib_dir = STAGE / "Contents/Resources/Runtime/lib"
share_dir = STAGE / "Contents/Resources/Runtime/share"
licenses = STAGE / "Contents/Resources/ThirdParty"
for directory in (bin_dir, lib_dir, share_dir, licenses, STAGE / "Contents/MacOS"):
    directory.mkdir(parents=True, exist_ok=True)
shutil.copy2(sys.argv[1], STAGE / "Contents/MacOS/GoConnect")
shutil.copy2(ROOT / "Transport/bin/GoConnectTransport", bin_dir / "GoConnectTransport")
shutil.copy2(ROOT / "Transport/bin/GoConnectLauncher", bin_dir / "GoConnectLauncher")
shutil.copy2(ROOT / "Packaging/Licenses/vpnonly-MIT.txt", licenses / "vpnonly-MIT.txt")

brew_cellar = Path(run("brew", "--cellar"))
packages = set()
copied = {}

def remember_license(source):
    try:
        relative = source.resolve().relative_to(brew_cellar)
    except ValueError:
        return
    package = brew_cellar / relative.parts[0] / relative.parts[1]
    if package in packages:
        return
    packages.add(package)
    destination = licenses / (relative.parts[0] + "-" + relative.parts[1])
    destination.mkdir()
    for item in package.iterdir():
        if item.is_file() and (item.name.startswith(("LICENSE", "COPYING", "NOTICE")) or item.name in ("AUTHORS", "sbom.spdx.json")):
            shutil.copy2(item, destination / item.name)

def embed(source, destination):
    source = source.resolve()
    if source in copied:
        return copied[source]
    if destination.exists():
        raise RuntimeError(f"Dependency basename collision: {destination.name}")
    copied[source] = destination
    shutil.copy2(source, destination)
    destination.chmod(0o755)
    remember_license(source)
    dependencies = []
    for line in run(OTOOL, "-L", source).splitlines()[1:]:
        dependency = line.strip().split(" (compatibility version")[0]
        if dependency.startswith(("/opt/homebrew/", "/usr/local/")):
            dep = Path(dependency)
            if dep.resolve() == source:
                continue
            embedded = embed(dep, lib_dir / dep.name)
            replacement = "@loader_path/" + os.path.relpath(embedded, destination.parent)
            dependencies.extend(["-change", dependency, replacement])
        elif not dependency.startswith(("/usr/lib/", "/System/Library/")):
            raise RuntimeError(f"Unresolved dependency: {dependency}")
    if destination.suffix == ".dylib":
        dependencies.extend(["-id", "@loader_path/" + destination.name])
    if dependencies:
        subprocess.run([INSTALL_NAME_TOOL, *dependencies, str(destination)], check=True, capture_output=True)
    return destination

for name in ("openconnect", "mihomo"):
    embed(Path(shutil.which(name)), bin_dir / name)
ca = Path(run("brew", "--prefix")) / "etc/ca-certificates/cert.pem"
shutil.copy2(ca, share_dir / "cacert.pem")

# Include exact module versions, source locations and the accompanying license texts.
modules_raw = run("go", "list", "-m", "-json", "all", cwd=ROOT / "Transport")
decoder = json.JSONDecoder()
modules = []
while modules_raw.strip():
    value, offset = decoder.raw_decode(modules_raw.lstrip())
    modules_raw = modules_raw.lstrip()[offset:]
    if value.get("Main"):
        continue
    modules.append({k: value[k] for k in ("Path", "Version")})
    directory = Path(value.get("Dir", ""))
    if directory.is_dir():
        target = licenses / value["Path"].replace("/", "_")
        target.mkdir(exist_ok=True)
        for item in directory.iterdir():
            if item.is_file() and item.name.startswith(("LICENSE", "COPYING", "NOTICE")):
                shutil.copy2(item, target / item.name)
metadata = {"openconnect": run(shutil.which("openconnect"), "--version").splitlines()[0],
            "mihomo": run(shutil.which("mihomo"), "-v").splitlines()[0], "goModules": modules,
            "sources": ["https://gitlab.com/openconnect/openconnect", "https://github.com/MetaCubeX/mihomo", "https://git.zx2c4.com/wireguard-go", "https://github.com/google/gvisor"]}
(licenses / "components.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
if (ROOT / "THIRD_PARTY.md").exists():
    shutil.copy2(ROOT / "THIRD_PARTY.md", licenses / "README.md")

icon = ROOT / "Packaging/GoConnect.icns"
subprocess.run(["swift", str(ROOT / "script/make_icon.swift"), str(icon)], check=True)
# A content-specific resource name also invalidates cached icons for pinned Dock items.
icon_name = "GoConnect-" + hashlib.sha256(icon.read_bytes()).hexdigest()[:12] + ".icns"
shutil.copy2(icon, STAGE / "Contents/Resources" / icon_name)

minimum = "14.0"
for binary in [*copied.values(), STAGE / "Contents/MacOS/GoConnect", bin_dir / "GoConnectTransport", bin_dir / "GoConnectLauncher"]:
    output = run(OTOOL, "-l", binary)
    for value in re.findall(r"\bminos\s+([0-9.]+)", output):
        if tuple(map(int, value.split("."))) > tuple(map(int, minimum.split("."))):
            minimum = value
info = {"CFBundleIdentifier": "com.willhsu.GoConnect", "CFBundleName": "GoConnect",
        "CFBundleDisplayName": "GoConnect", "CFBundleExecutable": "GoConnect", "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": "0.11.4", "CFBundleVersion": "41", "CFBundleIconFile": icon_name,
        "LSMinimumSystemVersion": minimum, "NSHighResolutionCapable": True,
        "NSHumanReadableCopyright": "GoConnect contributors. Includes open-source components."}
with (STAGE / "Contents/Info.plist").open("wb") as stream:
    plistlib.dump(info, stream)

for binary in [*lib_dir.iterdir(), *bin_dir.iterdir(), STAGE / "Contents/MacOS/GoConnect"]:
    options = ["--options", "runtime"] if binary.name == "GoConnectTransport" else []
    subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", *options, str(binary)], check=True, capture_output=True)
subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(STAGE)], check=True, capture_output=True)
subprocess.run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(STAGE)], check=True)

# Stop only this checkout's GUI. A live tunnel must be disconnected explicitly first.
processes = run("/bin/ps", "-axo", "pid=,command=").splitlines()
for line in processes:
    fields = line.strip().split(None, 1)
    if len(fields) == 2 and fields[1].startswith(str(APP / "Contents/Resources/Runtime/bin") + "/"):
        raise RuntimeError("Disconnect GoConnect before replacing its running transport.")
for line in processes:
    fields = line.strip().split(None, 1)
    if len(fields) == 2 and fields[1] == str(APP / "Contents/MacOS/GoConnect"):
        pid = int(fields[0]); os.kill(pid, signal.SIGTERM)
        for _ in range(30):
            try: os.kill(pid, 0)
            except ProcessLookupError: break
            time.sleep(0.1)
        else: raise RuntimeError("GoConnect did not exit; close it before rebuilding.")
if APP.exists():
    shutil.rmtree(APP)
STAGE.rename(APP)
# Refresh only this bundle's Launch Services registration; preserve the user's Dock layout.
lsregister = Path("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister")
if lsregister.exists() and os.environ.get("GOCONNECT_REGISTER_APP", "1") == "1":
    subprocess.run([str(lsregister), "-f", str(APP)], check=True)
archive = DIST / f"GoConnect-0.11.4-macos-{os.uname().machine}.zip"
if archive.exists(): archive.unlink()
subprocess.run(["/usr/bin/ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(APP), str(archive)], check=True)
(DIST / "SHA256SUMS").write_text(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name + "\n")
print(f"Packaged for macOS {minimum}+ ({os.uname().machine}): {archive}")
