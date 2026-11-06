#!/usr/bin/env python3
"""Render the actual menu components with inert data, outside the installed app."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
COMPONENTS = [
    "Sources/GoConnect/App/AppBranding.swift",
    "Sources/GoConnect/App/MenuBarStatusIcon.swift",
    "Sources/GoConnect/App/MenuBarController.swift",
    "Sources/GoConnect/App/MenuBarPreferences.swift",
    "Sources/GoConnect/Views/MenuBarSettings.swift",
]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true", help="Explicitly launch the isolated preview; remove the app on exit")
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        parser.error("Evidence must be outside the repository")
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    bundle_id = "com.willhsu.GoConnect.MenuBarPreview." + hashlib.sha256(str(evidence).encode()).hexdigest()[:10]
    with tempfile.TemporaryDirectory(prefix="goconnect-menubar-preview-") as folder:
        work = Path(folder)
        source = work / "Sources/Preview"
        source.mkdir(parents=True)
        shutil.copytree(ROOT / "Sources/GoConnectCore", work / "Sources/GoConnectCore")
        identity = work / "Sources/GoConnectCore/Configuration.swift"
        identity_text = identity.read_text()
        if identity_text.count('"com.willhsu.GoConnect"') != 1:
            raise RuntimeError("Cannot prove preview identity isolation")
        identity.write_text(identity_text.replace('"com.willhsu.GoConnect"', json.dumps(bundle_id)))
        hashes = {}
        for name in COMPONENTS:
            original = ROOT / name
            shutil.copy2(original, source / original.name)
            hashes[name] = hashlib.sha256(original.read_bytes()).hexdigest()
        shutil.copy2(ROOT / "script/preview_menubar.swift", source / "Preview.swift")
        (work / "Package.swift").write_text('''// swift-tools-version: 5.10
import PackageDescription
let package = Package(name: "MenuBarPreview", platforms: [.macOS("14.0")], targets: [
    .target(name: "GoConnectCore"),
    .executableTarget(name: "Preview", dependencies: ["GoConnectCore"], linkerSettings: [.linkedFramework("SwiftUI")])
])
''')
        sdk = subprocess.check_output(["xcrun", "--sdk", "macosx", "--show-sdk-path"], text=True).strip()
        build = ["swift", "build", "--build-system", "native", "--sdk", sdk, "--scratch-path", str(work / "build")]
        with (evidence / "build.log").open("w") as log:
            subprocess.run(build, cwd=work, stdout=log, stderr=subprocess.STDOUT, check=True)
        binary = Path(subprocess.check_output([*build, "--show-bin-path"], cwd=work, text=True).strip()) / "Preview"
        app = work / "GoConnect MenuBar Preview.app"
        contents = app / "Contents"
        (contents / "MacOS").mkdir(parents=True)
        shutil.copy2(binary, contents / "MacOS/Preview")
        version = json.loads((ROOT / "VERSION.json").read_text())
        (contents / "Info.plist").write_bytes(plistlib.dumps({
            "CFBundleIdentifier": bundle_id, "CFBundleName": "GoConnect MenuBar Preview",
            "CFBundleExecutable": "Preview", "CFBundlePackageType": "APPL",
            "CFBundleShortVersionString": version["version"], "CFBundleVersion": version["build"],
            "LSMinimumSystemVersion": "14.0", "NSPrincipalClass": "NSApplication",
            "CFBundleDevelopmentRegion": "zh_CN", "CFBundleLocalizations": ["zh-Hans"],
        }))
        subprocess.run(["codesign", "--force", "--sign", "-", str(app)], check=True, capture_output=True)
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
        metadata = {"bundleID": bundle_id, "app": str(app), "synthetic": True,
                    "networkImplementationIncluded": False, "credentialsIncluded": False, "coreIdentityIsolated": True,
                    "defaultsDomain": bundle_id, "componentHashes": hashes}
        (evidence / "session.json").write_text(json.dumps(metadata, indent=2) + "\n")
        print(json.dumps(metadata), flush=True)
        if not args.run:
            return
        environment = os.environ.copy()
        environment["GOCONNECT_PREVIEW_EVIDENCE"] = str(evidence)
        process = subprocess.Popen(["open", "-n", "-W", "--env", "GOCONNECT_PREVIEW_EVIDENCE=" + str(evidence), str(app)], env=environment)

        def interrupt(_signum, _frame):
            raise KeyboardInterrupt

        signal.signal(signal.SIGTERM, interrupt)
        try:
            if process.wait():
                raise RuntimeError("Preview launcher failed")
        finally:
            pid_file = evidence / "preview-pid.txt"
            if pid_file.exists():
                pid = int(pid_file.read_text())
                command = subprocess.run(["ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True).stdout.strip()
                if command and Path(command).resolve() == (contents / "MacOS/Preview").resolve():
                    os.kill(pid, signal.SIGTERM)
            if process.poll() is None:
                process.terminate()
                process.wait(timeout=5)
            registration = Path("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister")
            if registration.exists():
                subprocess.run([str(registration), "-u", str(app)], check=False, capture_output=True)


if __name__ == "__main__":
    main()
