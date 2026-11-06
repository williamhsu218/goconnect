#!/usr/bin/env python3
"""Prepare an authenticated GitHub appcast without publishing or exporting keys."""
import argparse
import base64
from datetime import datetime, timezone
import hashlib
from pathlib import Path, PurePosixPath
import plistlib
import posixpath
import re
import stat
import subprocess
import tempfile
from urllib.parse import quote, urlsplit
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parent.parent
ACCOUNT = "com.willhsu.GoConnect"
NS = "http://www.andymatuschak.org/xml-namespaces/sparkle"
ET.register_namespace("sparkle", NS)


def validate_archive(path):
    app_roots = set()
    with zipfile.ZipFile(path) as archive:
        names = set()
        for entry in archive.infolist():
            name = entry.filename
            parts = PurePosixPath(name).parts
            if not parts or name.startswith("/") or "\\" in name or ".." in parts or name in names:
                raise ValueError("Unsafe or duplicate archive member")
            names.add(name)
            if parts[0] == "__MACOSX":
                continue
            if not parts[0].endswith(".app"):
                raise ValueError("Update ZIP must contain only one top-level app")
            app_roots.add(parts[0])
            if stat.S_ISLNK(entry.external_attr >> 16):
                target = archive.read(entry).decode("utf-8")
                resolved = posixpath.normpath(posixpath.join(posixpath.dirname(name), target))
                if target.startswith("/") or "\\" in target or not PurePosixPath(resolved).parts or PurePosixPath(resolved).parts[0] != parts[0]:
                    raise ValueError("Archive symlink escapes the app")
    if len(app_roots) != 1:
        raise ValueError("Expected exactly one top-level app")
    return next(iter(app_roots))


def validate_download_url(url, version, name, qa=False):
    parsed = urlsplit(url)
    if parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("Update URL must not contain credentials, query, or fragment")
    if qa:
        if parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or not parsed.port or not 1 <= parsed.port <= 65535:
            raise ValueError("QA downloads require an explicit loopback port")
    elif parsed.scheme != "https" or parsed.hostname != "github.com" or parsed.port not in (None, 443) or parsed.path != f"/williamhsu218/goconnect/releases/download/v{version}/{quote(name)}":
        raise ValueError("Production update URL must match this GitHub release asset")


