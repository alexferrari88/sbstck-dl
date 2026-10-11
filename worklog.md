# Current work

Goal: validate and build all three platforms before publishing any release, preventing public releases without binaries.

Status: implementation complete; local checks passed. v0.8.1 remains the verified latest release; no new application release is requested. Live build-only validation is next.

Scope: replace the post-publication release event with a manual validation/build/publish workflow; keep build-only dispatches; verify the complete six-asset set before and after draft uploads; publish only after successful native tests and builds. Document the release command and add failure-path checks for the publication gate.

Decisions: resolve the requested source once to a commit SHA and use that SHA in every job. Serialize releases for the same tag. Preserve asset names, existing published tags/releases, and custom draft notes. Failed tests/builds create no release; upload/verification failures leave a draft. A nonempty release tag must match the CLI version. Blank tag performs validation only.

Validation: 14 publication-gate tests pass, including missing/empty assets, partial/corrupt uploads, upload errors, mismatched source tags/drafts, published releases, draft retries, and publish-last ordering. The gate verifies all six real v0.8.1 downloads in build-only mode. actionlint and shellcheck pass for both workflows after updating the official GitHub actions to supported Node 24 releases. Live three-platform build-only verification is pending.

Next action: push the completed workflow, run it with an empty tag, and verify all native jobs and artifacts without creating a release.
