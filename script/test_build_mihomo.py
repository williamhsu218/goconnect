#!/usr/bin/env python3
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from build_mihomo import load_lock


class MihomoPinTests(unittest.TestCase):
    def fixture(self, root):
        source = Path(__file__).parent
        shutil.copy2(source / "mihomo.lock.json", root / "mihomo.lock.json")
        shutil.copytree(source / "patches", root / "patches")
        return root / "mihomo.lock.json"

    def test_real_pinned_patch_matches_lock(self):
        lock, patch = load_lock(Path(__file__).with_name("mihomo.lock.json"))
        self.assertEqual(lock["buildVersion"], "v1.19.30-goconnect2")
        self.assertIn("goconnect-native-addresses", patch.read_text())
        self.assertIn("GOCONNECT-APP-CHILD", patch.read_text())
        self.assertIn("anchor apple generic", patch.read_text())

    def test_tampered_patch_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.fixture(Path(directory))
            lock = json.loads(path.read_text())
            with (path.parent / lock["patch"]).open("a") as stream:
                stream.write("\nchanged\n")
            with self.assertRaises(ValueError): load_lock(path)

    def test_source_or_build_identity_cannot_silently_change(self):
        for field, value in [("url", "https://example.invalid/source.tar.gz"), ("buildVersion", "stock"), ("tags", [])]:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as directory:
                path = self.fixture(Path(directory)); lock = json.loads(path.read_text())
                lock[field] = value; path.write_text(json.dumps(lock))
                with self.assertRaises(ValueError): load_lock(path)


if __name__ == "__main__":
    unittest.main()
