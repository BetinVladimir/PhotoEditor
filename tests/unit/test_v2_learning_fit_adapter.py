"""The learning.fit worker validates a frozen manifest before staging a model."""

from uuid import UUID
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.operations.learning.contracts import (
    CandidateProfile, FrozenRecipe, LearningFitParams, TrainingManifest,
)
from photo_worker.operations.learning.fit import execute, fit_candidate
from photo_worker.operations.learning.freeze import _session_order
from photo_worker.operations.session.contracts import Calibration, DescriptorShard
from photo_worker.runtime.executor import OperationFailure


def ref(n):
    return ArtifactRef(artifact_id=UUID(int=n), sha256=f'{n:064x}')


def fixture():
    profile = UUID(int=20)
    manifest_ref, baseline_ref, calibration_ref = ref(100), ref(101), ref(102)
    params = LearningFitParams.model_validate(dict(schema_version=1,
        manifest=manifest_ref, base_profile=baseline_ref,
        policy=dict(schema_version=1, mode='photographer', min_examples=40,
                    min_sessions=6, holdout_fraction=.2, seed=2026,
                    calibration=calibration_ref)))
    sessions = sorted([UUID(int=i+1) for i in range(6)], key=_session_order)
    held = set(sessions[:2])
    examples = []
    for s, session in enumerate(sessions):
        for i in range(10):
            n = s * 10 + i + 1
            source = ref(2000+n)
            examples.append(dict(event_id=UUID(int=100+n), session_id=session,
                subject_group_id=profile,
                split='holdout' if session in held else 'train',
                features=[n/100]*5 + [0]*7,
                target=dict(ev=.1, log_r=0, log_g=0, log_b=0,
                            saturation_delta=0, retouch_delta=0),
                render_case=dict(binding=dict(asset_version_id=UUID(int=300+n),
                            source=source, canvas_hash='a'*64),
                    working_image=source, base_recipe=ref(400+n),
                    approved_recipe=ref(500+n), protected=ref(600+n),
                    faces=ref(700+n), approval_id=UUID(int=800+n),
                    base_document=ref(10000+n), approved_document=ref(11000+n),
                    descriptor_shard=ref(12000+n), descriptor_analysis_id=UUID(int=13000+n),
                    descriptor_ordinal=0,
                    source_lineage_sha=f'{n:064x}')))
    manifest = TrainingManifest.model_validate(dict(schema_version=1,
        profile_id=profile, mode='photographer', person_id=None,
        consents=[dict(consent_id=UUID(int=30), epoch='1')],
        examples=examples, policy_sha=canonical_hash(params.policy),
        baseline=baseline_ref,
        renderer_version='v2.2', icc_lock_sha='c'*64,
        model_lock_sha='d'*64, calibration=calibration_ref))
    baseline = CandidateProfile.model_validate(dict(schema_version=1,
        profile_id=profile, training_sha='0'*64, feature_order='scene12_v1',
        means=[0]*12, scales=[1]*12, coefficients=[[0]*6 for _ in range(12)],
        intercept=[0]*6, ridge_lambda=1.0))
    calibration = Calibration.model_validate(dict(schema_version=1,
        model_lock_sha='d'*64, dataset_sha='e'*64, qualified=True,
        scene_cosine_min=.3, scene_log_luma_max=1,
        cluster_diameter_max=1, sharpness_scale=1, clipping_penalty=1,
        match_inlier_min=.3, match_error_px_max=5,
        identity_similarity_min=.4, landmark_drift_max=.2,
        normalization_scales=[1, .2, .2, .2, .15, .15],
        loss_weights=[.4, .1, .1, .1, .2, .1], focus_warning=.5,
        focus_critical=.2, eye_open_min=.5,
        glare_warning_fraction=.1, clipping_warning_fraction=.1,
        clipping_critical_fraction=.3, min_face_edge_px=64,
        depth_sigma=.1, registration_error_px=2, motion_threshold=.1))
    return params, manifest, baseline, calibration


