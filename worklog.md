# Current work

Goal: ship #19, multiple output formats in one download execution. Completed.

Status: #19 is implemented and integrated on main alongside #20, #14, and #18; GitHub completion comments identify the shipped commits. Validation passed 423 repository tests, 70 focused race tests, build, vet, and diff checks. Coordinator independently checked archive/multiple-format behavior and final publication/ordering/cancellation cases. Windows amd64 and macOS arm64 command tests compile; runtime checks ran on Linux. All nine backlog issues have category/state/priority labels. #23 and #27 remain needs-info.

Decisions: accept comma-separated, space-separated, repeated, and short format flags; default HTML; normalize/deduplicate in requested order and validate before downloading. Fetch each post and prepare media once, then render separate formats and archives. Bulk runs fill only missing nonempty regular files; explicit single-post requests overwrite selected formats. Retain atomic writes, per-format metadata, source URL and text behavior; share cover proofs/assets and failed attempts across formats. A publication failure preserves the prior affected index and permits other formats to finish, with a nonzero exit. Preserve #20/#14/#18 behavior.

Next action: re-triage #23/#27 when reporters supply evidence. Deferred enhancement order: #22 (HEIC conversion), #24 (video), then #21 (EPUB/PDF). No further implementation is currently assigned.
