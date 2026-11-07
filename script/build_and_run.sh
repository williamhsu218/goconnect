#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
CONFIG=release
LAUNCH=0
VERIFY=0
LOGS=0
DEBUG=0
EXPORT_DIR=""
ISOLATED_TEST=0
NETWORK_TEST=0
unset GOCONNECT_ISOLATED_TEST GOCONNECT_NETWORK_TEST
usage() {
  echo "Usage: $0 [--verify] [--build-only|--package] [--run] [--debug] [--team-sign] [--isolated-test|--network-test] [--export DIRECTORY] [--logs|--telemetry]"
}
# Swift Build in Xcode 27 records the deployment target as the linked SDK for
# this SwiftPM executable. The native backend records the actual SDK, which
# macOS uses to enable its current control appearance (including Liquid Glass).
while [[ $# -gt 0 ]]; do
  case "$1" in
    --verify) VERIFY=1 ;;
    --build-only|--package) LAUNCH=0 ;;
    --run) LAUNCH=1 ;;
    --debug) CONFIG=debug; DEBUG=1 ;;
    --isolated-test) ISOLATED_TEST=1; export GOCONNECT_ISOLATED_TEST=1 ;;
    --network-test) NETWORK_TEST=1; export GOCONNECT_NETWORK_TEST=1 ;;
    --team-sign) export GOCONNECT_REQUIRE_TEAM_SIGN=1 ;;
    --export)
      [[ $# -ge 2 && -n "$2" ]] || { usage >&2; exit 2; }
      EXPORT_DIR="$2"; shift ;;
    --logs|--telemetry) LOGS=1 ;;
    --help|-h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
  shift
done
[[ "$ISOLATED_TEST" != 1 || "$NETWORK_TEST" != 1 ]] || { echo "Choose one test flavor." >&2; exit 2; }
[[ -z "${GOCONNECT_OUTPUT_DIR:-}" ]] || {
  echo "GOCONNECT_OUTPUT_DIR is no longer a persistent build destination. Use --export with a directory outside this project." >&2
  exit 2
}
[[ "$VERIFY" != 1 || "$LAUNCH" != 1 ]] || { echo "--verify cannot launch the GUI." >&2; exit 2; }
if [[ -n "$EXPORT_DIR" ]]; then
  EXPORT_DIR="$(python3 - "$ROOT" "$EXPORT_DIR" <<'PY'
from pathlib import Path
import sys
root, output = map(lambda value: Path(value).resolve(), sys.argv[1:])
if output == root or root in output.parents:
    raise SystemExit("Export directory must be outside the project; dist is reserved for accepted releases.")
if output.exists():
    raise SystemExit("Export directory must be new; existing files will not be overwritten.")
print(output)
PY
)" || exit 2
fi
for tool in swift go python3 brew openconnect mihomo; do
  command -v "$tool" >/dev/null || { echo "Missing $tool. Install Xcode and run: brew install go openconnect mihomo" >&2; exit 1; }
