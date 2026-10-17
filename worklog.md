# Current work

Goal: finish triage of issue #21 and publish an agent-ready brief for EPUB export. Implementation is not part of this task.

Status: enhancement / needs-triage / priority:P3. Maintainer accepted EPUB first with PDF deferred, one book containing selected posts as chapters, a table of contents, and embedded images. The next round accepted offline export from an existing HTML archive, KOReader as the reference reader alongside automated EPUB validation, and failure on missing required posts/images while preserving any previous EPUB. Audio, video, and attachments remain links.

Verified: current post conversion, CLI formats, archive restoration, and index generation support only HTML, Markdown, and text. No prior rejection was found. Archive metadata retains post titles/dates and cover references, but does not retain author, language, or canonical source URLs; legacy HTML may contain source links only if downloaded with that option. No EPUB implementation or reader check has been performed.

Pending decisions: which local posts enter the book and their reading order; book title/author/language and treatment of unavailable source metadata. Capture the final scope, then post the brief and move #21 to ready-for-agent while retaining enhancement and priority:P3.

Next action: confirm the remaining selection and metadata defaults with the maintainer, then finish triage without starting implementation.

Previous work: v0.9.0 publication is complete and verified: https://github.com/alexferrari88/sbstck-dl/releases/tag/v0.9.0. Native validation and asset publication succeeded in https://github.com/alexferrari88/sbstck-dl/actions/runs/39059798050.
