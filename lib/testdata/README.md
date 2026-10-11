`preview.heic` is `testdata/test_exif.heic` from
https://github.com/gen2brain/heic/tree/bb64247bd334c327aded704a94e8fb41ee0962a7, under the upstream MIT license (`LICENSE.heic`).
SHA-256: `49d881a7a87d91cdf79d4599a3f7ecf331fa9d940743954856dc1c66fdd2eae3`.
The bundled WASM decoder renders this real HEVC/HEIF file as a 480 x 640 image.
It exercises container metadata and orientation without a native converter.