def validate_bundle_info(info, qa=False):
    expected = ACCOUNT + ".UpdateQA" if qa else ACCOUNT
    if info.get("CFBundleIdentifier") != expected or (qa and info.get("GoConnectUpdateQAMode") is not True):
        raise ValueError("Wrong bundle identifier for this feed")
    if info.get("GoConnectSigningMode") != "team" or not re.fullmatch(r"[A-Z0-9]{10}", info.get("GoConnectSigningTeam", "")):
        raise ValueError("OTA requires a verified team-signed app")
    for key in ("SURequireSignedFeed", "SUVerifyUpdateBeforeExtraction", "GoConnectUpdatesEnabled"):
        if info.get(key) is not True:
            raise ValueError("Required update authentication setting missing: " + key)
    for key in ("SUAllowsAutomaticUpdates", "SUAutomaticallyUpdate"):
        if info.get(key) is not False:
            raise ValueError("Unattended installation must be disabled")
    if not qa and info.get("GoConnectUpdateQAMode") is True:
        raise ValueError("Production bundle cannot enable QA mode")
    feed = urlsplit(info.get("SUFeedURL", ""))
    if qa:
        if feed.scheme != "http" or feed.hostname != "127.0.0.1" or not feed.port or feed.path != "/appcast.xml" or feed.username or feed.password or feed.query or feed.fragment:
            raise ValueError("QA feed must be the explicit loopback appcast")
    elif info.get("SUFeedURL") != "https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml":
        raise ValueError("Wrong production feed")
    if info.get("GoConnectUpdateChannel") not in ("stable", "preview"):
        raise ValueError("Invalid update channel")
    if not re.fullmatch(r"[0-9]+", info.get("CFBundleVersion", "")) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", info.get("CFBundleShortVersionString", "")):
        raise ValueError("Expected a numeric build and semantic version")
    if len(base64.b64decode(info.get("SUPublicEDKey", ""), validate=True)) != 32:
        raise ValueError("Expected an Ed25519 public key")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--sparkle-bin", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--download-url")
    parser.add_argument("--qa-feed", action="store_true")
    args = parser.parse_args()
    archive = args.archive.resolve(strict=True)
    output = args.output.resolve()
    if ROOT == output or ROOT in output.parents or output.exists():
        parser.error("Output must be a new file outside the source tree")
    app_name = validate_archive(archive)
    with tempfile.TemporaryDirectory(prefix="goconnect-update-") as folder:
        subprocess.run(["ditto", "-x", "-k", str(archive), folder], check=True)
        app = Path(folder) / app_name
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
        info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
        validate_bundle_info(info, args.qa_feed)
        details = subprocess.check_output(["codesign", "-dvv", str(app)], stderr=subprocess.STDOUT, text=True)
        if "TeamIdentifier=" + info["GoConnectSigningTeam"] not in details or "runtime" not in details:
            raise ValueError("Archive signature does not match its declared team/runtime")
        public = subprocess.check_output([str(args.sparkle_bin / "generate_keys"), "--account", ACCOUNT, "-p"], text=True).strip()
        if public != info["SUPublicEDKey"]:
            raise ValueError("Archive public key does not match this project's Keychain account")
    version, build = info["CFBundleShortVersionString"], info["CFBundleVersion"]
    url = args.download_url or f"https://github.com/williamhsu218/goconnect/releases/download/v{version}/{quote(archive.name)}"
    validate_download_url(url, version, archive.name, args.qa_feed)
    signer = str(args.sparkle_bin / "sign_update")
    signature = subprocess.check_output([signer, "--account", ACCOUNT, "-p", str(archive)], text=True).strip()
    subprocess.run([signer, "--account", ACCOUNT, "--verify", str(archive), signature], check=True)
    feed = ET.Element("rss", {"version": "2.0"})
    channel = ET.SubElement(feed, "channel")
    ET.SubElement(channel, "title").text = "GoConnect updates"
    ET.SubElement(channel, "link").text = "https://github.com/williamhsu218/goconnect/releases"
    ET.SubElement(channel, "description").text = "GoConnect signed updates"
    item = ET.SubElement(channel, "item")
    ET.SubElement(item, "title").text = "GoConnect " + version
    ET.SubElement(item, "pubDate").text = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")
    ET.SubElement(item, "{" + NS + "}version").text = build
    ET.SubElement(item, "{" + NS + "}shortVersionString").text = version
    ET.SubElement(item, "{" + NS + "}minimumSystemVersion").text = info["LSMinimumSystemVersion"]
    if info["GoConnectUpdateChannel"] == "preview":
        ET.SubElement(item, "{" + NS + "}channel").text = "preview"
    ET.SubElement(item, "description").text = "<p>团队签名、受保护的在线更新与新图标。安装前请断开 VPN 并保存线路草稿。</p>"
    ET.SubElement(item, "enclosure", {"url": url, "length": str(archive.stat().st_size), "type": "application/octet-stream", "{" + NS + "}edSignature": signature})
    output.parent.mkdir(parents=True, exist_ok=True)
    ET.indent(feed, space="  ")
    ET.ElementTree(feed).write(output, encoding="utf-8", xml_declaration=True)
    subprocess.run([signer, "--account", ACCOUNT, str(output)], check=True, capture_output=True)
    subprocess.run([signer, "--account", ACCOUNT, "--verify", str(output)], check=True)
    print("Signed archive and feed verified:", output)
    print("Archive SHA256:", hashlib.sha256(archive.read_bytes()).hexdigest())
    print("Publish verified release assets before promoting the signed feed.")


if __name__ == "__main__":
    main()
