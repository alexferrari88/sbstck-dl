# Current work

Goal: deepen archive workflow ownership while preserving incremental downloads, cover reconciliation, and independent publication by output format.

Status: implementation and outcome tests are complete. The coordinator reviewed the combined changes and an independent review found no material regressions. All required local validation gates passed.

Design: ArchiveWorkflow restores all selected archives before downloads, records each post's media evidence and attempted write outcomes, and publishes through the existing Archive/ImageDownloader implementation. Archive state is private to the workflow. CLI retains discovery/extraction, media preparation, post writes, incremental inventory, and terminal output. Existing exported entry points remain compatible; inventory, export, and decoder redesign are outside scope. Domain vocabulary is recorded in GLOSSARY.md.

Preserved rules: keep evidence from failed media/write attempts; only successful writes prefer current body media. Reconcile covers across formats using source identity, retain previous previews after failed writes, and recover missing/corrupt previews locally when possible. Ordinary cover failures warn and retain remote references; cancellation stops subsequent publication. Save metadata before each format's index and continue other formats after a publication failure. Each file remains atomically published; formats do not form one transaction. The pinned HEIC decoder and request-local retry state remain unchanged.

Validation: 463 checks passed, with the existing optional scraped-HTML fixture test skipped. Seven new public-interface workflow scenarios cover restoration, mixed write outcomes, unequal archive sizes and retained cover reuse, publication failure isolation, cancellation, conflicting media with no writes, and missing output directories. Vet, formatting, diff checks, and CGo-disabled Windows amd64/macOS amd64/arm64 builds passed. The full race suite also passed all 463 checks, with the same optional fixture skip. Runtime tests run on Linux; Windows/macOS validation is cross-compilation.

Next action: none for this refactor. No additional feature work is included. Existing #23/#27 await reporter evidence; #24/#21 remain deferred.
