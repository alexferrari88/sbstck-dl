# Current work

Goal: publish v0.10.1 through the gated release workflow, including issue #32's completed opening EPUB contents page.

Release status: preparing the version/source commit and custom-notes draft. Native Linux/macOS/Windows validation, uploaded-asset verification, publication, and independent public-download checks are pending. Use the exact full source SHA and matching v0.10.1 tag; publication must happen only through the final workflow gate.

Spec: https://github.com/alexferrari88/sbstck-dl/issues/32#issuecomment-6471224386. The reporter confirmed existing reader navigation works on Windows; the requested addition is a visible contents page before the oldest chapter.

Implementation: reuse the existing EPUB navigation document as the first linear reading item and label it Table of contents. Existing chapter order, reader navigation, metadata, single-post exports, offline operation, and atomic publication behavior are preserved. README explains the default behavior, Calibre's contents shortcut, restored reading positions, and re-exporting older books.

Automated validation: final full `go test ./... -count=1` passed with EPUBCHECK_JAR set to the SHA-256-verified EPUBCheck 5.4.0 distribution pinned in CI. Focused EPUB race tests, vet, formatting, and diff checks passed. CLI regressions inspect actual package reading order, navigation semantics, unique and correct chapter destinations, single-post output, Unicode, and XML-sensitive titles. They failed on the baseline; a deliberate duplicated-destination mutation was also detected, then restored. The representative book passed EPUBCheck with zero errors or warnings.

Reader acceptance: Calibre 9.16 and KOReader desktop v2026.07.1 were run in distinct network namespaces without network routes, using a fresh export after the source archive was moved from its original location. Fresh profiles opened at the contents page; normal paging reached the oldest chapter next; the opening page's second-chapter link and each reader's contents-panel link opened the correct chapters. Screenshots were visually checked. This is Linux desktop-reader evidence, not a physical e-reader or new Windows acceptance test. Local evidence: /tmp/sbstck-issue32/acceptance/ (opening.epub under books; calibre-*.png and koreader-*.png captures).

Review: independent standards and spec reviews against starting commit 0860c89. Standards found no issues. Spec review identified a destination-identity coverage gap, fixed in ea2c78d and re-reviewed with zero outstanding findings. Runtime change is in 0ffe25d.

Next action: validate the version and publication-gate tests locally, commit/push, create only an unpublished draft targeting the source SHA, and dispatch the native release workflow. Preserve all existing published releases.
