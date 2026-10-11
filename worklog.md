# Current work

Goal: publish v0.8.1 with the completed archive, discovery, multiple-format, HEIC-preview, and fetch-retry work since v0.7.

Status: v0.8.1 is published and is GitHub's latest release: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.8.1. The immutable tag points to 16c5f85855c07636a6fa2d6acf9cef92a07c1830. Native tests, builds, and uploads passed on Linux, macOS, and Windows in workflow run 38122985346. All six downloadable assets were verified against GitHub's SHA-256 digests; the downloaded Linux binary reports v0.8.1. Release code and tests were pushed to main; this handoff update is documentation only.

Release scope: cumulative archive indexes and atomic publication; fallback discovery for publications without usable sitemaps; local archive covers; multiple output formats per download; bundled HEIC/HEIF PNG previews with retained originals and decoder-source/license assets; Go 1.27.2 and isolated concurrent retry state; archive workflow ownership refactor.

Decisions: v0.8.0's Windows job exposed two Unix-specific permission assertions, blocking all asset builds. Preserve that published tag and ship the test fix as v0.8.1; the v0.8.0 release page now directs users to v0.8.1. Tests compare actual permissions before and after the operation. Existing Linux, macOS, and Windows amd64 asset names are preserved. No unrelated feature or issue work is included. Existing #23/#27 await reporter evidence; #24/#21 remain deferred.

Validation: the v0.8.0 candidate passed 463 Linux race-suite checks, with the existing optional scraped-HTML fixture test skipped, plus vet, module verification, diff checks, and CGo-disabled Linux/Windows/macOS builds. Both corrected HEIC tests passed under the Linux race detector before v0.8.1 publication, and vet/diff checks passed again. The v0.8.1 workflow passed all three native suites and built the three binaries plus companion decoder-source archives. Downloaded source bundles contain the bridge, embedded decoder, build recipe, notices, and module files. Local/remote release-tag parity was verified; the source tag is preserved while main can advance for this documentation update.

Next action: none for this release. Existing issue evidence requests and deferred work remain unchanged.
