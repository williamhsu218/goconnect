#!/usr/bin/env python3
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from prepare_release_sources import collect_patched_mihomo


class PatchedCoreSourceTests(unittest.TestCase):
    def fixture(self, directory):
        scripts = Path(__file__).parent
        component = directory / "app" / "mihomo-v1.19.30-goconnect1"
        component.mkdir(parents=True)
        shutil.copy2(scripts / "mihomo.lock.json", component / "mihomo.lock.json")
        shutil.copytree(scripts / "patches", component / "patches")
        shutil.copy2(scripts / "build_mihomo.py", component / "build_mihomo.py")
        lock = json.loads((component / "mihomo.lock.json").read_text())
        source = {"kind": "goconnect-patched-go", "url": lock["url"], "version": lock["buildVersion"],
                  "sha256": lock["sha256"], "patchSHA256": lock["patchSHA256"], "tags": lock["tags"]}
        (component / "source.json").write_text(json.dumps(source))
        return component

    def test_matching_source_recipe_and_nested_patch_are_retained(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); component = self.fixture(root); output = root / "sources"
            record = collect_patched_mihomo(component, output)
            copied = output / "components" / component.name
            self.assertEqual((copied / "patches/mihomo-1.19.30-goconnect.patch").read_bytes(),
                             (component / "patches/mihomo-1.19.30-goconnect.patch").read_bytes())
            self.assertTrue((copied / "build_mihomo.py").is_file())
            self.assertEqual(record["file"], str(Path("components") / component.name / "upstream.tar.gz"))

    def test_mismatched_provenance_rejected_before_collecting(self):
        for field in ["version", "sha256", "patchSHA256", "url", "kind"]:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as temp:
                root = Path(temp); component = self.fixture(root); output = root / "sources"
                path = component / "source.json"; source = json.loads(path.read_text())
                source[field] = "wrong"; path.write_text(json.dumps(source))
                with self.assertRaises(ValueError): collect_patched_mihomo(component, output)
                self.assertFalse(output.exists())

    def test_tampered_shipped_patch_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); component = self.fixture(root)
            (component / "patches/mihomo-1.19.30-goconnect.patch").write_text("wrong patch")
            with self.assertRaises(ValueError): collect_patched_mihomo(component, root / "sources")


if __name__ == "__main__":
    unittest.main()
