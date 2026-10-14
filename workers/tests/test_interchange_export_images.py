from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.operations.interchange.export_images import qualify_export_image
from photo_worker.operations.interchange.export_sidecar_factory import SidecarExportPreparationFactory
from photo_worker.v2.adoption import ArtifactEvidence, ArtifactRef as EvidenceRef
from test_interchange_export_prepare import fixture


@pytest.mark.parametrize('mode', ['valid', 'media', 'contract', 'decode', 'changed'])
def test_export_image_qualification_streams_then_decodes_and_rechecks(tmp_path, monkeypatch, mode):
    import photo_worker.operations.interchange.export_images as module
    plan, sources, _ = fixture();entry = plan.entries[0]
    ref = entry.content
    evidence = ArtifactEvidence(EvidenceRef(ref.artifact_id, ref.sha256),
        'image/jpeg' if mode == 'media' else 'image/tiff', 5, None, 'input')
    events = []
    class Reader:
        entries = {(ref.artifact_id, ref.sha256):SimpleNamespace(contract='Wrong' if mode == 'contract' else 'EncodedImage')}
        calls = 0
        def evidence(self, reference):
            self.calls += 1
            if mode == 'changed' and self.calls > 1:
                return ArtifactEvidence(evidence.ref, evidence.media_type, 6, None, 'input')
            return evidence
        def iter_bytes(self, reference, maximum):
            events.append('stream');assert maximum == 5;yield b'image'
    class Reservation:
        def path(self, role, suffix):
            assert suffix == '.tiff'
            return tmp_path / ('image' + suffix)
        def validate_file(self, path):
            assert path.read_bytes() == b'image'
    class Scratch:
        @contextmanager
        def reserve(self, size):
            assert size == 5;yield Reservation()
        def open_writer(self, path, reservation):return path.open('wb')
    monkeypatch.setattr(module, 'verify_magic', lambda path: 'tiff')
    monkeypatch.setattr(module, 'verify_suffix', lambda kind, suffix: None)
    monkeypatch.setattr(module, 'read_metadata', lambda source: SimpleNamespace(orientation=6))
    def decode(path, check_active):
        events.append('decode');check_active()
        if mode == 'decode': raise ValueError('IMAGE_DECODE_FAILED')
    monkeypatch.setattr(module, 'verify_raster_decode', decode)
    if mode == 'valid':
        result = qualify_export_image(entry, Reader(), Scratch())
        assert result.reference == ref and result.bytes == 5 and result.orientation == 6
        assert events == ['stream', 'decode']
    else:
        with pytest.raises(ValueError):qualify_export_image(entry, Reader(), Scratch())
        if mode == 'contract':assert events == []


@pytest.mark.parametrize('pin', ['', 'x'*64, 'a'*63, None])
def test_sidecar_factory_requires_installed_work_icc(pin):
    with pytest.raises(ValueError, match='ICC_PIN'):
        SidecarExportPreparationFactory(pin)

@pytest.mark.parametrize('mode', ['valid', 'hash', 'size', 'symlink', 'changed', 'cancelled'])
def test_native_file_qualification_checks_physical_pin(tmp_path, monkeypatch, mode):
    from hashlib import sha256
    from photo_worker.operations.interchange.export_images import qualify_export_image_file
    import photo_worker.operations.interchange.export_images as module
    path = tmp_path / 'image.tiff'; path.write_bytes(b'image')
    expected_sha, size, events = sha256(b'image').hexdigest(), 5, []
    if mode == 'hash': expected_sha = 'a' * 64
    if mode == 'size': size = 6
    if mode == 'symlink':
        target = tmp_path / 'target.tiff'; path.rename(target); path.symlink_to(target)
    monkeypatch.setattr(module, 'verify_magic', lambda path: 'tiff')
    monkeypatch.setattr(module, 'verify_suffix', lambda kind, suffix: None)
    monkeypatch.setattr(module, 'read_metadata', lambda source: SimpleNamespace(orientation=6))
    def decode(path, check_active):
        events.append('decode'); check_active()
        if mode == 'changed': path.write_bytes(b'changed')
    monkeypatch.setattr(module, 'verify_raster_decode', decode)
    def active():
        if mode == 'cancelled': raise ValueError('cancelled')
    if mode == 'valid':
        assert qualify_export_image_file(path, 'image/tiff', size, expected_sha, active=active).orientation == 6
        assert events == ['decode']
    else:
        with pytest.raises(ValueError): qualify_export_image_file(path, 'image/tiff', size, expected_sha, active=active)
        if mode in ('hash', 'size', 'symlink', 'cancelled'): assert events == []