done
python3 script/version.py --check
export MACOSX_DEPLOYMENT_TARGET="$(python3 script/version.py --field deploymentTarget)"
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/goconnect-build.XXXXXX")"
APP_PID=""
cleanup() {
  status=$?
  trap - EXIT
  if [[ -n "$APP_PID" ]] && kill -0 "$APP_PID" 2>/dev/null; then
    kill -TERM "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
  fi
  rm -rf -- "$WORK"
  echo "Temporary build directory removed."
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
export GOCONNECT_WORK_DIR="$WORK"
export GOCONNECT_OUTPUT_DIR="$WORK/package"
export GOCONNECT_TRANSPORT_BIN_DIR="$WORK/transport-bin"
export GOCONNECT_REGISTER_APP=0
export PYTHONDONTWRITEBYTECODE=1
SWIFT_BUILD_ARGS=(--build-system native --sdk "$(xcrun --sdk macosx --show-sdk-path)" --scratch-path "$WORK/swift")
SWIFT_TEST_ARGS=("${SWIFT_BUILD_ARGS[@]}")
GO_BUILD_ARGS=(-trimpath)
if [[ "$NETWORK_TEST" == 1 ]]; then SWIFT_BUILD_ARGS+=(-Xswiftc -DGOCONNECT_NETWORK_TEST); fi
if [[ "$ISOLATED_TEST" == 1 || "$NETWORK_TEST" == 1 ]]; then GO_BUILD_ARGS+=(-ldflags "-X main.serviceBuildFlavor=test"); fi
if [[ "$ISOLATED_TEST" == 1 ]]; then SWIFT_BUILD_ARGS+=(-Xswiftc -DGOCONNECT_ISOLATED_TEST); fi
echo "Temporary build directory: $WORK"
if [[ "$VERIFY" == 1 ]]; then
  python3 script/test_release_artifacts.py
  python3 script/test_request_probe.py
  python3 script/test_analyze_bridge_probe.py
  python3 script/test_yunyoo_subscription_service.py
  python3 script/test_signing.py
  python3 script/test_prepare_update.py
  swift test "${SWIFT_TEST_ARGS[@]}"
  (cd Transport && go test -race -timeout 60s ./...)
fi
swift build "${SWIFT_BUILD_ARGS[@]}" -c "$CONFIG"
SWIFT_BIN="$(swift build "${SWIFT_BUILD_ARGS[@]}" -c "$CONFIG" --show-bin-path)/GoConnect"
mkdir -p "$GOCONNECT_TRANSPORT_BIN_DIR"
(cd Transport && CGO_ENABLED=1 go build "${GO_BUILD_ARGS[@]}" -o "$GOCONNECT_TRANSPORT_BIN_DIR/GoConnectTransport" ./cmd/goconnect-transport)
SPARKLE_SDK="$WORK/swift/artifacts/sparkle/Sparkle"
export GOCONNECT_SPARKLE_SDK="$SPARKLE_SDK"
export GOCONNECT_SPARKLE_FRAMEWORK="$SPARKLE_SDK/Sparkle.xcframework/macos-arm64_x86_64/Sparkle.framework"
python3 script/package_app.py "$SWIFT_BIN"
APP="$GOCONNECT_OUTPUT_DIR/GoConnect.app"
if [[ "$ISOLATED_TEST" == 1 || "$NETWORK_TEST" == 1 ]]; then APP="$GOCONNECT_OUTPUT_DIR/GoConnect Dual VPN Test.app"; fi
"$APP/Contents/Resources/Runtime/bin/openconnect" --version
"$APP/Contents/Resources/Runtime/bin/mihomo" -v
"$APP/Contents/Resources/Runtime/bin/GoConnectTransport" version
if [[ "$NETWORK_TEST" == 1 || "$ISOLATED_TEST" == 1 ]]; then
  "$APP/Contents/Resources/Runtime/bin/GoConnectTransport" service-identity | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["flavor"] == "test" and v["label"] == "com.willhsu.GoConnect.DualVPNTest.network"'
fi
/usr/bin/codesign --verify --deep --strict "$APP"
if [[ "$VERIFY" == 1 ]]; then
  python3 script/check_runtime.py "$APP"
  (cd Transport && GOCONNECT_TEST_RUNTIME="$APP/Contents/Resources/Runtime" go test -count=1 -race -v -timeout 60s ./cmd/goconnect-transport -run "TestLive")
  (cd Transport && GOCONNECT_TEST_RUNTIME="$APP/Contents/Resources/Runtime" go test -count=1 -race -v -timeout 180s ./integration)
fi
if [[ -n "$EXPORT_DIR" ]]; then
  python3 - "$GOCONNECT_OUTPUT_DIR" "$EXPORT_DIR" <<'PY'
from pathlib import Path
import shutil
import sys
source, output = map(Path, sys.argv[1:])
output.mkdir(parents=True, exist_ok=False)
try:
    for item in [*source.glob("*.zip"), source / "SHA256SUMS"]:
        shutil.copy2(item, output / item.name)
except BaseException:
    shutil.rmtree(output)
    raise
PY
  echo "Candidate archive exported: $EXPORT_DIR"
fi
if [[ "$LAUNCH" == 1 ]]; then
  if [[ "$DEBUG" == 1 ]]; then
    echo "Attach with: lldb -n GoConnect"
  fi
  if [[ "$LOGS" == 1 ]]; then
    echo "In another terminal: log stream --style compact --predicate 'process == \"GoConnect\"' --level info"
  fi
  echo "Temporary app will be removed after it exits."
  GOCONNECT_DATA_DIR="$WORK/preview-data" "$APP/Contents/MacOS/GoConnect" &
  APP_PID=$!
  wait "$APP_PID"
  APP_PID=""
fi
echo "Package build and requested checks passed. Retained releases were not changed."
