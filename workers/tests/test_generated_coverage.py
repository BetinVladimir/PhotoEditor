from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.image.generated_coverage import stage_generated_coverage
from photo_worker.runtime.scratch import ScratchSpace
from test_interchange_import_stage import adopted_fixture


class Artifacts:
    def __init__(self, root):
        self.scratch = ScratchSpace(root, uuid4(), uuid4(), 1, 16*1048576)
        self.outputs = {}
    def stage(self, path, owner, role, media_type):
        body = path.read_bytes()
        self.outputs[role] = body
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    def stage_contract(self, document, role):
        self.outputs[role] = document
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(document)).hexdigest())


def test_exact_selection_is_published_without_modifying_mask(tmp_path):
    from io import BytesIO
    _, _, _, raw = adopted_fixture()
    image = WorkingImage.model_validate_json(raw)
    selection = np.zeros((image.canvas.height,image.canvas.width),dtype=np.bool_)
    selection[0,0] = True
    original = selection.copy()
    artifacts = Artifacts(tmp_path)
    pixels, reference, written = stage_generated_coverage(artifacts,image,selection,lambda: None)
    physical = np.load(BytesIO(artifacts.outputs['generated_pixels']), allow_pickle=False)
    assert physical.dtype == np.float32
    assert np.array_equal(physical,original.astype(np.float32))
    assert np.array_equal(selection,original)
    mask = artifacts.outputs['generated_mask']
    assert mask.artifact == pixels and mask.selector == 'generated' and not mask.protected
    assert mask.canvas == image.canvas and mask.transform_chain == image.transform_chain
    assert written == len(artifacts.outputs['generated_pixels'])+len(canonical_bytes(mask))


def test_coverage_rejects_fuzzy_or_foreign_selection_before_upload(tmp_path):
    _, _, _, raw = adopted_fixture()
    image = WorkingImage.model_validate_json(raw)
    artifacts = Artifacts(tmp_path)
    with pytest.raises(ValueError):
        stage_generated_coverage(artifacts,image,np.zeros((1,1),np.float32),lambda: None)
    assert artifacts.outputs == {}


def test_coverage_cancellation_prevents_publication(tmp_path):
    _, _, _, raw = adopted_fixture()
    image = WorkingImage.model_validate_json(raw)
    artifacts = Artifacts(tmp_path)
    def cancelled(): raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        stage_generated_coverage(artifacts,image,np.zeros((image.canvas.height,image.canvas.width),bool),cancelled)
    assert artifacts.outputs == {}
