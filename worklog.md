# Current work

Goal: publish v0.10.0 through the validated GitHub release workflow, including the completed offline EPUB export feature from issue #21. Complete.

Status: v0.10.0 published at 2026-10-17T20:08:43Z and verified as latest, non-draft, non-prerelease: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.10.0. Source and public tag resolve to 412c0f6238dd25ec046728103dd30c0a608ec739. Custom title and release notes were preserved.

Release validation: https://github.com/alexferrari88/sbstck-dl/actions/runs/39065267793 succeeded for source resolution, all three native Linux/macOS/Windows jobs, and final asset verification/publication. Every platform passed the publication-gate tests, native application tests, vet, CLI/tag version matching, amd64 build, and decoder-source packaging. The workflow verified all six draft asset names, sizes, SHA-256 digests, and source notices before publication.

Independent post-publication checks: downloaded all three binaries and three decoder-source/license bundles, verified the exact asset set, nonempty sizes, SHA-256 hashes against public GitHub metadata, source notices, and each binary's intended platform/amd64/source-revision stamp. The downloaded Linux binary reports sbstck-dl v0.10.0 and exports a two-chapter EPUB in a network namespace without networking. That book passed EPUBCheck 5.4.0 with zero errors/warnings. Public latest-release metadata and tag/source parity were verified. Local verification evidence: /tmp/sbstck-dl-v0.10.0-verify-05gwtz_s.

Agreed scope: EPUB first, PDF deferred. Export one existing local HTML archive offline as one book, with every eligible post as a chapter, a table of contents, and embedded reading images. Order oldest first, unknown dates last, filename ascending for ties. Require a book title; author/language are optional, unknown author is omitted, and language defaults to undetermined (`und`). Missing required posts/images or publication failures must fail clearly and preserve a previous EPUB. Source archives remain unchanged.

Media: preserve known web/source links; audio/video/attachments are not bundled. Local-only destinations without a known web URL become clear notes. Date filters and individual post selection are deferred.

Implementation: separate export-epub command; static chapter XHTML, navigation, packaged PNG/JPEG/GIF/WebP images, internal chapter/fragment links, explicit metadata, strict retained-post completeness, atomic publication, and source/output collision checks. Existing download formats remain HTML/Markdown/text. Required image formats that cannot be decoded fail clearly; originals and archive metadata/indexes are preserved.

Feature validation: the full local Go suite passed with EPUBCHECK_JAR set, as did focused EPUB race tests and vet. CLI/public-export regressions cover missing/corrupt/unreadable content, symlink/hardlink/output collisions, live cancellation preserving the previous book, chronological order, explicit metadata, offline resources, and legacy anchors. Fixture books and the representative book passed EPUBCheck 5.4.0 with zero errors/warnings.

Reader acceptance: KOReader Linux v2026.07.1 under Xvfb opened the final representative book after its source directory was moved. The reader ran in a distinct network namespace with no network routes. Embedded imagery, Unicode/layout, a link to chapter two's heading, and table-of-contents navigation were visually verified. This is desktop reader evidence; a physical e-reader was not used. Local evidence: /tmp/sbstck-epub-acceptance/representative-final.epub and koreader-final-{first,linked,toc}.png in the same directory.

Review: independent standards/spec reviews against b8e9145 found renderer/preflight classification, cross-chapter image links, and linked-media XHTML defects. All were fixed and covered by regressions, including transparent block/inline cards and plain link labels. Both reviewers confirmed zero outstanding findings on final implementation commit 98d8672.

Next action: none. Release work is complete. PDF export and direct URL-to-EPUB workflows remain deferred.
