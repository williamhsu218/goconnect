#!/usr/bin/env python3
import base64
from pathlib import Path
import stat
import tempfile
import unittest
import zipfile
from prepare_update import validate_archive, validate_bundle_info, validate_download_url


class UpdateArtifactBoundaryTests(unittest.TestCase):
    def test_release_url_cannot_point_elsewhere_or_carry_credentials(self):
        name = "GoConnect-1.0.1-macos-arm64.zip"
        valid = "https://github.com/williamhsu218/goconnect/releases/download/v1.0.1/" + name
        validate_download_url(valid, "1.0.1", name)
        for url in [valid.replace("github.com", "example.com"), valid.replace("v1.0.1", "v1.0.0"),
                    valid + "?token=private", valid + "#x", valid.replace("https://", "https://user:pass@"),
                    valid.replace("https://", "http://")]:
            with self.assertRaises(ValueError): validate_download_url(url, "1.0.1", name)

    def test_loopback_download_is_qa_only(self):
        validate_download_url("http://127.0.0.1:12345/target.zip", "1.0.1", "target.zip", qa=True)
        for url in ["http://127.0.0.1:12345/target.zip", "http://localhost:12345/target.zip"]:
            with self.assertRaises(ValueError): validate_download_url(url, "1.0.1", "target.zip")
        with self.assertRaises(ValueError):
            validate_download_url("http://localhost:12345/target.zip", "1.0.1", "target.zip", qa=True)

    def test_archive_traversal_multiple_apps_and_escaping_symlinks_are_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            for member in ["../outside", "/outside", "GoConnect.app/../outside", "other.app/file", "loose.txt"]:
                path = Path(folder) / "fixture.zip"
                with zipfile.ZipFile(path, "w") as z:
                    z.writestr("GoConnect.app/Contents/Info.plist", b"fixture")
                    z.writestr(member, b"fixture")
                with self.assertRaises(ValueError): validate_archive(path)
            with zipfile.ZipFile(path, "w") as z:
                z.writestr("GoConnect.app/Contents/Info.plist", b"fixture")
                link = zipfile.ZipInfo("GoConnect.app/Contents/link")
                link.create_system = 3; link.external_attr = (stat.S_IFLNK | 0o777) << 16
                z.writestr(link, "../../../outside")
            with self.assertRaises(ValueError): validate_archive(path)

    def test_framework_relative_symlink_is_retained(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "fixture.zip"
            with zipfile.ZipFile(path, "w") as z:
                z.writestr("GoConnect.app/Contents/Info.plist", b"fixture")
                link = zipfile.ZipInfo("GoConnect.app/Contents/Frameworks/Sparkle.framework/Versions/Current")
                link.create_system = 3; link.external_attr = (stat.S_IFLNK | 0o777) << 16
                z.writestr(link, "B")
            self.assertEqual(validate_archive(path), "GoConnect.app")

    def test_adhoc_or_unsigned_feed_cannot_be_promoted(self):
        info = {"CFBundleIdentifier": "com.willhsu.GoConnect", "CFBundleVersion": "58", "CFBundleShortVersionString": "1.0.1",
                "GoConnectSigningMode": "team", "GoConnectSigningTeam": "REALTEAM01", "GoConnectUpdatesEnabled": True,
                "SURequireSignedFeed": True, "SUVerifyUpdateBeforeExtraction": True,
                "SUAllowsAutomaticUpdates": False, "SUAutomaticallyUpdate": False,
                "SUFeedURL": "https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml",
                "SUPublicEDKey": base64.b64encode(bytes(32)).decode(), "GoConnectUpdateChannel": "preview"}
        validate_bundle_info(info)
        for key, value in [("GoConnectSigningMode", "adhoc"), ("SURequireSignedFeed", False),
                           ("SUVerifyUpdateBeforeExtraction", False), ("SUAutomaticallyUpdate", True),
                           ("GoConnectUpdateQAMode", True), ("SUFeedURL", "http://127.0.0.1:12345/appcast.xml")]:
            with self.assertRaises(ValueError): validate_bundle_info({**info, key: value})


if __name__ == "__main__":
    unittest.main()
