from types import SimpleNamespace
import pytest
from photo_worker.operations.interchange.export_image_probe import probe_export_image


def test_probe_returns_only_decoder_metadata_and_runtime_pin(monkeypatch, tmp_path):
    import photo_worker.operations.interchange.export_image_probe as module
    calls = []
    def native(path, media, size, sha):
        calls.append((path, media, size, sha))
        return SimpleNamespace(width=100, height=200, bit_depth=16, has_alpha=False,
                               orientation=6, icc_sha256=None)
    monkeypatch.setattr(module, 'qualify_export_image_file', native)
    monkeypatch.setattr(module, 'export_native_runtime_sha', lambda: 'b' * 64)
    request = dict(schema_version=1, file_path=str(tmp_path / 'private.tiff'),
                   sha256='a' * 64, bytes=123, media_type='image/tiff')
    result = probe_export_image(request)
    assert len(calls) == 1
    assert result['sha256'] == request['sha256'] and result['bytes'] == 123
    assert result['orientation'] == 6 and result['runtime_sha256'] == 'b' * 64
    assert 'file_path' not in result


@pytest.mark.parametrize('patch', [{'schema_version': True}, {'file_path': 'relative.tiff'}, {'approved': True}])
def test_probe_rejects_invalid_envelope_before_decoder(monkeypatch, tmp_path, patch):
    import photo_worker.operations.interchange.export_image_probe as module
    def native(*args): raise AssertionError('invalid envelope reached decoder')
    monkeypatch.setattr(module, 'qualify_export_image_file', native)
    request = dict(schema_version=1, file_path=str(tmp_path / 'private.tiff'),
                   sha256='a' * 64, bytes=123, media_type='image/tiff')
    request.update(patch)
    with pytest.raises(ValueError): probe_export_image(request)
