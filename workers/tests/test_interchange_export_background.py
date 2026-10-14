from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
from photo_worker.operations.interchange.export_images import qualify_export_background


@pytest.mark.parametrize('mode', ['valid', 'size', 'media', 'changed', 'decode'])
def test_background_qualification_binds_frozen_pin_and_dedicated_contract(monkeypatch, mode):
    import photo_worker.operations.interchange.export_images as module
    source = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    pin = FrozenBackgroundAsset(schema_version=1, portable_asset_id='studio',
        style_version_id=uuid4(), style=ArtifactRef(artifact_id=uuid4(), sha256='b' * 64),
        source=source, expected_bytes=5, media_type='image/tiff', suffix='.tiff',
        assume_profile=None, work_icc_sha='c' * 64)
    events = []
    evidence = SimpleNamespace(bytes=6 if mode == 'size' else 5,
        media_type='image/png' if mode == 'media' else 'image/tiff')
    class Reader:
        calls = 0
        def evidence(self, ref):
            assert ref == source
            self.calls += 1
            if mode == 'changed' and self.calls > 1:
                return SimpleNamespace(bytes=6, media_type='image/tiff')
            return evidence
    def raster(entry, reader, scratch, contract, *, active):
        events.append(contract)
        assert entry.content == source and entry.path == 'background.tiff'
        if mode == 'decode': raise ValueError('native rejected')
        return SimpleNamespace(reference=source)
    monkeypatch.setattr(module, '_qualify_export_image', raster)
    if mode == 'valid':
        assert qualify_export_background(pin, Reader(), object()).reference == source
        assert events == ['StyleBackgroundAsset']
    else:
        with pytest.raises(ValueError): qualify_export_background(pin, Reader(), object())
        if mode in ('size', 'media'): assert events == []
