from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import ArtifactEvidence, AdoptionInvalid, ArtifactRef as EvidenceRef
from photo_worker.v2.interchange_recipe_backgrounds import PreparedImportBackgroundQualifier
from test_interchange_import_stage import adopted_fixture


@pytest.mark.parametrize('mode', ['valid', 'foreign_binding', 'role', 'media', 'size', 'hash', 'changed', 'native_refusal'])
def test_prepared_background_qualifies_stream_and_current_evidence(tmp_path, monkeypatch, mode):
    _, sources, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    binding = VersionBinding(asset_version_id=sources.entries[0].asset_version_id,
        source=sources.entries[0].master, canvas_hash=canonical_hash(image.canvas))
    raw = b'background-raster'
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    class Reader:
        opens = 0
        calls = 0
        entries = {(ref.artifact_id, ref.sha256): SimpleNamespace(contract='StyleBackgroundAsset')}
        def evidence(self, reference):
            self.calls += 1
            return ArtifactEvidence(EvidenceRef(reference.artifact_id, reference.sha256),
                'application/octet-stream' if mode == 'media' else 'image/png',
                2 if mode == 'size' else len(raw) + (1 if mode == 'changed' and self.opens else 0), None, 'input')
        def iter_bytes(self, reference, maximum):
            self.opens += 1
            yield b'x' + raw[1:] if mode == 'hash' else raw
    reader = Reader()
    native_calls = []
    def decode(path, media, count, digest, *, active):
        active()
        native_calls.append(path.suffix)
        data = path.read_bytes()
        if mode == 'native_refusal' or len(data) != count or sha256(data).hexdigest() != digest:
            raise ValueError('native refusal')
        assert media == 'image/png'
        return SimpleNamespace(orientation=1)
    monkeypatch.setattr('photo_worker.operations.interchange.export_images.qualify_export_image_file', decode)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportBackgroundQualifier(canonical_images=[(binding, image)], reader=reader, scratch=scratch)
        target = binding.model_copy(deep=True)
        if mode == 'foreign_binding':
            target.asset_version_id = uuid4()
        contract = 'EncodedImage' if mode == 'role' else 'StyleBackgroundAsset'
        if mode == 'valid':
            assert qualifier(ref, contract, target) is True
            assert reader.opens == 1 and native_calls == ['.png']
        else:
            with pytest.raises((AdoptionInvalid, ValueError)):
                qualifier(ref, contract, target)
            if mode in ('foreign_binding', 'role', 'media', 'size'):
                assert reader.opens == 0 and native_calls == []
