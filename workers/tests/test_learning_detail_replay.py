from types import SimpleNamespace
from contextlib import contextmanager
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.learning.detail_replay import (
    _admit_detail, _copy_detail_arm, _detail_protection,
    _same_detail_styles,
)
from photo_worker.operations.learning.pipeline_admission import validate_holdout_pipeline
from photo_worker.operations.learning.qualification import required_learning_effects
from photo_worker.runtime.executor import OperationFailure


def test_detail_arm_preserves_protected_bits_and_rejects_changes():
    before = np.full((2, 3, 3), .25, dtype=np.float32)
    before[0, 0] = np.array([.11, .22, .33], dtype=np.float32)
    protected = np.zeros((2, 3), dtype=np.float32)
    protected[0, 0] = 1
    prepared = before.copy()
    prepared[1, 1] = .5
    _copy_detail_arm(before, prepared, protected, lambda: None)
    assert np.array_equal(before[0, 0].view(np.uint32),
                          np.array([.11, .22, .33], dtype=np.float32).view(np.uint32))
    assert np.all(before[1, 1] == .5)
    changed = prepared.copy()
    changed[0, 0] = .9
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_PROTECTION_CHANGED'):
        _copy_detail_arm(before, changed, protected, lambda: None)


def test_detail_source_guard_is_exact_even_with_additional_protection():
    source_id, extra_id = uuid4(), uuid4()
    source = np.zeros((2, 3), dtype=np.float32)
    source[0, 0] = 1
    extra = np.zeros((2, 3), dtype=np.float32)
    extra[1, 2] = 1
    masks = {source_id: source, extra_id: extra}
    params = SimpleNamespace(protected=[
        SimpleNamespace(artifact=SimpleNamespace(artifact_id=source_id)),
        SimpleNamespace(artifact=SimpleNamespace(artifact_id=extra_id)),
    ])
    protection = SimpleNamespace(artifact=SimpleNamespace(artifact_id=source_id))
    union = _detail_protection(masks, params, protection, source.copy(),
                               lambda: None)
    assert union[0, 0] == 1 and union[1, 2] == 1
    assert source[1, 2] == 0
    substituted = source.copy()
    substituted[0, 0] = 0
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_PROTECTION_CHANGED'):
        _detail_protection(masks, params, protection, substituted,
                           lambda: None)


def test_detail_admission_requires_peak_and_10gb_cap():
    pixels = 64 * 64
    profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
               'resource_estimate': {'pixel_count': pixels,
                                     'ram_bytes': 128*pixels + 1073741824,
                                     'scratch_bytes': 144*pixels + pixels*12 + 8192 +
                                                      2*(4*pixels + 8192),
                                     'vram_bytes': 10_000_000_000,
                                     'output_bytes': 8192}}
    _admit_detail(profile, 64, 64, 2)
    smaller = {'backend': profile['backend'],
               'renderer_version': profile['renderer_version'],
               'resource_estimate': dict(profile['resource_estimate'])}
    smaller['resource_estimate']['ram_bytes'] -= 1
    with pytest.raises(OperationFailure):
        _admit_detail(smaller, 64, 64, 2)
    smaller['resource_estimate']['ram_bytes'] += 1
    smaller['resource_estimate']['vram_bytes'] += 1
    with pytest.raises(OperationFailure):
        _admit_detail(smaller, 64, 64, 2)


def test_detail_admission_accounts_for_every_pinned_mask_file():
    pixels = 64*64
    profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
        'resource_estimate': {'pixel_count': pixels,
            'ram_bytes': 128*pixels + 1073741824,
            'scratch_bytes': 144*pixels + 12*pixels + 8192 +
                             2*(4*pixels + 8192),
            'vram_bytes': 10_000_000_000, 'output_bytes': 8192}}
    _admit_detail(profile, 64, 64, 2)
    with pytest.raises(OperationFailure):
        _admit_detail(profile, 64, 64, 3)


def test_detail_requires_explicit_qualification_effect():
    extension = SimpleNamespace(portrait_detail=SimpleNamespace(enabled=True),
        region_adjustments=(), background=None, restoration=None)
    arm = SimpleNamespace(local=SimpleNamespace(enabled=False),
        beauty=SimpleNamespace(enabled=False),
        defects=SimpleNamespace(enabled=False), extensions=extension)
    styles = SimpleNamespace(baseline=arm, candidate=arm, approved=arm)
    assert 'portrait.detail' in required_learning_effects(styles)


def _detail_styles():
    def arm():
        extension = SimpleNamespace(
            portrait_detail=SimpleNamespace(enabled=True, strength=.5),
            region_adjustments=(), background=None, restoration=None,
            culling=None, series=None, composition=None,
            inpainting=None, generation=None)
        return SimpleNamespace(local=SimpleNamespace(enabled=False),
            beauty=SimpleNamespace(enabled=False),
            defects=SimpleNamespace(enabled=False),
            geometry=SimpleNamespace(enabled=False),
            extensions=extension)
    return SimpleNamespace(baseline=arm(), candidate=arm(), approved=arm())


def test_detail_pipeline_requires_matching_three_arms_and_evidence():
    styles = _detail_styles()
    validate_holdout_pipeline(styles, reviewed_detail=True)
    assert _same_detail_styles(styles) == styles.baseline.extensions.portrait_detail
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_EVIDENCE_REQUIRED'):
        validate_holdout_pipeline(styles)
    styles.candidate.extensions.portrait_detail.strength = .7
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_STYLE_MISMATCH'):
        validate_holdout_pipeline(styles, reviewed_detail=True)


