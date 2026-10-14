"""The GPU adapter stages only measured, pinned Evaluation evidence."""
import json
from contextlib import nullcontext
from pathlib import Path
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.operations.learning.contracts import Evaluation, FrozenRecipe
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.learning.evaluate_adapter import (
    execute, learning_evaluate_operations)
from photo_worker.operations.learning.evaluation import RenderedHoldout, _qualified_identity
from photo_worker.operations.session.contracts import FaceObservations
from photo_worker.runtime.executor import OperationFailure

from test_v2_learning_evaluation import inputs
from test_v2_learning_fit_adapter import ref,descriptor_shard


class Provider:
    backend = 'cuda'
    renderer_version = 'v2.2'

    def __init__(self, params):
        self.model_lock_sha = params.model_lock_sha
        self.icc_lock_sha = params.icc_lock_sha
        self.calls = []

    def render(self, ctx, artifacts, item, styles, calibration):
        self.calls.append(item.event_id)
        assert isinstance(styles.baseline, StyleV2)
        assert isinstance(styles.candidate, StyleV2)
        assert isinstance(styles.approved, StyleV2)
        rgb = np.full((2, 2, 3), .5, np.float32)
        alpha = np.ones((2, 2), np.float32)
        guard = np.zeros((2, 2), np.float32)
        return nullcontext(RenderedHoldout(item.event_id, item.render_case.binding,
            self.renderer_version, self.icc_lock_sha, self.model_lock_sha,
            rgb, rgb.copy(), rgb.copy(), alpha, guard,
            True, True, 0, False, 'no_person'))


def test_zero_faces_does_not_upgrade_unmeasurable_identity_to_no_person():
    params, manifest, _, _, _, _ = inputs()
    item = next(example for example in manifest.examples
                if example.split == 'holdout')
    rgb = np.full((2, 2, 3), .5, np.float32)
    evidence = RenderedHoldout(
        item.event_id, item.render_case.binding, 'v2.2',
        params.icc_lock_sha, params.model_lock_sha, rgb, rgb, rgb,
        np.ones((2, 2), np.float32), np.zeros((2, 2), np.float32),
        True, True, 0, False, 'no_person')
    assert _qualified_identity(evidence, 0) == 'no_person'
    assert _qualified_identity(evidence.__class__(
        **{**vars(evidence), 'identity_status': 'not_measurable'}), 0) == 'not_measurable'
    assert _qualified_identity(evidence.__class__(
        **{**vars(evidence), 'identity_status': 'fail',
           'detector_qualified': False}), 0) == 'fail'
    with pytest.raises(ValueError, match='INVALID_IDENTITY_EVIDENCE'):
        _qualified_identity(evidence.__class__(
            **{**vars(evidence), 'identity_status': 'unknown'}), 0)
    with pytest.raises(ValueError, match='INVALID_IDENTITY_EVIDENCE'):
        _qualified_identity(evidence, 1)


def test_adapter_stages_measured_evaluation_and_checks_locks(tmp_path, metric_scratch):
    params, manifest, baseline, candidate, calibration, _ = inputs()
    provider = Provider(params)
    staged = []
    values = [(params.manifest, manifest), (params.baseline, baseline),
              (params.candidate, candidate), (params.calibration, calibration)]
    for item in manifest.examples:
        case = item.render_case
        values.append((case.descriptor_shard,descriptor_shard(item)))
        values.append((case.faces, FaceObservations(
            schema_version=1, binding=case.binding, faces=[])))
        for recipe_ref in (case.base_recipe, case.approved_recipe):
            values.append((recipe_ref, FrozenRecipe.model_validate(dict(
                schema_version=1, source=case.binding, base_style=ref(3000),
                effective_delta=item.target, renderer_version='v2.2',
                icc_lock_sha=params.icc_lock_sha,
                model_lock_sha=params.model_lock_sha))))
    def resolve(source, model):
        return next(value for pinned, value in values if pinned == source)
    artifacts = SimpleNamespace(params=lambda model: params,
        contract=resolve,
        style=lambda ref: StyleV2.model_validate(json.loads((
            Path(__file__).resolve().parents[2] /
            'docs/implementation/contracts/examples/style-fixed.example.json'
        ).read_text())),
        scratch=metric_scratch,
        stage_contract=lambda value, role: staged.append((value, role)) or ref(900))
    progress = []
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cuda'}),
        check_active=lambda: None,
        progress=lambda done, total: progress.append((done, total)))
    result = execute(ctx, artifacts, provider)
    assert result.outputs == [ref(900)]
    assert len(provider.calls) == len(staged[0][0].cases)
    assert staged[0][1] == 'document' and isinstance(staged[0][0], Evaluation)
    assert staged[0][0].eligible and progress[-1][0] == progress[-1][1]
    provider.icc_lock_sha = '0' * 64
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(ctx, artifacts, provider)
    assert len(staged) == 1


