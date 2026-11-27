from pathlib import Path
import tempfile
import unittest

from preview_ui import ROOT, prepare


class PreviewPreparationTests(unittest.TestCase):
    def test_connection_action_keeps_signature_and_runs_the_fixture(self):
        control = Path("Sources/GoConnect/Stores/AppStoreConnectionControl.swift")
        declaration = next(line for line in (ROOT / control).read_text().splitlines()
                           if line.startswith("    func setConnectionEnabled("))
        with tempfile.TemporaryDirectory(prefix="goconnect-preview-test-") as temporary:
            source = prepare(Path(temporary))
            self.assertIn(
                f"{declaration}\n        setPreviewConnectionEnabled(enabled)\n    }}\n",
                (source / control).read_text(),
            )
            self.assertIn(
                "func setPreviewConnectionEnabled(_ enabled: Bool)",
                (source / "Sources/GoConnect/App/GoConnectApp.swift").read_text(),
            )


if __name__ == "__main__":
    unittest.main()
