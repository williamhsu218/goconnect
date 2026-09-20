#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
CONFIG=release
LAUNCH=1
VERIFY=0
LOGS=0
DEBUG=0
for argument in "$@"; do
  case "$argument" in
    --verify) VERIFY=1; LAUNCH=0 ;;
    --build-only|--package) LAUNCH=0 ;;
    --debug) CONFIG=debug; DEBUG=1 ;;
    --logs|--telemetry) LOGS=1 ;;
    *) echo "Usage: $0 [--verify] [--build-only] [--debug] [--logs] [--telemetry]" >&2; exit 2 ;;
  esac
done
for tool in swift go python3 brew openconnect mihomo; do
  command -v "$tool" >/dev/null || { echo "Missing $tool. Install Xcode and run: brew install go openconnect mihomo" >&2; exit 1; }
done
if [[ "$VERIFY" == 1 ]]; then
  swift test
  (cd Transport && go test -race -timeout 60s ./...)
fi
swift build -c "$CONFIG"
SWIFT_BIN="$(swift build -c "$CONFIG" --show-bin-path)/GoConnect"
mkdir -p Transport/bin
(cd Transport && CGO_ENABLED=1 go build -trimpath -o bin/GoConnectTransport ./cmd/goconnect-transport)
clang -O2 -Wall -Wextra -Werror Transport/launcher/main.c -o Transport/bin/GoConnectLauncher
python3 script/package_app.py "$SWIFT_BIN"
APP="${GOCONNECT_OUTPUT_DIR:-$ROOT/dist}/GoConnect.app"
"$APP/Contents/Resources/Runtime/bin/openconnect" --version
"$APP/Contents/Resources/Runtime/bin/mihomo" -v
"$APP/Contents/Resources/Runtime/bin/GoConnectTransport" version
/usr/bin/codesign --verify --deep --strict "$APP"
if [[ "$VERIFY" == 1 ]]; then
  python3 script/check_runtime.py "$APP"
  (cd Transport && GOCONNECT_TEST_RUNTIME="$APP/Contents/Resources/Runtime" go test -count=1 -race -v -timeout 60s ./cmd/goconnect-transport -run "TestLive")
  (cd Transport && GOCONNECT_TEST_RUNTIME="$APP/Contents/Resources/Runtime" go test -count=1 -race -v -timeout 60s ./integration)
fi
if [[ "$LAUNCH" == 1 ]]; then
  /usr/bin/open -n "$APP"
  if [[ "$DEBUG" == 1 ]]; then
    echo "App launched. Attach with: lldb -n GoConnect"
  fi
  if [[ "$LOGS" == 1 ]]; then
    /usr/bin/log stream --style compact --predicate 'process == "GoConnect"' --level info
  fi
fi
echo "Built: $APP"
