# Current work

Goal: update the project's Go requirement, CI, release builds, and local toolchain to Go 1.27.2; fix the existing retry race, rerun validation, and commit/push the completed changes to main.

Status: Go upgrade and retry-race fix are implemented and validated. Go 1.27.2 is installed and active locally, with its official archive checksum verified and Go 1.26.3 retained. The module requirement, CI/release workflows, and build documentation are updated. All 452 tests and the full race suite passed; one existing optional scraped-HTML fixture test was skipped. Vet, module verification, workflow YAML/configuration checks, diff checks, and CGo-disabled Linux amd64, Windows amd64, and macOS amd64/arm64 builds passed. The Linux CLI help smoke check passed. Tests ran locally on Linux; Windows/macOS validation was cross-compilation. Earlier #22, #20, #14, #18, and #19 are complete.

Decisions: with --download-images, HEIC body images and covers receive PNG previews in adjacent .sbstck-previews/<content-hash>/ folders while originals remain intact. HTML/Markdown reference escaped local PNG paths; plain text still downloads requested media. Shared multiple-format processing fetches/converts once. Old cover originals and missing/corrupt previews recover locally when possible. Failed conversions warn and retain remote references without certifying stale previews; previous completed PNGs remain available. Cancellation prevents publication. Preserve incremental archives, discovery fallback, cover identity/collision protection, and per-format publication behavior.

Decoder: pinned gen2brain/heic v0.4.8 WASM/C bridge and adapted Go wrapper, running through wazero v1.6.0 without native tools or CGo. Decoder input/pixel/WASM memory limits and cancellation/deadline are documented. Exact upstream provenance, licenses, source, and build recipe are retained; future release builds package companion decoder-source archives. No release was triggered.

Toolchain decision: Go 1.27.2 is the source-build minimum and the version used by CI/release builds. Install the machine's toolchain in a versioned directory and retain the previous installation.

Retry decision: the original race also reproduces on unchanged HEAD 2de399f with Go 1.26.3. The new regression test reproduced races, mutated template state, and premature retry exhaustion before the fix, then passed five consecutive race-enabled runs after the fix. Each request now copies exponential/constant configuration and owns its retry state. Stateless stop/zero policies remain concurrent; opaque custom policies retain their implementation with serialized retry loops because BackOff has no clone operation. Dependency versions are unchanged.

Next action: no further implementation remains for this update. #23/#27 still need reporter evidence; #24 and #21 remain deferred.
