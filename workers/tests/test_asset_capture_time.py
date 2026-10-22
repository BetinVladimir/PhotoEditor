"""Capture metadata is optional sideband; floating EXIF never inherits host TZ."""
import pytest
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef

from photo_worker.operations.asset.inspect import _capture_time
from photo_worker.contracts.assets import AssetMetadata


class Header:
    def __init__(self, fields):
        self.fields = fields

    def get_fields(self):
        return list(self.fields)

    def get(self, name):
        return self.fields[name]


@pytest.mark.parametrize('offset,expected', [
    ('+03:00', ('2026-10-17T10:20:30Z', None)),
    ('-04:30', ('2026-10-17T17:50:30Z', None)),
    (None, (None, '2026-10-17T13:20:30')),
    ('+15:00', (None, '2026-10-17T13:20:30')),
])
def test_genuine_exif_offset_is_only_timezone_authority(offset, expected):
    fields = {'exif-ifd2-DateTimeOriginal': '2026:10:17 13:20:30 (20 bytes)'}
    if offset is not None:
        fields['exif-ifd2-OffsetTimeOriginal'] = offset
    assert _capture_time(Header(fields)) == expected


@pytest.mark.parametrize('fields', [
    {},
    {'exif-ifd0-DateTime': '2026:10:17 13:20:30'},
    {'exif-ifd2-DateTimeOriginal': '2026:02:30 13:20:30'},
    {'exif-ifd2-DateTimeOriginal': 'not a timestamp'},
])
def test_missing_or_invalid_capture_is_unknown_not_file_time(fields):
    assert _capture_time(Header(fields)) == (None, None)


def test_capture_contract_preserves_historical_wire_and_floating_classification():
    source = ArtifactRef(artifact_id=UUID('11111111-1111-4111-8111-111111111111'), sha256='a' * 64)
    base = dict(source=source, width=400, height=300, bit_depth=16,
                has_alpha=False, orientation=1, icc_sha256=None, format='jpeg')
    historical = AssetMetadata(**base).model_dump(mode='json')
    assert 'captured_at' not in historical and 'captured_local' not in historical
    floating = AssetMetadata(**base, captured_local='2026-10-17T13:20:30').model_dump(mode='json')
    assert floating['captured_local'] == '2026-10-17T13:20:30'
    assert 'captured_at' not in floating
    with pytest.raises(ValueError):
        AssetMetadata(**base, captured_at='2026-02-30T13:20:30Z')
    with pytest.raises(ValueError):
        AssetMetadata(**base, captured_at='2026-10-17T10:20:30Z', captured_local='2026-10-17T13:20:30')
