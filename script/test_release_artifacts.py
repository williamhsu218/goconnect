"""Regression checks for retention and destructive cleanup boundaries."""
from pathlib import Path
import os
import plistlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import release_artifacts as artifacts


class ArtifactBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="goconnect-artifacts-test-")
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.root = self.base / "project"
        self.root.mkdir()
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        self.trash = self.base / "trash"
        self.trash.mkdir()
        self.patcher = patch.object(artifacts, "trash_directory", return_value=self.trash)
        self.patcher.start()
        self.addCleanup(self.patcher.stop)
        self.current = self.archive("0.11.7", "44")
        self.rollback = self.archive("0.11.6", "43")
        artifacts.retain(self.current, self.rollback, self.root)

    def archive(self, version, build):
        archive = self.base / f"GoConnect-{version}-macos-arm64.zip"
        with zipfile.ZipFile(archive, "w") as stream:
            stream.writestr("GoConnect.app/Contents/Info.plist", plistlib.dumps({
                "CFBundleIdentifier": "com.willhsu.GoConnect",
                "CFBundleShortVersionString": version,
                "CFBundleVersion": build,
            }))
        return archive

    def test_pair_and_dry_run_preserve_release_and_source(self):
        (self.root / ".build").mkdir()
        (self.root / ".build/obsolete.zip").write_bytes(b"unused")
        source = self.root / "source.swift"
        source.write_text("// source")
        result = artifacts.clean(False, self.root)
        self.assertEqual(result["targets"], [".build"])
        self.assertTrue((self.root / ".build").exists())
        artifacts.clean(True, self.root)
        self.assertTrue((self.trash / ".build/obsolete.zip").exists())
        self.assertEqual(source.read_text(), "// source")
        self.assertEqual(artifacts.check(self.root)["current"]["build"], "44")

    def test_tracked_zip_and_external_symlink_target_are_preserved(self):
        tracked = self.root / "fixture.zip"
        tracked.write_bytes(b"source fixture")
        subprocess.run(["git", "add", "fixture.zip"], cwd=self.root, check=True)
        external = self.base / "external"
        external.mkdir()
        (external / "important.zip").write_bytes(b"external")
        (self.root / ".build").symlink_to(external, target_is_directory=True)
        artifacts.clean(True, self.root)
        self.assertEqual(tracked.read_bytes(), b"source fixture")
        self.assertEqual((external / "important.zip").read_bytes(), b"external")
        self.assertTrue((self.trash / ".build").is_symlink())

    def test_tracked_file_in_build_directory_blocks_cleanup(self):
        (self.root / ".build").mkdir()
        (self.root / ".build/important").write_text("keep")
        subprocess.run(["git", "add", ".build/important"], cwd=self.root, check=True)
        with self.assertRaisesRegex(ValueError, "version-controlled"):
            artifacts.clean(True, self.root)
        self.assertTrue((self.root / ".build/important").exists())

    def test_corrupt_retained_archive_blocks_cleanup(self):
        (self.root / ".build").mkdir()
        archive = next((self.root / "dist/current").glob("*.zip"))
        archive.write_bytes(b"corrupt")
        with self.assertRaises(zipfile.BadZipFile):
            artifacts.clean(True, self.root)
        self.assertTrue((self.root / ".build").exists())

    def test_running_generated_process_blocks_cleanup(self):
        (self.root / ".build").mkdir()
        original = subprocess.check_output

        def output(command, **options):
            if command[0] == "/bin/ps":
                return f"123 {self.root}/.build/GoConnect.app/Contents/MacOS/GoConnect\n"
            return original(command, **options)

        with patch.object(subprocess, "check_output", side_effect=output):
            with self.assertRaisesRegex(ValueError, "Running process"):
                artifacts.clean(True, self.root)
        self.assertTrue((self.root / ".build").exists())

    def test_invalid_rollback_does_not_change_retained_pair(self):
        before = (self.root / "dist/releases.json").read_bytes()
        with self.assertRaisesRegex(ValueError, "distinct"):
            artifacts.retain(self.current, self.current, self.root)
        with self.assertRaisesRegex(ValueError, "earlier"):
            artifacts.retain(self.rollback, self.current, self.root)
        self.assertEqual((self.root / "dist/releases.json").read_bytes(), before)

    def test_failed_replacement_restores_previous_pair(self):
        next_release = self.archive("0.11.8", "45")
        before = artifacts.check(self.root)
        original = Path.rename

        def rename(path, target):
            if path.name == "dist" and path.parent.name.startswith(".release-stage-"):
                raise OSError("simulated replacement failure")
            return original(path, target)

        with patch.object(Path, "rename", rename):
            with self.assertRaisesRegex(OSError, "simulated"):
                artifacts.retain(next_release, self.current, self.root)
        self.assertEqual(artifacts.check(self.root), before)
        self.assertEqual(list(self.root.glob(".release-stage-*")), [])


class BuildBoundaryTests(unittest.TestCase):
    script = Path(__file__).resolve().parent / "build_and_run.sh"

    def test_failed_tool_setup_removes_temporary_build(self):
        with tempfile.TemporaryDirectory(prefix="goconnect-build-check-") as temporary:
            base = Path(temporary)
            tools = base / "tools"
            tools.mkdir()
            xcrun = tools / "xcrun"
            xcrun.write_text("#!/bin/sh\nexit 37\n")
            xcrun.chmod(0o755)
            environment = dict(os.environ, PATH=str(tools) + ":" + os.environ["PATH"], TMPDIR=str(base))
            environment.pop("GOCONNECT_OUTPUT_DIR", None)
            result = subprocess.run([str(self.script), "--build-only"], env=environment, text=True, capture_output=True)
            self.assertEqual(result.returncode, 37, result.stderr)
            self.assertIn("Temporary build directory removed.", result.stdout)
            self.assertEqual(list(base.glob("goconnect-build.*")), [])

    def test_persistent_project_output_and_verify_gui_are_rejected(self):
        for arguments in (("--export", str(artifacts.ROOT / "dist/candidate")), ("--verify", "--run")):
            with self.subTest(arguments=arguments):
                result = subprocess.run([str(self.script), *arguments], text=True, capture_output=True)
                self.assertEqual(result.returncode, 2, result.stderr)
        result = subprocess.run(
            [str(self.script), "--build-only"],
            env=dict(os.environ, GOCONNECT_OUTPUT_DIR="/tmp/goconnect-legacy"),
            text=True, capture_output=True,
        )
        self.assertEqual(result.returncode, 2, result.stderr)


if __name__ == "__main__":
    unittest.main()
