# Current work

Goal: publish v0.9.0 through the validated GitHub release workflow.

Status: version bump committed and pushed with verified main/origin/remote parity at 29f1a0916b02d9b96609857a2e8351335e960e5d. The v0.9.0 draft targets that exact source SHA and has custom release notes. First workflow https://github.com/alexferrari88/sbstck-dl/actions/runs/39059528426 passed native Linux/macOS validation but Windows failed an existing permission assertion; publication was skipped. The draft remains unpublished with no assets or tag. The test expected Unix 0600 although Windows reports 0666 for a writable file. Update the regression to compare the actual mode before and after embedding, retaining the preservation assertion on every OS; no runtime change is needed. Publication is still pending. The release includes the issue #31 post-preload fix, native podcast/video metadata and downloads, failure-safe media handling, and offline archive index rebuilding added since v0.8.1.

Decisions: use v0.9.0 because the unreleased changes include new CLI capabilities. Freeze one full source SHA after updating cmd/version.go. Keep custom release notes in an unpublished draft targeting that SHA. Dispatch build-release.yml from main with that exact source and tag; publish only through its native Linux/macOS/Windows gates and six-asset checksum/source-notice verification. Existing published releases remain unchanged.

Prior validation: issue #31 implementation passed full Go tests, race checks, vet, Linux/Windows cross-builds, and separate standards/spec reviews. The fixed CLI downloaded/indexed the live public reproduction and anonymous paid-post preview. Authenticated full paid-body access remains unverified. These local checks do not replace native release gates.

Next action: validate and commit the portable permission assertion, push it, retarget the unpublished draft to the new exact source SHA, and rerun all native platform gates. Then verify successful publication, tag/source parity, all six release assets, and the released Linux binary against the public reproduction; push the completed release handoff.
