import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import package_release as release


class ReleaseTests(unittest.TestCase):
    def test_semver_tags(self):
        for tag, prerelease in [("v0.1.0", False), ("v1.2.3+build.4", False), ("v1.2.3-rc.1", True), ("v1.2.3-beta+build", True)]:
            self.assertEqual(release.validate_tag(tag), prerelease)
        for tag in ["1.2.3", "v1", "v01.2.3", "v1.2.3-01", "v1.2.3-rc.01", "v1.2.3-", "v1.2.3\n", "v1.2.3;echo x"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.validate_tag(tag)

    def test_portable_deterministic_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            first, second = Path(tmp) / "a.tar.gz", Path(tmp) / "b.tar.gz"
            payload = {"LICENSE": b"license", "examples/base/kong.yaml": b"services: []\n"}
            release.write_archive(first, b"binary", payload)
            release.write_archive(second, b"binary", payload)
            self.assertEqual(first.read_bytes(), second.read_bytes())
            with tarfile.open(first) as archive:
                self.assertEqual(archive.getnames(), ["kongweave", "LICENSE", "examples/base/kong.yaml"])
                self.assertEqual(archive.getmember("kongweave").mode, 0o755)
                for member in archive:
                    self.assertEqual((member.uid, member.gid, member.mtime), (0, 0, 0))
                    self.assertEqual((member.uname, member.gname), ("", ""))
                self.assertEqual(archive.extractfile("examples/base/kong.yaml").read(), payload["examples/base/kong.yaml"])

    def assets(self, directory):
        checksums = []
        for target in release.TARGETS:
            name = f"kongweave_{target}.tar.gz"
            data = target.encode()
            (directory / name).write_bytes(data)
            checksums.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
        (directory / "checksums.txt").write_text("".join(checksums))

    def test_incomplete_or_corrupt_assets_fail(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self.assets(directory)
            release.verify_artifacts(directory)
            (directory / "kongweave_linux_arm64.tar.gz").write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                release.verify_artifacts(directory)
            self.assets(directory)
            checksums = directory / "checksums.txt"
            checksums.write_text(checksums.read_text().splitlines()[0] + "\n")
            with self.assertRaisesRegex(ValueError, "every archive"):
                release.verify_artifacts(directory)
            self.assets(directory)
            (directory / "extra.txt").write_text("unexpected")
            with self.assertRaisesRegex(ValueError, "exactly four"):
                release.verify_artifacts(directory)

    def test_failed_build_does_not_publish_or_overwrite(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(release, "git", return_value="commit"), patch.object(release, "payload_files", return_value={"LICENSE": b"license"}):
            output = Path(tmp) / "release"
            with patch.object(release.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "go")):
                with self.assertRaises(subprocess.CalledProcessError):
                    release.package("v1.2.3", output)
            self.assertEqual(list(Path(tmp).iterdir()), [])
            output.mkdir()
            (output / "keep").write_text("original")
            with self.assertRaisesRegex(ValueError, "already exists"):
                release.package("v1.2.3", output)
            self.assertEqual((output / "keep").read_text(), "original")

    def test_publish_is_draft_first_and_stops_on_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            assets = work / "assets"
            assets.mkdir()
            self.assets(assets)
            log = work / "gh.jsonl"
            gh = work / "gh"
            gh.write_text("#!/usr/bin/env python3\nimport json,os,sys\nwith open(os.environ['GH_TEST_LOG'],'a') as f: f.write(json.dumps(sys.argv[1:])+'\\n')\nif os.environ.get('GH_TEST_FAIL') == '1' and sys.argv[1:3] == ['release','create']: sys.exit(1)\n")
            gh.chmod(0o755)
            env = dict(os.environ, PATH=str(work) + os.pathsep + os.environ["PATH"], GH_REPO="example/kongweave", RELEASE_TAG="v1.2.3-rc.1", GH_TEST_LOG=str(log))
            command = ["bash", str(release.ROOT / "scripts/publish-release.sh"), str(assets)]
            subprocess.run(command, env=env, check=True, capture_output=True)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            self.assertEqual([call[:2] for call in calls], [["release", "create"], ["release", "edit"]])
            self.assertIn("--draft", calls[0])
            self.assertIn("--verify-tag", calls[0])
            self.assertIn("--prerelease", calls[0])
            self.assertIn("--draft=false", calls[1])
            log.unlink()
            env.update(RELEASE_TAG="v1.2.3", GH_TEST_FAIL="1")
            result = subprocess.run(command, env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            self.assertEqual(len(calls), 1)
            self.assertNotIn("--prerelease", calls[0])
            log.unlink()
            (assets / "kongweave_linux_amd64.tar.gz").write_text("corrupt")
            self.assertNotEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)
            self.assertFalse(log.exists())

    def test_all_actions_are_pinned_to_full_commit_shas(self):
        for workflow in (release.ROOT / ".github/workflows").glob("*.yml"):
            for line in workflow.read_text().splitlines():
                match = re.search(r"\buses:\s+(\S+)", line)
                if match:
                    self.assertRegex(match[1], r"^[\w.-]+/[\w./-]+@[0-9a-f]{40}$", str(workflow))


if __name__ == "__main__":
    unittest.main()
