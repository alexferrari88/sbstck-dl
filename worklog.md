# Current work

Goal: implement issue #21's accepted offline EPUB export brief. PDF remains deferred.

Status: exporter, CLI, focused regression tests, user documentation, and a pinned EPUBCheck CI job are implemented in the isolated feat/issue-21-epub worktree, based on b8e9145. Authoritative brief: https://github.com/alexferrari88/sbstck-dl/issues/21#issuecomment-6239626967. Issue assigned to the maintainer; final review and suite remain.

Agreed scope: EPUB first, PDF deferred. Export one existing local HTML archive offline as one book, with every eligible post as a chapter, a table of contents, and embedded reading images. Order oldest first, unknown dates last, filename ascending for ties. Require a book title; author/language are optional, unknown author is omitted, and language defaults to undetermined (`und`). Missing required posts/images or publication failures must fail clearly and preserve a previous EPUB. Source archives remain unchanged.

Media and acceptance: preserve known web/source links; audio/video/attachments are not bundled. Local-only media destinations with no known online URL become clear notes rather than broken links. KOReader is the reference reader alongside automated EPUB validation; actual reader verification remains an implementation acceptance check. Date filters and individual post selection are deferred.

Implementation: separate export-epub command; static chapter XHTML, navigation, packaged PNG/JPEG/GIF/WebP images, internal chapter/fragment links, explicit metadata, strict retained-post completeness, atomic publication, and source/output collision checks. Existing download formats remain HTML/Markdown/text. Required image formats that cannot be decoded fail clearly; originals and archive metadata/indexes are preserved.

Verified so far: focused CLI and public exporter tests pass, including missing/corrupt/unreadable content, symlink/hardlink/output collisions, live cancellation with previous-book preservation, chronological order, metadata, offline resources, and legacy anchors. EPUBCheck 5.4.0 reports zero errors/warnings on representative generated books. Its downloaded ZIP matches the official asset's SHA-256. A moved-source representative book opens in KOReader Linux v2026.07.1: embedded image, Unicode/layout, linked second-chapter heading, and table-of-contents navigation were visually checked under Xvfb. This is desktop reader evidence, not a physical e-reader test.

Validation seams: the real offline CLI and resulting EPUB archive; the public export operation for cancellation and source/output preservation. Use vertical TDD slices, then EPUBCheck and a KOReader desktop check if available. Review the completed diff against starting commit b8e9145 on standards and spec axes before integration.

Next action: implement and test the exporter, validate and review it, then commit, fast-forward main, push, verify parity, and remove the completed worktree/branch. Report actual reader evidence separately from automated validation.

Previous work: v0.9.0 publication is complete and verified: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.9.0. Native validation and asset publication succeeded in https://github.com/alexferrari88/sbstck-dl/actions/runs/39059798050.
