# Current work

Goal: triage the GitHub backlog and fix incremental archive index loss (#20).

Status: all nine issues have category/state/priority labels and triage notes. #23 and #27 are needs-info. #20 is implemented and validated: 306 tests, focused race tests, build, and vet pass; coordinator checks confirm the CLI and restoration regressions. #14 has a verified brief and implementation is underway. #18 is reproduced and awaits implementation. The coordinator owns GitHub updates and integration.

Decisions: prioritize archive integrity (#20), post discovery (#14), then offline cover images (#18). Preserve legacy entries with documented title/date/mtime fallbacks and compact per-format metadata; atomically replace posts/indexes without broadening existing permissions. For #14, use paginated public archive API fallback rather than treating recent RSS/HTML pages as complete. #23 and #27 need reporter evidence before implementation. Export and media enhancements remain deferred.

Next action: validate and integrate #20 to main with remote parity, then address #14 and #18 according to their verified briefs.
