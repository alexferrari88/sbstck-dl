"""Verify all release assets, upload to a draft, and publish only after verification."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile


BINARIES = ("sbstck-dl-ubuntu-amd64", "sbstck-dl-mac-amd64", "sbstck-dl-win-amd64.exe")
ASSETS = BINARIES + tuple(
    name.removesuffix(".exe") + "-decoder-source.tar.gz" for name in BINARIES
)
SOURCE_FILES = (
    "internal/heicpreview/README.md",
    "internal/heicpreview/LICENSE",
    "internal/heicpreview/lib/LICENSE.libheif",
    "internal/heicpreview/lib/LICENSE.libde265",
    "internal/heicpreview/lib/Makefile",
    "internal/heicpreview/lib/heif.c",
    "internal/heicpreview/lib/heif.wasm.gz",
    "go.mod", "go.sum", "LICENSE",
)


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def verify_local(directory):
    files = {path.name for path in directory.iterdir()}
    if files != set(ASSETS):
        raise ValueError(f"Expected six release assets; missing={set(ASSETS) - files}, extra={files - set(ASSETS)}")
    digests = {}
    for name in ASSETS:
        path = directory / name
        if not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"Missing or empty asset: {name}")
        digests[name] = "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
        if name.endswith(".tar.gz"):
            with tarfile.open(path) as archive:
                for required in SOURCE_FILES:
                    member = archive.getmember(required)
                    if not member.isfile() or member.size == 0:
                        raise ValueError(f"Missing or empty source/notice in {name}: {required}")
    return digests


def verify_uploaded(release, tag, digests):
    if release["tagName"] != tag or not release["isDraft"]:
        raise ValueError("Release must still be a draft with the requested tag")
    assets = release["assets"]
    if len(assets) != len(ASSETS) or {asset["name"] for asset in assets} != set(ASSETS):
        raise ValueError("Draft does not contain exactly the six required assets")
    for asset in assets:
        if asset["state"] != "uploaded" or asset["size"] <= 0 or asset["digest"] != digests[asset["name"]]:
            raise ValueError(f"Upload checksum/state mismatch: {asset['name']}")


def check_tag(tag, commit):
    # Refresh immediately before mutation; never move an existing tag.
    run("git", "fetch", "origin", "--tags")
    refs = run("git", "tag", "--list", tag)
    if refs and run("git", "rev-parse", f"refs/tags/{tag}^{{commit}}") != commit:
        raise ValueError("Existing tag points to a different commit")
    return bool(refs)


def release_info(tag):
    return json.loads(run("gh", "release", "view", tag, "--json", "tagName,isDraft,targetCommitish,assets"))


def publish(directory, tag, commit):
    digests = verify_local(directory)
    print("Verified all six local release assets", flush=True)
    if not tag:
        print("Build-only run: no tag or release created")
        return
    if not re.fullmatch(r"v\d+\.\d+\.\d+", tag) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Expected a vX.Y.Z tag and full commit SHA")
    has_tag = check_tag(tag, commit)
    try:
        release = release_info(tag)
    except subprocess.CalledProcessError:
        # A network/auth failure will also make creation fail. Never publish on error.
        run("gh", "release", "create", tag, "--draft", "--target", commit, "--title", tag, "--generate-notes")
        release = release_info(tag)
    if not release["isDraft"] or release["tagName"] != tag:
        raise ValueError("Refusing to modify an already published release")
    if not has_tag and release["targetCommitish"] != commit:
        raise ValueError("Existing draft targets a different source; use a fresh tag")
    run("gh", "release", "upload", tag, *(str(directory / name) for name in ASSETS), "--clobber")
    verify_uploaded(release_info(tag), tag, digests)
    check_tag(tag, commit)
    # This is the only publication operation, after every gate and upload.
    run("gh", "release", "edit", tag, "--draft=false", "--latest", "--target", commit)
    print(f"Published {tag} with all six verified assets")


if __name__ == "__main__":
    publish(Path(sys.argv[1]), os.environ.get("RELEASE_TAG", ""), os.environ.get("RELEASE_COMMIT", ""))
