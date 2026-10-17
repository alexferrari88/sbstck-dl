# Current work

Goal: publish v0.10.0 through the validated GitHub release workflow, including the completed offline EPUB export feature from issue #21.

Status: preparing the v0.10.0 version commit and custom release notes. The previous latest published release is v0.9.0. Native release validation, draft asset verification, publication, and independent post-publication checks remain.

Agreed scope: EPUB first, PDF deferred. Export one existing local HTML archive offline as one book, with every eligible post as a chapter, a table of contents, and embedded reading images. Order oldest first, unknown dates last, filename ascending for ties. Require a book title; author/language are optional, unknown author is omitted, and language defaults to undetermined (`und`). Missing required posts/images or publication failures must fail clearly and preserve a previous EPUB. Source archives remain unchanged.

Media: preserve known web/source links; audio/video/attachments are not bundled. Local-only destinations without a known web URL become clear notes. Date filters and individual post selection are deferred.

Implementation: separate export-epub command; static chapter XHTML, navigation, packaged PNG/JPEG/GIF/WebP images, internal chapter/fragment links, explicit metadata, strict retained-post completeness, atomic publication, and source/output collision checks. Existing download formats remain HTML/Markdown/text. Required image formats that cannot be decoded fail clearly; originals and archive metadata/indexes are preserved.

Validation: the final full Go suite passed with EPUBCHECK_JAR set. Focused EPUB race tests and go vet passed. Linux, macOS, and Windows amd64 builds passed with CGo disabled; the latter two are cross-builds, not native runtime tests. Focused CLI/public-export tests cover missing/corrupt/unreadable content, symlink/hardlink/output collisions, live cancellation preserving the previous book, chronological order, explicit metadata, offline resources, and legacy anchors. EPUBCheck 5.4.0 reports zero errors/warnings on fixture books and the final representative book; its ZIP matches the official asset SHA-256.

Reader acceptance: KOReader Linux v2026.07.1 under Xvfb opened the final representative book after its source directory was moved. The reader ran in a distinct network namespace with no network routes. Embedded imagery, Unicode/layout, a link to chapter two's heading, and table-of-contents navigation were visually verified. This is desktop reader evidence; a physical e-reader was not used. Local evidence: /tmp/sbstck-epub-acceptance/representative-final.epub and koreader-final-{first,linked,toc}.png in the same directory.

Review: independent standards/spec reviews against b8e9145 found renderer/preflight classification, cross-chapter image links, and linked-media XHTML defects. All were fixed and covered by regressions, including transparent block/inline cards and plain link labels. Both reviewers confirmed zero outstanding findings on final implementation commit 98d8672.

Next action: commit/push the release version, prepare an unpublished draft targeting that exact full source SHA, dispatch build-release.yml for v0.10.0, monitor the three native validation/build jobs and publication gate, then independently verify release state, tag/source parity, all six assets, and the released Linux CLI.
