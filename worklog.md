# Current work

Goal: triage the GitHub backlog and ship archive integrity (#20), post discovery (#14), and offline archive covers (#18).

Status: all nine issues have category/state/priority labels and triage notes. Archive integrity (#20), paginated discovery (#14), and local archive covers (#18) are implemented and integrated on main; GitHub completion comments identify the shipped commits. Final combined validation passed 377 tests, 110 focused race tests, build, vet, and diff checks. Windows amd64 and macOS arm64 command tests compile; runtime checks ran on Linux. Live #14 CLI discovery returned 103 unique public posts. #23 and #27 remain needs-info.

Decisions: prioritize archive integrity (#20), post discovery (#14), then offline cover images (#18). Preserve legacy entries with documented title/date/mtime fallbacks and compact per-format metadata; atomically replace posts/indexes without broadening existing permissions. For #14, use paginated public archive API fallback rather than treating recent RSS/HTML pages as complete. #23 and #27 need reporter evidence before implementation. Export and media enhancements remain deferred.

Next action: re-triage #23 and #27 when reporters supply evidence. Deferred enhancement order: #19 (multiple formats), #22 (HEIC conversion), #24 (video), then #21 (EPUB/PDF). No further implementation is currently assigned.
