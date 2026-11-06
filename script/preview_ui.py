#!/usr/bin/env python3
"""Build/run a disposable, synthetic UI app without the installed VPN runtime."""
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
import time

ROOT = Path(__file__).resolve().parents[1]
BUNDLE_ID = "com.willhsu.GoConnect.UIPreview"


def prepare(work: Path) -> Path:
    source = work / "source"
    source.mkdir()
    for name in ("Package.swift", "VERSION.json"):
        shutil.copy2(ROOT / name, source / name)
    shutil.copytree(ROOT / "Sources", source / "Sources")
    shutil.copytree(ROOT / "Tests", source / "Tests")
    hashes = {str(path.relative_to(source)): hashlib.sha256(path.read_bytes()).hexdigest() for path in (source / "Sources/GoConnect/Views").glob("*.swift")}
    (source / "original-view-hashes.json").write_text(json.dumps(hashes, indent=2))
    # Every override below applies only to the disposable copy.
    core = source / "Sources/GoConnectCore/Configuration.swift"
    core_text = core.read_text()
    if core_text.count('"com.willhsu.GoConnect"') != 1:
        raise RuntimeError("Cannot prove preview bundle identity isolation; refusing to build.")
    core.write_text(core_text.replace('"com.willhsu.GoConnect"', json.dumps(BUNDLE_ID)))
    app = source / "Sources/GoConnect"
    for path in app.rglob("*.swift"):
        text = path.read_text()
        for system, preview in [("colorSchemeContrast", "previewContrast"), ("accessibilityReduceTransparency", "previewReduceTransparency"), ("accessibilityReduceMotion", "previewReduceMotion")]:
            text = text.replace("@Environment(\\." + system + ")", "@Environment(\\." + preview + ")")
        path.write_text(text)
    shutil.copy2(ROOT / "script/preview_ui.swift", app / "App/GoConnectApp.swift")
    (app / "Services/KeychainStore.swift").write_text('''import Foundation
struct KeychainStore {
    func read(_ id: UUID) throws -> String? { nil }
    func write(_ password: String, id: UUID) throws { throw CocoaError(.userCancelled) }
    func delete(_ id: UUID) throws {}
}
''')
    web = app / "Services/YunyooQuotaClient.swift"
    web_text = web.read_text()
    if web_text.count("configuration.websiteDataStore = .default()") != 1:
        raise RuntimeError("Cannot prove preview WebKit storage isolation; refusing to build.")
    web.write_text(web_text.replace("configuration.websiteDataStore = .default()", "configuration.websiteDataStore = .nonPersistent()"))
    service = app / "Services/NetworkServiceClient.swift"
    text = service.read_text()
    start = text.index("    private func authorize(arguments:")
    end = text.index("    nonisolated private static func readStatus", start)
    service.write_text(text[:start] + '''    private func authorize(arguments: [String]) async throws {
        throw ServiceError("隔离界面测试不操作本机网络服务。")
    }

''' + text[end:])
    control = app / "Stores/AppStoreConnectionControl.swift"
    text = control.read_text()
    if text.count("    var connectionControlState: VPNControlState {\n        VPNControlState(") != 1:
        raise RuntimeError("Preview state injection no longer matches; refusing to build.")
    text = text.replace("    var connectionControlState: VPNControlState {\n        VPNControlState(", '''    var connectionControlState: VPNControlState {
        if let raw = ProcessInfo.processInfo.environment["GOCONNECT_PREVIEW_CONTROL_STATE"], let state = VPNControlState(rawValue: raw) { return state }
        return VPNControlState(''')
    control.write_text("import Foundation\n" + text)
    return source


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true", help="Launch the isolated native app; remove it on exit.")
    parser.add_argument("--evidence", type=Path, required=True, help="Project-external evidence directory.")
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        parser.error("Evidence must be outside the repository.")
    evidence.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="goconnect-ui-preview-") as temporary:
        work = Path(temporary)
        source = prepare(work)
        sdk = subprocess.check_output(["xcrun", "--sdk", "macosx", "--show-sdk-path"], text=True).strip()
        build = ["swift", "build", "--build-system", "native", "--sdk", sdk, "--scratch-path", str(work / "swift")]
        with (evidence / "preview-build.log").open("w") as log:
            subprocess.run(build, cwd=source, stdout=log, stderr=subprocess.STDOUT, check=True)
        binary = Path(subprocess.check_output([*build, "--show-bin-path"], cwd=source, text=True).strip()) / "GoConnect"
        bundle = work / "GoConnect UI Preview.app"
        contents = bundle / "Contents"
        (contents / "MacOS").mkdir(parents=True)
        (contents / "Resources").mkdir()
        shutil.copy2(binary, contents / "MacOS/GoConnect")
        with (contents / "Info.plist").open("wb") as stream:
            plistlib.dump({"CFBundleIdentifier": BUNDLE_ID, "CFBundleExecutable": "GoConnect", "CFBundleName": "GoConnect UI Preview", "CFBundlePackageType": "APPL", "LSMinimumSystemVersion": "14.0", "NSPrincipalClass": "NSApplication", "CFBundleDevelopmentRegion": "zh_CN", "CFBundleLocalizations": ["zh-Hans"]}, stream)
        subprocess.run(["codesign", "--force", "--sign", "-", str(bundle)], check=True)
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(bundle)], check=True)
        command = evidence / "preview-command.json"
        command.write_text(json.dumps({"sequence": time.time(), "scenario": "connected", "page": "connection", "theme": "light", "width": 980, "height": 700}))
        metadata = {"bundleID": BUNDLE_ID, "app": str(bundle), "data": str(work / "data"), "source": str(source), "command": str(command), "runtimeIncluded": False, "keychain": "stub", "webStorage": "nonPersistent", "privilegedAuthorization": "blocked in disposable source"}
        metadata["sourceViewHashes"] = json.loads((source / "original-view-hashes.json").read_text())
        (evidence / "preview-session.json").write_text(json.dumps(metadata, indent=2))
        print(json.dumps({key: value for key, value in metadata.items() if key != "sourceViewHashes"}), flush=True)
        if not args.run:
            return
        environment = {"GOCONNECT_DATA_DIR": str(work / "data"), "GOCONNECT_PREVIEW_EVIDENCE": str(evidence)}
        launch = ["open", "-n", "-W"]
        for key, value in environment.items():
            launch += ["--env", f"{key}={value}"]
        launch += [str(bundle)]
        process = subprocess.Popen(launch)
        def stop(_signum, _frame):
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, stop)
        try:
            return_code = process.wait()
            if return_code:
                raise subprocess.CalledProcessError(return_code, launch)
        finally:
            pid_file = evidence / "preview-pid.txt"
            if pid_file.exists():
                pid = int(pid_file.read_text())
                actual = subprocess.run(["ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True).stdout.strip()
                if actual and Path(actual).resolve() == (contents / "MacOS/GoConnect").resolve():
                    os.kill(pid, signal.SIGTERM)
                    for _ in range(50):
                        if subprocess.run(["kill", "-0", str(pid)], stderr=subprocess.DEVNULL).returncode:
                            break
                        time.sleep(0.1)
            if process.poll() is None:
                process.terminate()
    print("Isolated preview and temporary build removed.", flush=True)


if __name__ == "__main__":
    main()
