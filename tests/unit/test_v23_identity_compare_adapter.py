"""Exact-canvas identity comparison produces an inspectable changed mask."""

from contextlib import contextmanager
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID

import numpy as np
import pytest
from jsonschema import Draft202012Validator, FormatChecker

from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import (Canvas, ColorSpace, MaskRef,
                                           TransformChain, WorkingImage)
from photo_worker.operations.identity.adapter import execute
from photo_worker.operations.identity.contracts import IdentityCompareParams
from photo_worker.operations.session.contracts import (
    ConsentSnapshot, FaceObservations, VersionBinding,
)
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_v2_session_describe import calibration


def ref(value):
    return ArtifactRef(artifact_id=UUID(int=value), sha256=f'{value:064x}')


class Artifacts:
    def __init__(self, root: Path):
        self.scratch = ScratchSpace(root, UUID(int=100), UUID(int=101), 1,
                                    1024 * 1024, min_free_bytes=0)
        canvas = Canvas(width=4, height=4, orientation=1, space_id='source',
                        source_sha256='1' * 64)
        chain = TransformChain.create([])
        color = ColorSpace(profile_id='work_rec2020_gamma24_v1',
                           icc_sha256='2' * 64, encoding='gamma24')
        before = WorkingImage(pixels=ref(3), canvas=canvas, color=color,
                              dtype='float32', alpha=None, transform_chain=chain)
        after = before.model_copy(update={'pixels': ref(4)})
        self.params_value = IdentityCompareParams(
            schema_version=1,
            before=VersionBinding(asset_version_id=UUID(int=20),
                                  source=ref(1), canvas_hash=canonical_hash(canvas)),
            after=VersionBinding(asset_version_id=UUID(int=21),
                                 source=ref(2), canvas_hash=canonical_hash(canvas)),
            effective_mask=ref(5), protected=ref(6),
            model_lock_sha='a' * 64, calibration=ref(7), consents=ref(8))
        mask = dict(canvas=canvas, transform_chain=chain, object_id=None,
                    selector='test', confidence=1, model_lock_id=None)
        self.documents = {
            ref(1).artifact_id: before, ref(2).artifact_id: after,
            ref(5).artifact_id: MaskRef(artifact=ref(9), protected=False, **mask),
            ref(6).artifact_id: MaskRef(artifact=ref(10), protected=True, **mask),
            ref(7).artifact_id: calibration(),
            ref(8).artifact_id: ConsentSnapshot(schema_version=1, grants=[],
                                    confirmed_faces=[]),
        }
        before_pixels = np.full((4, 4, 3), .5, np.float32)
        after_pixels = before_pixels.copy()
        after_pixels[1, 2, 0] = .75
        self.paths = {}
        for artifact, array in ((ref(3), before_pixels), (ref(4), after_pixels),
                                (ref(9), np.ones((4, 4), np.float32)),
                                (ref(10), np.zeros((4, 4), np.float32))):
            path = root / f'{artifact.artifact_id}.npy'
            np.save(path, array)
            self.paths[artifact.artifact_id] = path
        self.staged = []
        self.mask_pixels = None

    def params(self, model):
        assert model is IdentityCompareParams
        return self.params_value

    def contract(self, reference, model):
        value = self.documents[reference.artifact_id]
        assert isinstance(value, model)
        return value

    @contextmanager
    def input(self, reference, *, suffix):
        yield SimpleNamespace(path=self.paths[reference.artifact_id])

    def stage(self, path, reservation, role, media_type):
        assert role == 'mask_pixels' and media_type == 'application/x-npy'
        self.mask_pixels = np.load(path).copy()
        return ref(30)

    def stage_contract(self, value, role):
        self.staged.append((role, value))
        return ref(30 + len(self.staged))


def context():
    return SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cuda'}),
        check_active=lambda: None, progress=lambda done, total: None)


def pool():
    return SimpleNamespace(lock=SimpleNamespace(lock_id='a' * 64, models=[
        SimpleNamespace(id=name) for name in ('sfd', 'fan2d', 'identity512')]))


def test_identity_compare_exact_canvas_mask_and_unmeasurable_faces(tmp_path,
                                                                   monkeypatch):
    artifacts = Artifacts(tmp_path)
    monkeypatch.setattr('photo_worker.operations.identity.adapter._face_observations',
                        lambda rgb, binding, *args: FaceObservations(
                            schema_version=1, binding=binding, faces=[]))
    try:
        result = execute(context(), artifacts, pool(), 'cuda')
        assert result.outputs == [ref(30), ref(31), ref(32)]
        assert result.review_required
        assert artifacts.mask_pixels.sum() == 1
        assert artifacts.mask_pixels[1, 2] == 1
        assert artifacts.staged[0][0] == 'mask'
        report = artifacts.staged[1][1]
        assert report.status == 'not_measurable'
        assert report.outside_mask_max_error == 0
        schemas = json.loads((Path(__file__).resolve().parents[2] /
                              'docs/implementation-v2/contracts/schemas.json').read_text())
        Draft202012Validator({'$defs': schemas['$defs'],
            '$ref': '#/$defs/IdentityReport'},
            format_checker=FormatChecker()).validate(report.model_dump(mode='json'))
    finally:
        artifacts.scratch.cleanup()


def test_identity_compare_rejects_unaligned_canvas(tmp_path):
    artifacts = Artifacts(tmp_path)
    after = artifacts.documents[ref(2).artifact_id]
    different = after.canvas.model_copy(update={'source_sha256': 'f' * 64})
    artifacts.documents[ref(2).artifact_id] = after.model_copy(update={'canvas': different})
    try:
        with pytest.raises(OperationFailure, match='INPUT_INVALID'):
            execute(context(), artifacts, pool(), 'cuda')
        assert not artifacts.staged
    finally:
        artifacts.scratch.cleanup()
