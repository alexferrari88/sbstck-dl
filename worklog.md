# Current work

Goal: publish v0.10.1 through the gated release workflow, including issue #32's completed opening EPUB contents page. Complete.

Release status: v0.10.1 published at 2026-10-28T14:36:41Z and independently verified as latest, non-draft, and non-prerelease: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.10.1. The public tag and release target resolve to source/version commit 61e7ede33a2f23cb55d735e317784b9283728306. Custom title and release notes were preserved.

Release validation: https://github.com/alexferrari88/sbstck-dl/actions/runs/40707120220 succeeded for source resolution, all three native Linux/macOS/Windows validation/build jobs, and final asset verification/publication. Every platform passed publication-gate tests, native application tests, vet, CLI/tag version matching, amd64 builds, and decoder-source packaging. All six uploaded draft assets were verified before gated publication. Local publication-gate tests also passed (14 tests).

Independent public-download checks: all three binaries and three decoder-source/license bundles match the exact public asset set, nonempty sizes, and metadata SHA-256 digests. Source notices match tagged-source content (Windows text line endings normalized for comparison); bundled WASM matches byte-for-byte. Each binary has its intended platform/amd64/source-revision stamp with an unmodified source tree. The downloaded Linux binary reports sbstck-dl v0.10.1 and exports a two-chapter EPUB with networking isolated. After moving the source archive, package inspection confirms the opening contents page, normal reading order, unique correct chapter destinations, and Unicode/XML-sensitive titles. The exported book passed EPUBCheck 5.4.0 with zero errors or warnings. Local release evidence: /tmp/sbstck-dl-v0.10.1-verify/ (public metadata, all six assets, build stamps, verification result, and released-opening.epub).

Spec: https://github.com/alexferrari88/sbstck-dl/issues/32#issuecomment-6471224386. The reporter confirmed existing reader navigation works on Windows; the requested addition is a visible contents page before the oldest chapter.

Implementation: reuse the existing EPUB navigation document as the first linear reading item and label it Table of contents. Existing chapter order, reader navigation, metadata, single-post exports, offline operation, and atomic publication behavior are preserved. README explains the default behavior, Calibre's contents shortcut, restored reading positions, and re-exporting older books.

Automated validation: final full `go test ./... -count=1` passed with EPUBCHECK_JAR set to the SHA-256-verified EPUBCheck 5.4.0 distribution pinned in CI. Focused EPUB race tests, vet, formatting, and diff checks passed. CLI regressions inspect actual package reading order, navigation semantics, unique and correct chapter destinations, single-post output, Unicode, and XML-sensitive titles. They failed on the baseline; a deliberate duplicated-destination mutation was also detected, then restored. The representative book passed EPUBCheck with zero errors or warnings.

Reader acceptance: Calibre 9.16 and KOReader desktop v2026.07.1 were run in distinct network namespaces without network routes, using a fresh export after the source archive was moved from its original location. Fresh profiles opened at the contents page; normal paging reached the oldest chapter next; the opening page's second-chapter link and each reader's contents-panel link opened the correct chapters. Screenshots were visually checked. This is Linux desktop-reader evidence, not a physical e-reader or new Windows acceptance test. Local evidence: /tmp/sbstck-issue32/acceptance/ (opening.epub under books; calibre-*.png and koreader-*.png captures).

Review: independent standards and spec reviews against starting commit 0860c89. Standards found no issues. Spec review identified a destination-identity coverage gap, fixed in ea2c78d and re-reviewed with zero outstanding findings. Runtime change is in 0ffe25d.

Next action: none. Release work is complete; all earlier published releases remain unchanged.