def descriptor_shard(item):
    # Independently derive the pre-edit fixture pixels' descriptor values;
    # changing item.features must not change the trusted artifact fixture.
    level=(item.event_id.int-100)/100
    return DescriptorShard.model_validate(dict(schema_version=1,snapshot_digest='a'*64,
        frames=[dict(schema_version=1,binding=item.render_case.binding,ordinal=0,
            captured_at=None,camera_hash=None,scene_embedding=[0]*384,
            luma_quantiles=[level]*5,neutral_rgb=None,sharpness=0,
            clipping_fraction=0,person_fraction=0,faces=item.render_case.faces)]))


def frozen_recipe(item, manifest, *, style=3000):
    return FrozenRecipe.model_validate(dict(
        schema_version=1,
        source=item.render_case.binding.model_copy(deep=True),
        base_style=ref(style), effective_delta=item.target,
        renderer_version=manifest.renderer_version,
        icc_lock_sha=manifest.icc_lock_sha,
        model_lock_sha=manifest.model_lock_sha))


def test_fit_candidate_uses_only_train_rows_and_pins_manifest_sha():
    params, manifest, baseline, calibration = fixture()
    candidate = fit_candidate(params, manifest, baseline, calibration)
    assert candidate.profile_id == manifest.profile_id
    assert candidate.training_sha == params.manifest.sha256
    changed = manifest.model_copy(deep=True)
    for example in changed.examples:
        if example.split == 'holdout':
            example.target.ev = -.1
    assert fit_candidate(params, changed, baseline, calibration) == candidate


def test_fit_candidate_rejects_rebound_split_and_duplicate_lineage():
    params, manifest, baseline, calibration = fixture()
    changed = manifest.model_copy(deep=True)
    changed.examples[0].split = 'train'
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, changed, baseline, calibration)
    changed = manifest.model_copy(deep=True)
    changed.examples[1].render_case.source_lineage_sha = (
        changed.examples[0].render_case.source_lineage_sha)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, changed, baseline, calibration)


def test_fit_candidate_rejects_duplicate_source_bytes_across_sessions():
    params, manifest, baseline, calibration = fixture()
    changed = manifest.model_copy(deep=True)
    train = next(item for item in changed.examples if item.split == 'train')
    holdout = next(item for item in changed.examples if item.split == 'holdout')
    train.render_case.working_image = holdout.render_case.working_image
    train.render_case.binding.source = holdout.render_case.working_image
    assert train.render_case.source_lineage_sha != holdout.render_case.source_lineage_sha
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, changed, baseline, calibration)


def test_fit_candidate_rejects_calibration_and_baseline_rebinding():
    params, manifest, baseline, calibration = fixture()
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, manifest, baseline.model_copy(update={
            'profile_id': UUID(int=999)}), calibration)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, manifest, baseline,
                      calibration.model_copy(update={'qualified': False}))


def test_worker_adapter_stages_only_canonical_candidate_document():
    params, manifest, baseline, calibration = fixture()

    class Artifacts:
        staged = []

        def params(self, model):
            assert model is LearningFitParams
            return params

        def contract(self, artifact, model):
            if model is DescriptorShard:
                matched=next(example for example in manifest.examples
                    if artifact==example.render_case.descriptor_shard)
                return descriptor_shard(matched)
            if model is FrozenRecipe:
                item = next(item for item in manifest.examples
                    if artifact in (item.render_case.base_recipe,
                                    item.render_case.approved_recipe))
                return frozen_recipe(item, manifest)
            return {TrainingManifest: manifest, CandidateProfile: baseline,
                    Calibration: calibration}[model]

        def stage_contract(self, value, role):
            self.staged.append((value, role))
            return ref(999)

    artifacts = Artifacts()
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cpu'}),
        check_active=lambda: None)
    result = execute(ctx, artifacts)
    assert result.outputs == [ref(999)]
    assert result.bytes_written > 0
    assert len(artifacts.staged) == 1
    assert artifacts.staged[0][1] == 'document'
    assert artifacts.staged[0][0].training_sha == params.manifest.sha256


