#!/usr/bin/env python3
"""Read-only component checks and Mihomo configuration validation (no TUN)."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

app = Path(sys.argv[1]).resolve()
runtime = app / "Contents/Resources/Runtime"
for binary in [*(runtime / "bin").iterdir(), *(runtime / "lib").iterdir()]:
    text = subprocess.check_output(["otool", "-L", str(binary)], text=True)
    for line in text.splitlines()[1:]:
        if "/opt/homebrew/" in line or "/usr/local/" in line:
            raise SystemExit(f"External dependency: {binary.name}: {line}")
with tempfile.TemporaryDirectory(prefix="goconnect-check-") as directory:
    path = Path(directory) / "session.json"
    session = {"ownerPID": os.getpid(), "controlDirectory": directory,
                               "token": "test-configuration-token-0000000000000000", "socksPort": 18081,
                               "appPaths": ["/Applications/Example (QA).app"], "gateway": "192.0.2.10"}
    for mode, live, empty in [("whitelist", False, False), ("global", False, False), ("whitelist", True, False), ("global", True, False), ("whitelist", True, True), ("global", True, True)]:
        session["liveRouting"] = live
        session["routingMode"] = mode
        session["appPaths"] = [] if empty else ["/Applications/Example (QA).app"]
        session["directDomains"] = [{"domain": "example.com", "includeSubdomains": True}]
        path.write_text(json.dumps(session))
        subprocess.run([str(runtime / "bin/GoConnectTransport"), "config-check", str(path)], check=True)
print("Runtime dependencies and legacy/live Mihomo modes, empty App lists and direct-domain exceptions passed; no system routes changed.")
