---
id: T-1dt
title: e2e helper - assert-bytes TIFF/PNG inspection
area: tests
size: S
depends: [T-1d]
edit:
  - tests/e2e/helpers/assert-bytes-tiff.ts (new)
  - tests/e2e/helpers/assert-bytes-tiff.unit.test.ts (new)
context:
  - tests/e2e/helpers/assert-bytes.ts
verify:
  - cd tests/e2e && node --test helpers/assert-bytes-tiff.unit.test.ts
  - cd tests/e2e && npx tsc --noEmit
---
## Goal
E-11 must prove that a TIFF16 export has 16 bits per sample, the requested size and an embedded ICC profile.

## Spec (`helpers/assert-bytes-tiff.ts`; import `ImageInfo` from `./assert-bytes`)
- `export function inspectTiff(data: Uint8Array): ImageInfo` - header `II*\x00` or `MM\x00*`, first IFD offset (uint32), then read the IFD0 entries (12 bytes each: tag u16, type u16, count u32, value/offset). Tags: 256 ImageWidth, 257 ImageLength (SHORT or LONG), 258 BitsPerSample (SHORT; with count > 1 the value is an offset - read the FIRST value), 274 Orientation, 34675 (0x8773) ICC profile (presence only). Honour the byte order for every read. Return `format: 'tiff'`; invalid -> `format: 'unknown'`, never throw.
- `export function inspectPng(data: Uint8Array): ImageInfo` - 8-byte signature, IHDR (width, height, bit depth as `bitsPerSample`), `hasIcc` if an `iCCP` chunk precedes IDAT; `orientation: null`.

## Tests
Hand-built minimal TIFFs in both byte orders (2x2, bits 16/16/16, with and without tag 34675) and a minimal PNG header with and without iCCP. Truncated inputs -> unknown.

## Do not
- No third-party packages; do not modify `assert-bytes.ts`.
