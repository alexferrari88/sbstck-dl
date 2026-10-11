# Current work

Goal: triage the GitHub backlog and ship archive integrity (#20), post discovery (#14), and offline archive covers (#18).

Status: all nine issues have category/state/priority labels and triage notes. #23 and #27 are needs-info. #20 is shipped and closed at eec1bea, with main/origin/remote parity verified. #14 is implemented and rebased onto #20; 85 combined focused tests, build, and vet pass, and the live CLI discovered 103 unique public posts. #18 is reproduced, has a verified brief, and implementation is underway in its own worktree. The coordinator owns GitHub updates and integration.

Decisions: prioritize archive integrity (#20), post discovery (#14), then offline cover images (#18). Preserve legacy entries with documented title/date/mtime fallbacks and compact per-format metadata; atomically replace posts/indexes without broadening existing permissions. For #14, use paginated public archive API fallback rather than treating recent RSS/HTML pages as complete. #23 and #27 need reporter evidence before implementation. Export and media enhancements remain deferred.

Next action: verify the rebased #14 checks and ship it, then validate and integrate #18 with final repository checks and remote parity.
