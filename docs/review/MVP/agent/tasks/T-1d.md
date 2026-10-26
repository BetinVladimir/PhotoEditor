---
id: T-1d
title: e2e helper - assert-bytes (SHA-256 and JPEG inspection)
area: tests
size: S
depends: [T-1a]
edit:
  - tests/e2e/helpers/assert-bytes.ts (new)
  - tests/e2e/helpers/assert-bytes.unit.test.ts (new)
context: []
verify:
  - cd tests/e2e && node --test helpers/assert-bytes.unit.test.ts
  - cd tests/e2e && npx tsc --noEmit
---
## Goal
Specs must verify final data (hash, ICC, orientation, size) of downloaded files. Provide dependency-free helpers and unit tests that run with plain Node.

## Spec (`helpers/assert-bytes.ts`, ES module, only `node:crypto`; erasable TypeScript only - no enums, no parameter properties - so that `node --test file.ts` works on Node 24)
- `export function sha256(data: Uint8Array): string` - lowercase hex.
- `export interface ImageInfo { format: 'jpeg'|'png'|'tiff'|'unknown'; width: number; height: number; bitsPerSample: number|null; hasIcc: boolean; orientation: number|null }`.
- `export function inspectJpeg(data: Uint8Array): ImageInfo` - walk markers from SOI (FFD8): skip standalone markers; for each segment read the big-endian length; SOF markers FFC0..FFCF except FFC4, FFC8, FFCC give `bitsPerSample` (byte 0), `height`, `width`; APP2 (FFE2) payload starting with ASCII `ICC_PROFILE\x00` sets `hasIcc`; APP1 (FFE1) payload starting with `Exif\x00\x00` contains a TIFF header (`II*\x00` or `MM\x00*`): read IFD0 and take tag 0x0112 (SHORT) as `orientation`. Stop at SOS (FFDA). Truncated or non-JPEG input -> `format: 'unknown'` (never throw).
- `export function expectBytesEqual(actual: Uint8Array, expectedSha256: string): void` - throws `Error('SHA mismatch: <a> != <b>')`.

## Tests (`helpers/assert-bytes.unit.test.ts`, `node:test` + `node:assert/strict`)
Build tiny JPEGs in the test from hand-made segments (SOI, optional APP1 Exif with orientation 6 in both byte orders, optional APP2 ICC, SOF0 8x4 baseline, EOI). Cases: dimensions; ICC present/absent; orientation 6 / absent; truncated input -> unknown; known SHA-256 of the string `abc` (`ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad`).

## Do not
- No third-party packages. Do not add the tests to the Playwright testMatch.
