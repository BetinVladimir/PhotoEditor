from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
from photo_worker.v2.interchange_export_backgrounds import qualify_export_background_sources


@pytest.mark.parametrize('mode', ['valid', 'alias', 'receipt'])
def test_qualified_background_sources_deduplicate_bytes_and_keep_portable_receipts(monkeypatch, mode):
    import photo_worker.v2.interchange_export_backgrounds as module
    import photo_worker.operations.interchange.export_images as images
    source = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    pin = FrozenBackgroundAsset(schema_version=1, portable_asset_id='studio',
        style_version_id=uuid4(), style=ArtifactRef(artifact_id=uuid4(), sha256='b' * 64),
        source=source, expected_bytes=5, media_type='image/tiff', suffix='.tiff',
        assume_profile=None, work_icc_sha='c' * 64)
    second = pin.model_copy(deep=True);second.portable_asset_id = 'other-name'
    if mode == 'alias': second.expected_bytes = 6
    identifiers = [uuid4(), uuid4()]
    monkeypatch.setattr(module, 'resolve_export_background_pins', lambda *args, **kw:
        ((identifiers[0], pin), (identifiers[1], second)))
    calls = []
    def physical(value, *args, **kw):
        calls.append(value)
        return SimpleNamespace(reference=source, bytes=6 if mode == 'receipt' else 5,
                               media_type='image/tiff')
    monkeypatch.setattr(images, 'qualify_export_background', physical)
    class Reader:
        checks = 0
        def recheck(self): self.checks += 1
    reader = Reader()
    if mode == 'valid':
        sources, receipts = qualify_export_background_sources(object(), reader, object())
        assert len(sources) == 1 and len(calls) == 1 and reader.checks == 1
        assert len(receipts) == 2 and receipts[0][0] == identifiers[0]
        assert b'"portable_asset_id":"studio"' in receipts[0][1]
        assert b'"portable_asset_id":"other-name"' in receipts[1][1]
    else:
        with pytest.raises(ValueError):
            qualify_export_background_sources(object(), reader, object())
