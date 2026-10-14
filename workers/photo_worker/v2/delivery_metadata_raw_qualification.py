"""Operator-run seven-camera RAW metadata qualification matrix.

This produces local evidence only. It cannot grant SQL capability or assert
that the fixtures are genuine camera originals; an operator must review them.
"""
from hashlib import sha256
from pathlib import Path
from uuid import NAMESPACE_URL, uuid5

from pydantic import Field

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, parse_json
from photo_worker.contracts.metadata import MetadataDocument

from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES
from .delivery_metadata_source import prepare_delivery_metadata_source


class RawMetadataFixture(Contract):
    filename: str = Field(min_length=1, max_length=128)
    mime: str = Field(min_length=1, max_length=64)
    original_sha256: SHA256
    original_bytes: int = Field(ge=1, le=2 * 1024**3)
    expected_with_gps: MetadataDocument


class RawMetadataMatrix(Contract):
    schema_version: int = Field(ge=1, le=1)
    fixtures: list[RawMetadataFixture] = Field(min_length=7, max_length=7)


def qualify_raw_metadata_fixtures(root: Path, manifest: bytes, executable: str,
                                  executable_sha: str, *, check_active=lambda: None) -> bytes:
    """Run every format with GPS off/on; return canonical evidence for review."""
    check_active()
    if (not isinstance(root, Path) or not root.is_absolute() or
            root.resolve(strict=True) != root or not root.is_dir() or
            type(manifest) is not bytes or not 2 <= len(manifest) <= 65536):
        raise ValueError('DELIVERY_RAW_MATRIX_INPUT')
    matrix = RawMetadataMatrix.model_validate(parse_json(manifest, max_bytes=65536))
    if canonical_bytes(matrix) != manifest:
        raise ValueError('DELIVERY_RAW_MATRIX_CANONICAL')
    seen_mime, seen_file = set(), set()
    gps_positive = False
    for case in matrix.fixtures:
        if (case.mime not in RAW_METADATA_FILETYPES or case.mime in seen_mime or
                case.filename in seen_file or Path(case.filename).name != case.filename or
                case.filename in ('.', '..') or '/' in case.filename or '\\' in case.filename or
                '\x00' in case.filename):
            raise ValueError('DELIVERY_RAW_MATRIX_CASE')
        seen_mime.add(case.mime); seen_file.add(case.filename)
        gps_positive |= case.expected_with_gps.gps is not None
    if seen_mime != set(RAW_METADATA_FILETYPES) or not gps_positive:
        raise ValueError('DELIVERY_RAW_MATRIX_COVERAGE')
    rows = []
    for case in matrix.fixtures:
        check_active()
        ref = ArtifactRef(artifact_id=uuid5(NAMESPACE_URL, 'raw-metadata:' + case.original_sha256),
                          sha256=case.original_sha256)
        path = root / case.filename
        off = prepare_delivery_metadata_source(path, ref, case.mime, case.original_bytes,
            max_bytes=2 * 1024**3, raw_executable=executable, raw_tool_sha=executable_sha,
            include_gps=False, check_active=check_active)
        on = prepare_delivery_metadata_source(path, ref, case.mime, case.original_bytes,
            max_bytes=2 * 1024**3, raw_executable=executable, raw_tool_sha=executable_sha,
            include_gps=True, check_active=check_active)
        expected_on = canonical_bytes(case.expected_with_gps)
        expected_off = canonical_bytes(case.expected_with_gps.model_copy(update={'gps': None}))
        if on.metadata_jcs != expected_on or off.metadata_jcs != expected_off:
            raise ValueError('DELIVERY_RAW_MATRIX_METADATA_MISMATCH')
        rows.append({'mime': case.mime, 'original_sha256': case.original_sha256,
                     'original_bytes': case.original_bytes,
                     'metadata_sha256': on.metadata_sha256,
                     'gps_off_sha256': off.metadata_sha256})
    check_active()
    return canonical_bytes({'schema_version': 1, 'manifest_sha256': sha256(manifest).hexdigest(),
                            'exiftool_sha256': executable_sha,
                            'fixtures': sorted(rows, key=lambda row: row['mime'])})


def main():
    import argparse
    import sys
    parser = argparse.ArgumentParser(description='Validate the seven-format RAW metadata fixture matrix')
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--exiftool', required=True)
    parser.add_argument('--exiftool-sha256', required=True)
    args = parser.parse_args()
    with args.manifest.open('rb') as source:
        manifest = source.read(65537)
    evidence = qualify_raw_metadata_fixtures(args.root, manifest, args.exiftool,
                                             args.exiftool_sha256)
    sys.stdout.buffer.write(evidence + b'\n')


if __name__ == '__main__':
    main()
