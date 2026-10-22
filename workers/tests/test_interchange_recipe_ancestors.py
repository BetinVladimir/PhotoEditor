from hashlib import sha256
from uuid import uuid4
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_recipe_ancestors import install_prepared_ancestors
from test_interchange_import_stage import adopted_fixture


@pytest.mark.parametrize('mode', ['valid', 'no_authority', 'rejected', 'geometry', 'hash', 'duplicate'])
def test_prebeauty_ancestor_requires_exact_prepared_lineage_and_never_rebinds_current(mode):
    _, _, _, raw = adopted_fixture()
    current = WorkingImage.model_validate_json(raw)
    binding = VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(current)).hexdigest()), canvas_hash=canonical_hash(current.canvas))
    ancestor = current.model_copy(deep=True)
    ancestor.pixels = ArtifactRef(artifact_id=uuid4(), sha256='b' * 64)
    if mode == 'geometry': ancestor.canvas.width += 1
    reference = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(ancestor)).hexdigest())
    if mode == 'hash': reference.sha256 = 'c' * 64
    docs = SimpleNamespace(images={binding.asset_version_id: (binding, current)}, image_hashes={binding.source.sha256})
    remembered = []
    arrays = SimpleNamespace(_remember=lambda *args, **kwargs: remembered.append((args, kwargs)))
    rows = [(binding, reference, ancestor)] * (2 if mode == 'duplicate' else 1)
    authority = None if mode == 'no_authority' else lambda b, ref, image: mode != 'rejected'
    if mode == 'valid':
        install_prepared_ancestors(docs, arrays, rows, authority, lambda: None)
        assert reference.sha256 in docs.image_hashes
        assert docs.images[binding.asset_version_id] == (binding, current)
        assert remembered[0][0][1] == ancestor.pixels
    else:
        with pytest.raises((AdoptionInvalid, ValueError)):
            install_prepared_ancestors(docs, arrays, rows, authority, lambda: None)
