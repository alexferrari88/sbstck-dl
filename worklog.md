# Current work

Goal: publish v0.8.0 with the completed archive, discovery, multiple-format, HEIC-preview, and fetch-retry work since v0.7.

Status: release candidate validated on main, initially verified against origin/main. CLI version is v0.8.0. GitHub publication and native build verification are next.

Release scope: cumulative archive indexes and atomic publication; fallback discovery for publications without usable sitemaps; local archive covers; multiple output formats per download; bundled HEIC/HEIF PNG previews with retained originals and decoder-source/license assets; Go 1.27.2 and isolated concurrent retry state; archive workflow ownership refactor.

Decisions: use v0.8.0 for the additive feature release. Preserve the existing Linux, macOS, and Windows amd64 asset names and use the repository's release workflow for native tests, binaries, and decoder-source bundles. No unrelated feature or issue work is included. Existing #23/#27 await reporter evidence; #24/#21 remain deferred.

Validation: the full Linux race suite passed 463 checks, with the existing optional scraped-HTML fixture test skipped. Vet, module verification, and diff checks passed. CGo-disabled Linux, Windows amd64, and macOS amd64 builds passed; the Linux binary reports v0.8.0 and its download help exposes multiple formats. The decoder-source archive contains the bridge, embedded decoder, build recipe, notices, and module files. Windows/macOS runtime tests will run in the release workflow.

Next action: commit and push the release candidate, publish the tagged GitHub release, and verify automation, downloadable assets, and remote refs.