@pytest.mark.parametrize('change', ['source', 'base_style', 'model_lock_sha', 'features'])
def test_fit_rejects_invalid_frozen_recipe_in_train_partition(change):
    params, manifest, baseline, calibration = fixture()
    item = next(item for item in manifest.examples if item.split == 'train')
    if change == 'features':
        item.features[0] += .01

    class Artifacts:
        def params(self, model):
            return params

        def contract(self, artifact, model):
            if model is DescriptorShard:
                matched=next(example for example in manifest.examples
                    if artifact==example.render_case.descriptor_shard)
                return descriptor_shard(matched)
            if model is FrozenRecipe:
                matched = next(example for example in manifest.examples
                    if artifact in (example.render_case.base_recipe,
                                    example.render_case.approved_recipe))
                recipe = frozen_recipe(matched, manifest)
                if artifact == item.render_case.approved_recipe:
                    if change == 'source':
                        recipe.source.asset_version_id = ref(9999).artifact_id
                    elif change == 'base_style':
                        recipe.base_style = ref(9999)
                    elif change == 'model_lock_sha':
                        recipe.model_lock_sha = '0' * 64
                return recipe
            return {TrainingManifest: manifest, CandidateProfile: baseline,
                    Calibration: calibration}[model]

        def stage_contract(self, value, role):
            pytest.fail('staged an invalid training candidate')

    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cpu'}),
        check_active=lambda: None)
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(ctx, Artifacts())


@pytest.mark.parametrize('cancel_at', [1, 2, 3, 4, 5, 6, 7, 'stage'])
def test_fit_stops_after_cancelled_artifact_io(monkeypatch, cancel_at):
    from photo_worker.operations.learning import fit as adapter
    from photo_worker.runtime.executor import LeaseLost
    params, manifest, baseline, calibration = fixture()
    calls = []
    cancelled = False

    def active():
        if cancelled:
            raise LeaseLost('lost during artifact IO')

    class Artifacts:
        def record(self, value):
            nonlocal cancelled
            calls.append(value)
            if len(calls) == cancel_at or value == cancel_at:
                cancelled = True

        def params(self, model):
            self.record('params')
            return params

        def contract(self, artifact, model):
            self.record(model.__name__)
            if model is DescriptorShard:
                item = next(row for row in manifest.examples
                            if row.render_case.descriptor_shard == artifact)
                return descriptor_shard(item)
            if model is FrozenRecipe:
                item = next(row for row in manifest.examples
                            if artifact in (row.render_case.base_recipe,
                                            row.render_case.approved_recipe))
                return frozen_recipe(item, manifest)
            return {TrainingManifest: manifest, CandidateProfile: baseline,
                    Calibration: calibration}[model]

        def stage_contract(self, value, role):
            self.record('stage')
            return ref(999)

    monkeypatch.setattr(adapter, 'fit_candidate', lambda *args, **kwargs: baseline)
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cpu'}), check_active=active)
    with pytest.raises(LeaseLost, match='lost during artifact IO'):
        adapter.execute(ctx, Artifacts())
    if isinstance(cancel_at, int):
        assert len(calls) == cancel_at
        assert 'stage' not in calls
    else:
        assert calls[-1] == 'stage'
        assert calls.count('stage') == 1


@pytest.mark.parametrize('change', ['profile', 'consent', 'duplicate', 'epoch', 'person'])
def test_fit_rechecks_manifest_identity_and_consent_shape(change):
    params, manifest, baseline, calibration = fixture()
    if change == 'profile':
        manifest.profile_id = UUID(int=0)
        baseline.profile_id = UUID(int=0)
    elif change == 'consent':
        manifest.consents[0].consent_id = UUID(int=0)
    elif change == 'duplicate':
        manifest.consents.append(manifest.consents[0].model_copy(deep=True))
    elif change == 'epoch':
        manifest.consents[0].epoch = '9223372036854775808'
    else:
        manifest.person_id = UUID(int=1)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, manifest, baseline, calibration)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        TrainingManifest.model_validate(manifest.model_dump())


def test_fit_rejects_zero_subject_group():
    params, manifest, baseline, calibration = fixture()
    manifest.examples[0].subject_group_id = UUID(int=0)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        fit_candidate(params, manifest, baseline, calibration)
