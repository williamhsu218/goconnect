#!/usr/bin/env python3
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from signing import resolve_configuration, signing_order


class SigningBoundaryTests(unittest.TestCase):
    def test_ordinary_build_does_not_touch_keychain(self):
        with patch("signing.output", side_effect=AssertionError("unexpected keychain access")):
            self.assertEqual(resolve_configuration({}).mode, "adhoc")

    def test_team_build_never_falls_back_to_adhoc(self):
        with patch("signing.output", return_value="0 valid identities found"):
            with self.assertRaises(ValueError):
                resolve_configuration({"GOCONNECT_REQUIRE_TEAM_SIGN": "1"})

    def test_multiple_certificates_require_exact_choice(self):
        identities = 'A' * 40 + ' "Apple Development: A (1234567890)"\n' + 'B' * 40 + ' "Developer ID Application: B (0987654321)"'
        with patch("signing.output", return_value=identities):
            with self.assertRaises(ValueError):
                resolve_configuration({"GOCONNECT_REQUIRE_TEAM_SIGN": "1"})

    def test_wrong_certificate_team_is_rejected_before_signing(self):
        identities = 'A' * 40 + ' "Apple Development: A (PERSONID01)"'
        with patch("signing.output", return_value=identities), patch("signing.subprocess.check_output", side_effect=[b'public cert', b'subject=CN=A,OU=REALTEAM01,O=A']):
            with self.assertRaises(ValueError):
                resolve_configuration({"GOCONNECT_SIGN_IDENTITY": "A" * 40, "GOCONNECT_SIGN_TEAM": "WRONGTEAM1"})

    def test_uses_certificate_ou_instead_of_cn_identifier(self):
        identities = 'A' * 40 + ' "Apple Development: A (PERSONID01)"'
        with patch("signing.output", return_value=identities), patch("signing.subprocess.check_output", side_effect=[b'public cert', b'subject=CN=A,OU=REALTEAM01,O=A']):
            configuration = resolve_configuration({"GOCONNECT_REQUIRE_TEAM_SIGN": "1"})
            self.assertEqual(configuration.team, "REALTEAM01")

    def test_nested_code_precedes_container_and_symlink_is_not_signed_twice(self):
        with tempfile.TemporaryDirectory() as folder:
            app = Path(folder) / "GoConnect.app"
            framework = app / "Contents/Frameworks/Sparkle.framework"
            xpc = framework / "Versions/B/XPCServices/Installer.xpc"
            binary = xpc / "Contents/MacOS/Installer"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(bytes.fromhex("cffaedfe") + b'fixture')
            (framework / "Versions/Current").symlink_to("B")
            order = signing_order(app)
            self.assertLess(order.index(binary), order.index(xpc))
            self.assertLess(order.index(xpc), order.index(framework))
            self.assertEqual(order[-1], app)
            self.assertEqual(len([p for p in order if p.name == "Installer"]), 1)


if __name__ == "__main__":
    unittest.main()
