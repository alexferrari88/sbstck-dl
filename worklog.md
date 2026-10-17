# Current work

Goal: implement issue #31, restoring post extraction for Substack's JSON-script preload layout while retaining legacy support.

Status: implementation, validation, and the two-axis review are complete. Code commit: f6b0dc7310e4e8fdaf3c7ddf845210946002006f. The contract and original reproduction are https://github.com/alexferrari88/sbstck-dl/issues/31#issuecomment-6238622910. No release was published.

Changes: JSON-script preloads are decoded directly; legacy inline strings are unescaped once. Legacy parsing selects the window._preloads assignment rather than unrelated analytics or later JSON.parse calls. Missing, null, empty, and malformed post data return errors. Existing post/native-media metadata and subtitle/cover fallback behavior are preserved.

Regression evidence: the public Extractor.ExtractPost fixture and actual CLI Markdown/archive tests fail against baseline 63a4a724ab898ff48dfde971d5f7e55dbdc1cce4 and pass with the fix. Fixtures cover both layouts, quotes, backslashes, literal escapes, line breaks, Unicode, analytics, missing/malformed data, native-media metadata, and HTML metadata fallbacks. Invalid CLI inputs cannot publish a post or index.

Validation: go test ./... -count=1 passes (cmd 21.276s, lib 40.612s); go test -race ./... -count=1 passes (cmd 21.984s, lib 96.488s). go vet ./..., Linux amd64 and Windows amd64 builds with CGO disabled, formatting, and diff checks pass. Standards review: no findings. Spec/correctness review: no findings.

Live evidence: the fixed CLI successfully downloads and indexes https://letters.thedankoe.com/p/full-course-how-to-turn-your-interests as Markdown (40,967 bytes). The anonymous preview at https://nateszerotoai.substack.com/p/ai-presentation-prompts also downloads and indexes (1,798 bytes). These are separate from deterministic fixture checks. Authenticated full paid-body access and native Windows execution remain unverified. No reporter credential was used or retained in project files.

Next action: none for this implementation. A new binary release is a separate task following docs/agents/releases.md; v0.8.1 does not contain this fix.
