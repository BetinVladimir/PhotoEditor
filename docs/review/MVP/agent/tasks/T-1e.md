---
id: T-1e
title: e2e fixtures - deterministic photo generator and manifest
area: tests
size: S
depends: [T-1a]
edit:
  - tests/e2e/fixtures/make_fixtures.py (new)
  - tests/e2e/fixtures/README.md (new)
  - tests/e2e/.gitignore
context: []
verify:
  - python3 -c "import ast,sys;ast.parse(open('tests/e2e/fixtures/make_fixtures.py').read())"
  - test -s tests/e2e/fixtures/README.md
---
## Goal
The e2e suite needs license-clean sample photos. Generate them synthetically (no downloads) so that they are reproducible and never committed as binaries.

## Spec (`tests/e2e/fixtures/make_fixtures.py`, Python 3.9+, Pillow + numpy + tifffile; usage `python3 make_fixtures.py [--out DIR]`, default `tests/e2e/fixtures/photos`)
Create with a fixed seed (`numpy.random.default_rng(20260101)`) smooth gradients with a few coloured shapes (at most 1600x1067 px) and write:
- `jpeg-srgb-icc.jpg` (quality 90, embedded sRGB ICC from `PIL.ImageCms.createProfile('sRGB')` converted with `ImageCms.ImageCmsProfile(...).tobytes()`), `jpeg-noicc.jpg`, `jpeg-rot6.jpg` (EXIF orientation 6 via `Image.Exif()`), `png-alpha.png` (RGBA), `tiff16.tif` (RGB, 16 bits per channel: `tifffile.imwrite(path, arr_uint16_hwc, photometric='rgb')`, so also `pip install tifffile`; embed the same sRGB ICC with `extratags=[(34675, 'B', len(icc), icc, True)]`), `corrupt.jpg` (the first 60 % of bytes of `jpeg-srgb-icc.jpg`), `duplicate.jpg` (byte-identical copy of `jpeg-noicc.jpg`).
- `manifest.json`: list of `{name, sha256, bytes, kind, expect}` where `expect` is `accept` | `reject-corrupt` | `duplicate-of:<name>`.
- RAW files cannot be generated: if `raw/*.dng` exist next to the script output they are added to the manifest with `expect: accept` and kind `raw`; otherwise the script prints `NOTE: put 2 small CC0 DNG files into <out>/raw/ to enable the RAW cases`.
`README.md`: how to run, that the output folder is git-ignored, which E-xx scenarios use which file.
Add `fixtures/photos/` to `tests/e2e/.gitignore`.

## Do not
- Do not download images. Do not commit generated binaries.
