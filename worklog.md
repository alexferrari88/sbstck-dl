# Current work

Goal: publish v0.9.0 through the validated GitHub release workflow.

Status: release published and verified at 2026-10-17T18:51:55Z: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.9.0. It is the latest, non-draft, non-prerelease release. Source and tag resolve to d96bdc9e0cc51126ad7a35e4ea4ed65d7098edae.

Contents: the issue #31 post-preload extraction fix; native podcast/video metadata and downloads; failure-safe media handling; and offline archive index rebuilding added since v0.8.1. Custom release notes describe these changes and the amd64 downloads for Linux, macOS, and Windows.

Native validation: https://github.com/alexferrari88/sbstck-dl/actions/runs/39059798050 succeeded for source resolution, all three platform jobs, and asset verification/publication. Each platform passed publication-gate tests, native Go tests, vet, CLI/tag version matching, binary builds, and decoder-source packaging. Publication occurred only after all six draft asset names, sizes, checksums, and source notices were verified.

Independent post-publication checks: downloaded all three binaries and three decoder-source/license bundles; confirmed their exact asset set, nonempty sizes, source notices, and SHA-256 hashes against GitHub metadata. Verified the public tag matches the validated source and the latest release is v0.9.0. The downloaded Linux binary reports sbstck-dl v0.9.0 and successfully downloads/indexes the previously failing public post as Markdown (40,967 bytes). Authenticated full paid-body access was not tested.

Release-gate correction: the first attempt (39059528426) correctly blocked publication because a media-integrity test assumed Unix 0600 permissions on Windows. The assertion now compares the actual file mode before and after embedding on each platform; no runtime behavior changed. Focused normal/race checks passed, and the fresh native run above passed on all three platforms. No published release was modified.

Next action: none. Release work is complete.