def test_detail_pipeline_refuses_other_reviewed_effects():
    styles = _detail_styles()
    with pytest.raises(ValueError, match='HOLDOUT_EFFECT_EVIDENCE_CONFLICT'):
        validate_holdout_pipeline(styles, reviewed_detail=True,
                                  reviewed_dust=True)
    with pytest.raises(ValueError, match='HOLDOUT_EFFECT_EVIDENCE_CONFLICT'):
        validate_holdout_pipeline(styles, reviewed_detail=True,
                                  reviewed_defect=True)


@pytest.mark.parametrize('extra_guard', [False, True])
def test_detail_replay_uses_each_independent_arm_once(monkeypatch, extra_guard):
    import photo_worker.operations.learning.detail_replay as replay

    class Snapshot(SimpleNamespace):
        def model_copy(self, **kwargs):
            return self

    mask_ref = Snapshot(artifact_id=uuid4())
    extra_ref = Snapshot(artifact_id=uuid4())
    evidence_ref = Snapshot(artifact_id=uuid4())
    params_ref = Snapshot(artifact_id=uuid4())
    image_ref = Snapshot(artifact_id=uuid4())
    resolved_ref = Snapshot(artifact_id=uuid4())
    protection_ref = Snapshot(artifact_id=uuid4())
    detail = Snapshot(enabled=True, strength=.5)
    def arm_style():
        return Snapshot(extensions=Snapshot(portrait_detail=detail))
    styles = Snapshot(baseline=arm_style(), candidate=arm_style(),
                      approved=arm_style())
    image = Snapshot(canvas='canvas', transform_chain='chain',
                     color='color', dtype='float32')
    evidence = Snapshot(params=params_ref, image=image_ref,
                        resolved=resolved_ref, protection=protection_ref,
                        mask_artifacts=[mask_ref, extra_ref] if extra_guard else [mask_ref])
    params = Snapshot(protected=[Snapshot(artifact=mask_ref)] +
                      ([Snapshot(artifact=extra_ref)] if extra_guard else []),
                      regions=[])
    resolved = Snapshot(modules=Snapshot(extensions=Snapshot(
        portrait_detail=detail)))
    case = Snapshot(detail_evidence=evidence_ref)
    item = Snapshot(render_case=case, event_id=uuid4())
    source = Snapshot(rgb=np.zeros((4, 4, 3), np.float32),
        protected=np.zeros((4, 4), np.float32), image=image)
    arms = [np.full((4, 4, 3), value, np.float32)
            for value in (.2, .4, .6)]
    workspace = Snapshot(source=source, baseline=arms[0],
                         candidate=arms[1], approved=arms[2])
    documents = {id(evidence_ref): evidence, id(params_ref): params,
                 id(image_ref): image, id(resolved_ref): resolved,
                 id(protection_ref): Snapshot(artifact=mask_ref)}
    typed = []

    @contextmanager
    def input_file(ref, **kwargs):
        assert ref is mask_ref or extra_guard and ref is extra_ref
        yield Snapshot(path='extra.npy' if ref is extra_ref else 'mask.npy')

    @contextmanager
    def read_mask(path, shape, dtype, check_active, **kwargs):
        assert path in ('mask.npy', 'extra.npy') and shape == (4, 4)
        mask = np.zeros((4, 4), np.float32)
        if path == 'extra.npy':
            mask[0, 0] = 1
        yield mask

    @contextmanager
    def prepare(target, regions, module, read, protected, **kwargs):
        assert any(target is arm for arm in arms)
        assert read(mask_ref).shape == (4, 4)
        if extra_guard:
            assert read(extra_ref)[0, 0] == protected[0, 0] == 1
        result = target.copy() + np.float32(.1)
        result[protected > 0] = target[protected > 0]
        yield Snapshot(rgb=result)

    artifacts = Snapshot(contract=lambda ref, model: documents[id(ref)],
        require_declared_contract=lambda ref, contract: typed.append((ref, contract)),
        input=input_file)
    monkeypatch.setattr(replay, 'validate_holdout_detail_binding',
                        lambda *args: None)
    monkeypatch.setattr(replay, 'validate_frozen_detail_material',
                        lambda *args: None)
    monkeypatch.setattr(replay, 'read_array', read_mask)
    monkeypatch.setattr(replay, 'prepare_detail', prepare)
    profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
        'resource_estimate': {'pixel_count': 16,
            'ram_bytes': 128*16 + 1073741824,
            'scratch_bytes': 144*16 + 12*16 + 8192 +
                             (2 if extra_guard else 1)*(4*16 + 8192),
            'vram_bytes': 10_000_000_000, 'output_bytes': 8192}}
    with replay.open_holdout_portrait_detail(artifacts, item, workspace,
        styles, training_run_id=uuid4(), tenant_id=uuid4(),
        execution_profile=profile) as apply:
        for arm in arms:
            apply(arm)
        with pytest.raises(ValueError, match='HOLDOUT_DETAIL_ARM_INVALID'):
            apply(arms[0])
    for arm, value in zip(arms, (.2, .4, .6), strict=True):
        assert np.all(arm[1:] == np.float32(value) + np.float32(.1))
        expected = np.float32(value) if extra_guard else np.float32(value) + np.float32(.1)
        assert np.all(arm[0, 0] == expected)
    assert (mask_ref, 'NpyArray') in typed
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_ARMS_INCOMPLETE'):
        with replay.open_holdout_portrait_detail(artifacts, item, workspace,
            styles, training_run_id=uuid4(), tenant_id=uuid4(),
            execution_profile=profile) as apply:
            apply(arms[0])
