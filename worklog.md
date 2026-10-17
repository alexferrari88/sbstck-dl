# Current work

Goal: publish v0.9.0 through the validated GitHub release workflow.

Status: preparing the release version and notes. Starting main is 5a800e5980ae8e5f2c982f7847f3bb868e51ec09. The release includes the issue #31 post-preload fix, native podcast/video metadata and downloads, failure-safe media handling, and offline archive index rebuilding added since v0.8.1.

Decisions: use v0.9.0 because the unreleased changes include new CLI capabilities. Freeze one full source SHA after updating cmd/version.go. Keep custom release notes in an unpublished draft targeting that SHA. Dispatch build-release.yml from main with that exact source and tag; publish only through its native Linux/macOS/Windows gates and six-asset checksum/source-notice verification. Existing published releases remain unchanged.

Prior validation: issue #31 implementation passed full Go tests, race checks, vet, Linux/Windows cross-builds, and separate standards/spec reviews. The fixed CLI downloaded/indexed the live public reproduction and anonymous paid-post preview. Authenticated full paid-body access remains unverified. These local checks do not replace native release gates.

Next action: verify and commit the version bump, push and prove remote parity, prepare the draft, dispatch the workflow, then verify successful publication, tag/source parity, and all six release assets.
