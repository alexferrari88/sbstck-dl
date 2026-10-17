# Current work

Goal: triage issue #31, reproduce the reported post-data extraction error, and publish an actionable agent brief.

Status: triage complete. Issue #31 is open with bug, ready-for-agent, and priority:P1. Findings and the agent brief are posted at https://github.com/alexferrari88/sbstck-dl/issues/31#issuecomment-6238622910. No implementation or release was performed.

Verified cause: the reported publications serve post data in script#preloads-json[type="application/json"], loaded through JSON.parse(document.querySelector(...).textContent). The existing extractJSONString expects the older inline escaped-string form. Both layouts contain usable post JSON, but only the older container is supported.

Evidence: the SHA-256-verified v0.8.1 Linux release and current main (574a2ee63037be026bf5e98ecaeb1849a6e79bb9, Go 1.27.2/Linux) reproduce the exact error on https://letters.thedankoe.com/p/full-course-how-to-turn-your-interests. Current main also reproduces it on https://nateszerotoai.substack.com/p/ai-presentation-prompts without authentication. Both responses are HTTP 200 with valid post JSON. Captured-response and minimal-layout replays fail twice per post; the same payload in the legacy layout succeeds twice and preserves post ID, slug, and body HTML. Focused TestExtractJSONString and TestExtractorExtractPost tests pass but miss the new layout. Authenticated paid-body access and Windows execution were not tested. No reporter credential was used or retained in project files.

Scope for subsequent implementation: support both preload containers without double unescaping or evaluating JavaScript; preserve post/native-media metadata and subtitle/cover fallbacks; reject absent or malformed post data; cover actual CLI Markdown/archive output with deterministic fixtures and recheck the live public example. The posted brief is the implementation contract. Cookie redesign, access bypass, unrelated discovery/archive/media changes, and release publication are out of scope.

Next action: implement the posted brief when requested; triage itself has no outstanding blocker.
