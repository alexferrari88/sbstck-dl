# Current work

Goal: finish triage of issue #21 and publish an agent-ready brief for EPUB export. Triage is complete; implementation was not part of this task.

Status: the brief is posted and GitHub verifies the issue is open with exactly enhancement / ready-for-agent / priority:P3. Authoritative brief: https://github.com/alexferrari88/sbstck-dl/issues/21#issuecomment-6239626967.

Agreed scope: EPUB first, PDF deferred. Export one existing local HTML archive offline as one book, with every eligible post as a chapter, a table of contents, and embedded reading images. Order oldest first, unknown dates last, filename ascending for ties. Require a book title; author/language are optional, unknown author is omitted, and language defaults to undetermined (`und`). Missing required posts/images or publication failures must fail clearly and preserve a previous EPUB. Source archives remain unchanged.

Media and acceptance: preserve known web/source links; audio/video/attachments are not bundled. Local-only media destinations with no known online URL become clear notes rather than broken links. KOReader is the reference reader alongside automated EPUB validation; actual reader verification remains an implementation acceptance check. Date filters and individual post selection are deferred.

Verified: current post conversion, CLI formats, archive restoration, and index generation support only HTML, Markdown, and text. No prior rejection was found. Archive metadata retains post titles/dates and cover references, but does not retain author, language, or canonical source URLs; legacy HTML may contain source links only if downloaded with that option. No EPUB implementation or reader check has been performed.

Decisions: the maintainer explicitly accepted the final selection, ordering, metadata, and link-handling defaults. The posted brief supersedes the earlier triage notes; no open scope decisions remain. EPUB and reader behavior have not been implemented or tested.

Next action: none for triage. Future implementation should follow the authoritative issue brief and report any unavailable KOReader acceptance check as pending.

Previous work: v0.9.0 publication is complete and verified: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.9.0. Native validation and asset publication succeeded in https://github.com/alexferrari88/sbstck-dl/actions/runs/39059798050.
