# Current work

Goal: validate and build all three platforms before publishing any release, preventing public releases without binaries.

Status: completed and pushed. Live build-only workflow run 38123975023 passed all native Linux, macOS, and Windows tests, vet, builds, artifact uploads/downloads, and the complete six-asset check for commit 7b4e5adb773d531d208160af860eef5d817ef5f7. The final job explicitly reported no tag or release created. v0.8.1 remains the latest release.

Scope: replace the post-publication release event with a manual validation/build/publish workflow; keep build-only dispatches; verify the complete six-asset set before and after draft uploads; publish only after successful native tests and builds. Document the release command and add failure-path checks for the publication gate.

Decisions: resolve the requested source once to a commit SHA and use that SHA in every job. Serialize releases for the same tag. Preserve asset names, existing published tags/releases, and custom draft notes. Failed tests/builds create no release; upload/verification failures leave a draft. A nonempty release tag must match the CLI version. Blank tag performs validation only.

Validation: 14 publication-gate tests pass locally and on all three native platforms, including missing/empty assets, partial/corrupt uploads, upload errors, mismatched source tags/drafts, published releases, draft retries, and publish-last ordering. actionlint and shellcheck pass for both workflows after updating official GitHub actions to supported Node 24 releases. Live run 38123975023 passed the complete build-only path and retained three platform artifacts, each with binary/source bundle. Publication/failure ordering was tested with mocked GitHub commands; no new release was published to test the gate. An initial dispatch with an abbreviated SHA failed source checkout and correctly skipped all downstream jobs; the successful run used the full SHA.

Next action: none for this change. Future releases use the documented workflow with a matching tag; failed drafts can be retried against the same commit. Existing issue evidence requests and deferred work remain unchanged.
