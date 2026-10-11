# Current work

Goal: publish v0.8.1 with the completed archive, discovery, multiple-format, HEIC-preview, and fetch-retry work since v0.7.

Status: v0.8.0 was published at 300235a, but its Windows native test job failed on two Unix-specific permission assertions and prevented all asset builds. Linux/macOS native tests passed. v0.8.1 corrects those assertions to compare actual permissions before and after the operation; no production behavior changes. Both corrected tests pass under the Linux race detector; vet and diff checks pass, and the CLI reports v0.8.1. Publication and native Windows validation are next.

Release scope: cumulative archive indexes and atomic publication; fallback discovery for publications without usable sitemaps; local archive covers; multiple output formats per download; bundled HEIC/HEIF PNG previews with retained originals and decoder-source/license assets; Go 1.27.2 and isolated concurrent retry state; archive workflow ownership refactor.

Decisions: preserve the published v0.8.0 tag and ship the validation fix as v0.8.1. Preserve the existing Linux, macOS, and Windows amd64 asset names and use the repository's release workflow for native tests, binaries, and decoder-source bundles. No unrelated feature or issue work is included. Existing #23/#27 await reporter evidence; #24/#21 remain deferred.

Validation: the full Linux race suite passed 463 checks, with the existing optional scraped-HTML fixture test skipped. Vet, module verification, and diff checks passed. CGo-disabled Linux, Windows amd64, and macOS amd64 builds passed; the Linux binary reports v0.8.0 and its download help exposes multiple formats. The decoder-source archive contains the bridge, embedded decoder, build recipe, notices, and module files. Windows/macOS runtime tests will run in the release workflow.

Next action: commit and push v0.8.1, publish its tagged GitHub release, and verify automation, downloadable assets, and remote refs. Mark v0.8.0 as superseded.
