from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.algorithms.background import solid, transparent
from photo_worker.contracts.style import Background
from photo_worker.operations.learning.background import render_holdout_background
from test_style_resolve import profile


def fixture(mode, alpha_kind):
    rgb = np.linspace(.05, .95, 36, dtype=np.float32).reshape(3, 4, 3)
    opacity = np.linspace(0, 1, 12, dtype=np.float32).reshape(3, 4)
    alpha = None if alpha_kind == 'none' else (np.rint(opacity*65535).astype(np.uint16)
            if alpha_kind == 'uint16' else opacity)
    protected = np.zeros((3, 4), np.float32)
    protected[1, 2] = .001
    source = SimpleNamespace(rgb=rgb.copy(), alpha=alpha, protected=protected)
    subject = np.linspace(0, 1, 12, dtype=np.float32).reshape(3, 4)
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode=mode, asset_id=None,
        solid_rgb=[.2, .3, .4] if mode == 'solid' else None,
        blur_strength=.5, protect_subject=True)
    return source, subject, style


@pytest.mark.parametrize('mode', ['solid', 'transparent'])
@pytest.mark.parametrize('alpha_kind', ['none', 'uint16', 'float32'])
def test_linear_composition_and_exact_positive_protection(mode, alpha_kind):
    source, subject, style = fixture(mode, alpha_kind)
    target = source.rgb.copy()
    alpha_out = np.zeros(subject.shape, np.float32)
    original = (np.ones(subject.shape, np.float32) if source.alpha is None else
                source.alpha.astype(np.float32)/np.float32(65535)
                if source.alpha.dtype == np.uint16 else source.alpha)
    coverage = subject*original
    expected = (solid(target, coverage, style.extensions.background.solid_rgb)
                if mode == 'solid' else transparent(target, coverage))
    expected_alpha = np.ones_like(coverage) if mode == 'solid' else coverage.copy()
    mask = source.protected > 0
    expected[mask] = source.rgb[mask]
    expected_alpha[mask] = original[mask]
    render_holdout_background(source, target, alpha_out, style, subject=subject, confidence=.9)
    assert np.array_equal(target.view(np.uint32), expected.view(np.uint32))
    assert np.array_equal(alpha_out.view(np.uint32), expected_alpha.view(np.uint32))
    assert np.array_equal(source.rgb[mask], target[mask])


@pytest.mark.parametrize('mode', ['blur', 'generate'])
def test_missing_depth_or_generation_never_silently_skips(mode):
    source, subject, style = fixture(mode, 'none')
    target = source.rgb.copy()
    with pytest.raises(ValueError, match='HOLDOUT_COMPLETE_BACKGROUND_REQUIRED'):
        render_holdout_background(source, target, np.ones(subject.shape, np.float32),
                                  style, subject=subject, confidence=.9)
    assert np.array_equal(target, source.rgb)


@pytest.mark.parametrize('confidence', [0.74, float('nan'), True, None])
def test_weak_or_invalid_subject_refuses_before_composition(confidence):
    source, subject, style = fixture('solid', 'none')
    target = source.rgb.copy()
    with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_SUBJECT_REQUIRED'):
        render_holdout_background(source, target, np.ones(subject.shape, np.float32),
                                  style, subject=subject, confidence=confidence)
    assert np.array_equal(target, source.rgb)


def test_triplet_rejects_different_backgrounds_before_callbacks():
    from photo_worker.operations.learning.recipe import FrozenHoldoutStyles
    from photo_worker.operations.learning.workspace import render_holdout_triplet
    _, _, base = fixture('solid', 'none')
    candidate = base.model_copy(deep=True)
    candidate.extensions.background.solid_rgb = [.7, .8, .9]
    callbacks = []
    with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_BINDING_MISMATCH'):
        render_holdout_triplet(None, FrozenHoldoutStyles(base, candidate, base),
                              person_reliable=False, check_active=lambda: callbacks.append(True))
    assert callbacks == []


def test_holdout_blur_matches_production_and_restores_protection(tmp_path):
    from uuid import uuid4
    from photo_worker.operations.background.prepare_blur import prepare_blur
    from photo_worker.runtime.arrays import read_array
    from photo_worker.runtime.scratch import ScratchSpace
    source,subject,style = fixture('blur','float32')
    subject[:] = .8
    depth = np.linspace(0,1,12,dtype=np.float32).reshape(3,4)
    root = tmp_path/'blur-scratch'
    root.mkdir()
    scratch = ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0)
    style.strength = .5
    target = source.rgb.copy()
    alpha = source.alpha.copy()
    try:
        with prepare_blur(target,subject,depth,scratch,strength=.25) as prepared:
            with read_array(prepared.rgb,target.shape,'float32',lambda:None,unit_interval=True) as pixels:
                expected = pixels.copy()
        expected[source.protected>0] = source.rgb[source.protected>0]
        render_holdout_background(source,target,alpha,style,subject=subject,confidence=.9,
                                  depth=depth,artifacts=SimpleNamespace(scratch=scratch))
        assert np.array_equal(target.view(np.uint32),expected.view(np.uint32))
        assert np.array_equal(alpha.view(np.uint32),source.alpha.view(np.uint32))
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()
