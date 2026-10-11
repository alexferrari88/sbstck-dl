# Bundled HEIC preview decoder

This directory adapts the MIT Go bridge from
[gen2brain/heic v0.4.8](https://github.com/gen2brain/heic/tree/97b4f060b3afd2ea7b8cf0ec75d3cfc129dc027f).
`lib/heif.wasm.gz`, `lib/heif.c`, and `lib/Makefile` are copied unchanged from that revision.
The bridge runs only the bundled WebAssembly through wazero v1.6.0; it never loads native libraries, needs no CGo, and retains the application's Go 1.20 minimum.
Unlike the upstream Go bridge, each invocation has an isolated runtime, cancellation closes that runtime, and all initialization failures are returned as errors.
The decoder runs one image at a time with a 30-second deadline, caps WASM linear memory at 512 MiB, compressed input at 64 MiB, and the primary image at 50 million pixels before pixel decoding. PNG encoding and cache validation run outside that decoder deadline.
These bounds limit resources; exceptionally large, unsupported, or damaged files retain their originals and use remote references.

## Notices and source

The Go bridge and C/WASM bridge/build recipe are MIT (see `LICENSE`).
The bundled module contains [libheif v1.18.2](https://github.com/strukturag/libheif/tree/v1.18.2) and [libde265 v1.0.15](https://github.com/strukturag/libde265/tree/v1.0.15), both LGPL-3.0 components. Their complete upstream license texts are in `lib/LICENSE.libheif` and `lib/LICENSE.libde265`.
Corresponding component source is available from those exact tags:

- https://github.com/strukturag/libheif/archive/refs/tags/v1.18.2.tar.gz
- https://github.com/strukturag/libde265/archive/refs/tags/v1.0.15.tar.gz

The retained Makefile contains all upstream source modifications and flags needed to reproduce the bridge. It fetches those exact versions and disables encoders, external codecs, plugin loading, and worker threads.
To rebuild or replace the LGPL components, install a WASI SDK as `/opt/wasi-sdk`, run `make` in this directory's `lib` folder, gzip the resulting `heif.wasm` into `heif.wasm.gz`, then run `go build` from the repository root. The application source is available in this repository, so a modified library can be relinked by replacing the embedded module and rebuilding. No restriction on modifying or reverse engineering the library for debugging applies.
End users running distributed application binaries do not need the WASI SDK or any converter.

Bundled module SHA-256: `3634a6447ea3915633667f39e01ff540a60f4e2f837a8391d044d76dfd8a0435`.

When redistributing a binary, preserve these notices and license texts alongside it and make the corresponding library/application source and build recipe available under their licenses.
