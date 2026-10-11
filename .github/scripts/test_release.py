import copy
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import release


class ReleaseGateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        for name in release.BINARIES:
            (self.directory / name).write_bytes(b"built binary")
        for name in release.ASSETS[len(release.BINARIES):]:
            with tarfile.open(self.directory / name, "w:gz") as archive:
                for required in release.SOURCE_FILES:
                    member = tarfile.TarInfo(required)
                    member.size = 6
                    archive.addfile(member, io.BytesIO(b"source"))
        self.digests = release.verify_local(self.directory)
        self.tag = "v0.9.0"
        self.commit = "a" * 40
        self.draft = {
            "tagName": self.tag, "isDraft": True, "targetCommitish": self.commit,
            "assets": [
                {"name": name, "state": "uploaded", "size": 6, "digest": digest}
                for name, digest in self.digests.items()
            ],
        }

    def fake_commands(self, *, uploaded=None, upload_error=False, published=False, tag_commit=None, missing_release=False, draft_commit=None):
        views = 0

        def command(*args):
            nonlocal views
            if args[:3] == ("git", "tag", "--list"):
                return self.tag if tag_commit else ""
            if args[:2] == ("git", "rev-parse"):
                return tag_commit
            if args[:3] == ("gh", "release", "view"):
                views += 1
                if missing_release and views == 1:
                    raise subprocess.CalledProcessError(1, args)
                info = copy.deepcopy(self.draft)
                if draft_commit:
                    info["targetCommitish"] = draft_commit
                if published:
                    info["isDraft"] = False
                if uploaded is not None and views > 1:
                    info = uploaded
                return json.dumps(info)
            if args[:3] == ("gh", "release", "upload") and upload_error:
                raise subprocess.CalledProcessError(1, args)
            return ""

        return patch.object(release, "run", side_effect=command)

    def assert_not_published(self, commands):
        self.assertFalse(any(call.args[:3] == ("gh", "release", "edit") for call in commands.call_args_list))

    def test_missing_platform_never_creates_release(self):
        (self.directory / release.BINARIES[2]).unlink()
        with patch.object(release, "run") as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            commands.assert_not_called()

    def test_empty_binary_rejected(self):
        (self.directory / release.BINARIES[0]).write_bytes(b"")
        with self.assertRaises(ValueError):
            release.verify_local(self.directory)

    def test_missing_source_notice_rejected(self):
        with tarfile.open(self.directory / release.ASSETS[3], "w:gz"):
            pass
        with self.assertRaises(KeyError):
            release.verify_local(self.directory)

    def test_build_only_does_not_mutate_github(self):
        with patch.object(release, "run") as commands:
            release.publish(self.directory, "", self.commit)
            commands.assert_not_called()

    def test_partial_upload_does_not_publish(self):
        uploaded = copy.deepcopy(self.draft)
        uploaded["assets"].pop()
        with self.fake_commands(uploaded=uploaded) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_corrupt_upload_does_not_publish(self):
        uploaded = copy.deepcopy(self.draft)
        uploaded["assets"][0]["digest"] = "sha256:wrong"
        with self.fake_commands(uploaded=uploaded) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_unexpected_asset_does_not_publish(self):
        uploaded = copy.deepcopy(self.draft)
        uploaded["assets"].append(dict(uploaded["assets"][0], name="stale-binary"))
        with self.fake_commands(uploaded=uploaded) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_upload_error_leaves_draft(self):
        with self.fake_commands(upload_error=True) as commands:
            with self.assertRaises(subprocess.CalledProcessError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_published_release_untouched(self):
        with self.fake_commands(published=True) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)
            self.assertFalse(any(call.args[:3] == ("gh", "release", "upload") for call in commands.call_args_list))

    def test_existing_tag_cannot_move(self):
        with self.fake_commands(tag_commit="b" * 40) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_draft_with_different_source_untouched(self):
        with self.fake_commands(draft_commit="b" * 40) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)
            self.assertFalse(any(call.args[:3] == ("gh", "release", "upload") for call in commands.call_args_list))

    def test_release_published_during_upload_stops_gate(self):
        uploaded = copy.deepcopy(self.draft)
        uploaded["isDraft"] = False
        with self.fake_commands(uploaded=uploaded) as commands:
            with self.assertRaises(ValueError):
                release.publish(self.directory, self.tag, self.commit)
            self.assert_not_published(commands)

    def test_publish_last_after_verified_draft_upload(self):
        with self.fake_commands(missing_release=True) as commands:
            release.publish(self.directory, self.tag, self.commit)
            calls = [call.args for call in commands.call_args_list]
            create = next(args for args in calls if args[:3] == ("gh", "release", "create"))
            self.assertIn("--draft", create)
            self.assertIn(self.commit, create)
            self.assertEqual(calls[-1][:3], ("gh", "release", "edit"))
            self.assertIn("--draft=false", calls[-1])
            self.assertEqual(sum(args[:3] == ("gh", "release", "edit") for args in calls), 1)

    def test_retry_existing_draft_preserves_notes(self):
        with self.fake_commands() as commands:
            release.publish(self.directory, self.tag, self.commit)
            self.assertFalse(any(call.args[:3] == ("gh", "release", "create") for call in commands.call_args_list))
            self.assertEqual(commands.call_args_list[-1].args[:3], ("gh", "release", "edit"))


if __name__ == "__main__":
    unittest.main()
