# Current work

Goal: completed #22, browser-compatible previews of downloaded HEIC images while retaining original bytes.

Status: #22 implementation is complete and validated; earlier #20, #14, #18, and #19 are completed. All 447 repository tests passed, plus focused HEIC/media/cover/multiple-format race tests, vet, build, packaging, and diff checks. Coordinator independently verified actual CLI image sharing, original-byte preservation, moved archives, and offline browser rendering. HEIC library and CLI tests also passed on Go 1.20.14 with CGo disabled. Windows amd64 and macOS arm64 command tests compile; macOS amd64 builds with Go 1.20.14 and no CGo. Runtime verification ran on Linux.

Decisions: with --download-images, HEIC body images and covers receive PNG previews in adjacent .sbstck-previews/<content-hash>/ folders while originals remain intact. HTML/Markdown reference escaped local PNG paths; plain text still downloads requested media. Shared multiple-format processing fetches/converts once. Old cover originals and missing/corrupt previews recover locally when possible. Failed conversions warn and retain remote references without certifying stale previews; previous completed PNGs remain available. Cancellation prevents publication. Preserve incremental archives, discovery fallback, cover identity/collision protection, and per-format publication behavior.

Decoder: pinned gen2brain/heic v0.4.8 WASM/C bridge and adapted Go wrapper, running through wazero v1.6.0 without native tools or CGo; Go 1.20 minimum retained. Decoder input/pixel/WASM memory limits and cancellation/deadline are documented. Exact upstream provenance, licenses, source, and build recipe are retained; future release builds package companion decoder-source archives. No release was triggered.

Next action: no further implementation is authorized. #23/#27 still need reporter evidence; #24 and #21 remain deferred. Continue coordinating with gpt-6.1-sol at medium effort for implementation and gpt-6-luna for bounded supporting tasks when another issue is authorized.