@pytest.mark.parametrize('change', ['source', 'base_style', 'icc_lock_sha'])
def test_adapter_rejects_unbound_holdout_recipe_before_render(tmp_path, change, metric_scratch):
    params, manifest, baseline, candidate, calibration, _ = inputs()
    provider = Provider(params)
    first = next(item for item in manifest.examples if item.split == 'holdout')
    case = first.render_case
    base_recipe = FrozenRecipe.model_validate(dict(
        schema_version=1, source=case.binding, base_style=ref(3000),
        effective_delta=first.target, renderer_version='v2.2',
        icc_lock_sha=params.icc_lock_sha, model_lock_sha=params.model_lock_sha))
    changed = base_recipe.model_copy(deep=True)
    if change == 'source':
        changed.source.asset_version_id = ref(9999).artifact_id
    elif change == 'base_style':
        changed.base_style = ref(9999)
    else:
        changed.icc_lock_sha = '0' * 64
    def resolve(source, model):
        if source == case.descriptor_shard:
            return descriptor_shard(first)
        if source == params.manifest:
            return manifest
        if source == params.baseline:
            return baseline
        if source == params.candidate:
            return candidate
        if source == params.calibration:
            return calibration
        if source == case.base_recipe:
            return base_recipe
        if source == case.approved_recipe:
            return changed
        raise AssertionError('read past invalid first recipe')
    artifacts = SimpleNamespace(params=lambda model: params, contract=resolve,
        scratch=metric_scratch,
        stage_contract=lambda value, role: pytest.fail('staged invalid result'))
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cuda'}),
        check_active=lambda: None, progress=lambda done, total: None)
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(ctx, artifacts, provider)
    assert provider.calls == []


def test_adapter_requires_explicit_provider():
    with pytest.raises(ValueError, match='Qualified render provider required'):
        learning_evaluate_operations(None, None)


@pytest.fixture
def metric_scratch(tmp_path):
    from uuid import uuid4
    from photo_worker.runtime.scratch import ScratchSpace
    with ScratchSpace(tmp_path.resolve(), uuid4(), uuid4(), 1, 1024 * 1024,
                      min_free_bytes=0) as scratch:
        yield scratch


@pytest.mark.parametrize('profile', [None, [], 'cuda', 42])
def test_evaluate_invalid_profile_refuses_before_artifact_io(profile):
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile=profile))
    artifacts = SimpleNamespace(params=lambda _: pytest.fail('invalid profile read artifacts'))
    provider = SimpleNamespace(backend='cuda', renderer_version='v2.2')
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute(ctx, artifacts, provider)


def test_evaluate_owns_params_before_loading_manifest():
    params, manifest, baseline, candidate, calibration, _ = inputs()
    original = params.model_copy(deep=True)
    requested = []
    def resolve(source, model):
        requested.append(source.model_copy(deep=True))
        if model.__name__ == 'TrainingManifest':
            # A gateway callback cannot redirect later reads by changing the
            # previously returned parameter object retained by that gateway.
            params.baseline = ref(9998)
            return manifest
        if model.__name__ == 'CandidateProfile':
            assert source == original.baseline
            raise ValueError('stop after pinned baseline')
        pytest.fail('unexpected read')
    artifacts = SimpleNamespace(params=lambda _: params, contract=resolve)
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version':'v2.2','backend':'cuda'}),check_active=lambda:None)
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(ctx, artifacts, Provider(original))
    assert requested == [original.manifest, original.baseline]
