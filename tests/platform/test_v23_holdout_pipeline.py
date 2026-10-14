"""Composed native stages with model doubles, not GPU qualification."""
from contextlib import contextmanager
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.style import Beauty, Background
from photo_worker.operations.learning.recipe import FrozenHoldoutStyles
from photo_worker.operations.learning.workspace import open_holdout_workspace, render_holdout_triplet
from photo_worker.operations.learning.denoise import open_holdout_denoise
from photo_worker.operations.session.contracts import FaceObservations
from test_v23_holdout_denoise import setup


@pytest.mark.parametrize('cancel_at', [None, 'beauty', 'background'])
def test_full_native_holdout_order_and_cancellation_cleanup(tmp_path, monkeypatch, cancel_at):
    from photo_worker.operations.learning import color_render, beauty_render, background
    artifacts,case,base,provider,execution,model_events,engine = setup(tmp_path)
    base.beauty = Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=.4,
        tone_evenness=.1,texture_keep=1,skin_mask='face68_conservative_v1',protect_features=True)
    base.extensions.background = Background(mode='blur',asset_id=None,blur_strength=.5,
                                            protect_subject=True)
    styles = []
    for strength in (.2,.5,.8):
        style = base.model_copy(deep=True)
        style.strength = strength
        styles.append(style)
    before = FaceObservations(schema_version=1,binding=case.binding,faces=[])
    order = []
    originals = dict(color=color_render.render_materialized_style,
                     beauty=beauty_render.render_holdout_beauty,
                     background=background.render_holdout_background)
    for name,module,attribute in (
            ('color',color_render,'render_materialized_style'),
            ('beauty',beauty_render,'render_holdout_beauty'),
            ('background',background,'render_holdout_background')):
        def wrapped(*args,_name=name,**kwargs):
            order.append(_name)
            if cancel_at == _name:
                raise RuntimeError('lease cancelled')
            return originals[_name](*args,**kwargs)
        monkeypatch.setattr(module,attribute,wrapped)
    try:
        with open_holdout_workspace(artifacts,case,artifacts.image.color.icc_sha256,
                                    lambda:None) as workspace:
            @contextmanager
            def source(style):
                order.append('denoise-open')
                try:
                    with open_holdout_denoise(artifacts,case.working_image,workspace.source.rgb,
                            style,provider,model_lock_sha='c'*64,execution_profile=execution) as rgb:
                        yield rgb
                finally:
                    order.append('denoise-close')
            args = dict(person_reliable=False,artifacts=artifacts,observations=before,
                binding=case.binding,prepare_source=source,
                background_subject=np.full(artifacts.rgb.shape[:2],.8,np.float32),
                background_confidence=.9,background_depth=np.linspace(0,1,
                    artifacts.rgb.shape[0]*artifacts.rgb.shape[1],dtype=np.float32).reshape(artifacts.rgb.shape[:2]))
            if cancel_at is None:
                render_holdout_triplet(workspace,FrozenHoldoutStyles(*styles),**args)
                assert order == ['denoise-open','color','denoise-close','beauty','background']*3
                assert np.all(workspace.alpha == np.float32(10000)/np.float32(65535))
                for target in (workspace.baseline,workspace.candidate,workspace.approved):
                    assert np.isfinite(target).all()
            else:
                with pytest.raises(RuntimeError,match='lease cancelled'):
                    render_holdout_triplet(workspace,FrozenHoldoutStyles(*styles),**args)
                assert order.count('denoise-open') == 1
                assert order.count('denoise-close') == 1
                assert order[-1] == cancel_at
        assert list(artifacts.scratch.directory.iterdir()) == []
        assert sum(isinstance(x,tuple) and x[0]=='open' for x in model_events) == sum(
            isinstance(x,tuple) and x[0]=='close' for x in model_events)
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('mode', ['different_rule', 'missing', 'extra', 'duplicate', 'shape', 'dtype', 'nan', 'infinity', 'range'])
def test_triplet_region_admission_precedes_model_callbacks(tmp_path, mode):
    from photo_worker.contracts.style import RegionAdjustment
    artifacts, case, base, provider, execution, events, engine = setup(tmp_path)
    rule = RegionAdjustment(region_id='region', selector='manual', ev=.2,
                            saturation=1, hue_deg=0, strength=1, priority=0)
    base.extensions.region_adjustments = [rule]
    styles = [base.model_copy(deep=True) for _ in range(3)]
    masks = {'region': np.ones(artifacts.rgb.shape[:2], np.float32)}
    if mode == 'different_rule':
        styles[2].extensions.region_adjustments[0].ev = .3
    elif mode == 'missing':
        masks.clear()
    elif mode == 'extra':
        masks['other'] = masks['region']
    elif mode == 'duplicate':
        for style in styles:
            style.extensions.region_adjustments.append(rule.model_copy(deep=True))
    elif mode == 'shape':
        masks['region'] = np.ones((1, 1), np.float32)
    elif mode == 'dtype':
        masks['region'] = masks['region'].astype(np.float64)
    if mode in ('nan', 'infinity', 'range'):
        masks['region'][-1, -1] = {'nan': np.nan, 'infinity': np.inf, 'range': 1.01}[mode]
    try:
        with open_holdout_workspace(artifacts, case, artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            with pytest.raises(ValueError, match='HOLDOUT_REGION_BINDING_MISMATCH|INVALID_HOLDOUT_MASK'):
                render_holdout_triplet(workspace, FrozenHoldoutStyles(*styles),
                    person_reliable=False, region_masks=masks,
                    prepare_source=lambda style: pytest.fail('model callback reached'))
        assert not any(isinstance(event, tuple) and event[0] == 'open' for event in events)
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


def test_composed_denoise_and_regional_triplet_matches_pixel_math(tmp_path):
    from photo_worker.contracts.style import RegionAdjustment
    from photo_worker.algorithms.style import resolve_style
    from photo_worker.algorithms.color import apply_global, apply_local, apply_strength
    from photo_worker.algorithms.regions import apply_regions
    from photo_worker.operations.image.prepare_denoise import prepare_denoise
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.runtime.arrays import read_array
    artifacts, case, base, provider, execution, events, engine = setup(tmp_path)
    base.mode = 'fixed'
    base.extensions.region_adjustments = [RegionAdjustment(region_id='region', selector='manual',
        ev=.4, saturation=1.1, hue_deg=2, strength=.7, priority=0)]
    styles = []
    for strength in (.2, .5, .8):
        style = base.model_copy(deep=True)
        style.strength = strength
        styles.append(style)
    mask = np.linspace(0, 1, artifacts.rgb.shape[0]*artifacts.rgb.shape[1],
                       dtype=np.float32).reshape(artifacts.rgb.shape[:2])
    try:
        expected = []
        for style in styles:
            params = DenoiseParams(image=case.working_image,
                strength=style.extensions.restoration.denoise_strength*style.strength,
                model_lock_id='c'*64)
            with prepare_denoise(artifacts.rgb, params, engine, artifacts.scratch) as prepared:
                with read_array(prepared.path, artifacts.rgb.shape, 'float32', lambda: None,
                                unit_interval=True) as denoised:
                    resolved = resolve_style(style, person_reliable=False, strict_ood=False)
                    assert resolved.fixed is not None
                    colored = apply_global(denoised, resolved.fixed)
                    local = apply_local(colored, None, resolved.local)
                    regional = apply_regions(local, [(style.extensions.region_adjustments[0], mask)]).rgb
                    expected.append(apply_strength(denoised, regional, style.strength))
        with open_holdout_workspace(artifacts, case, artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            @contextmanager
            def source(style):
                with open_holdout_denoise(artifacts, case.working_image, workspace.source.rgb,
                        style, provider, model_lock_sha='c'*64, execution_profile=execution) as rgb:
                    yield rgb
            render_holdout_triplet(workspace, FrozenHoldoutStyles(*styles), person_reliable=False,
                region_masks={'region': mask}, prepare_source=source)
            for target, pixels in zip((workspace.baseline, workspace.candidate, workspace.approved), expected):
                assert np.array_equal(target.view(np.uint32), pixels.view(np.uint32))
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('mode', ['valid', 'missing_pin', 'wrong_icc'])
def test_asset_background_triplet_requires_pin_before_color(tmp_path, monkeypatch, mode):
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
    from photo_worker.v2.render_background_decode import NormalizedBackground
    from photo_worker.operations.learning import color_render
    from types import SimpleNamespace
    artifacts, case, base, provider, execution, events, engine = setup(tmp_path)
    base.extensions.restoration = None
    base.extensions.background = Background(mode='asset', asset_id='installed', blur_strength=0, protect_subject=True)
    style_ref = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    pin = FrozenBackgroundAsset(schema_version=1, portable_asset_id='installed', style_version_id=uuid4(),
        style=style_ref, source=ArtifactRef(artifact_id=uuid4(), sha256='b'*64), expected_bytes=10,
        media_type='image/png', suffix='.png', assume_profile=None, work_icc_sha=artifacts.image.color.icc_sha256)
    normalized = NormalizedBackground(SimpleNamespace(profile_sha256=pin.work_icc_sha, height=11, width=17),
        np.full((11,17,3), .25, np.float32))
    calls = []
    original = color_render.render_materialized_style
    def color(*args, **kwargs):
        calls.append('color')
        return original(*args, **kwargs)
    monkeypatch.setattr(color_render, 'render_materialized_style', color)
    try:
        with open_holdout_workspace(artifacts, case, artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            args = dict(person_reliable=False, background_subject=np.full(artifacts.rgb.shape[:2], .8, np.float32),
                background_confidence=.9, background_asset=normalized, background_pin=None if mode=='missing_pin' else pin,
                background_style_ref=style_ref, work_icc_sha='d'*64 if mode=='wrong_icc' else pin.work_icc_sha)
            if mode == 'valid':
                render_holdout_triplet(workspace, FrozenHoldoutStyles(base,base,base), **args)
                assert calls == ['color']*3
                assert np.array_equal(workspace.baseline.view(np.uint32), workspace.candidate.view(np.uint32))
                assert np.array_equal(workspace.baseline.view(np.uint32), workspace.approved.view(np.uint32))
                protected = workspace.source.protected > 0
                assert np.array_equal(workspace.candidate[protected], workspace.source.rgb[protected])
            else:
                with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH'):
                    render_holdout_triplet(workspace, FrozenHoldoutStyles(base,base,base), **args)
                assert calls == []
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('arm', [0, 1, 2])
@pytest.mark.parametrize('effect', ['upscale', 'sensor_dust', 'generate'])
def test_incomplete_pipeline_refuses_before_any_render_callback(tmp_path, monkeypatch, arm, effect):
    from photo_worker.operations.learning import color_render
    artifacts, case, base, provider, execution, events, engine = setup(tmp_path)
    styles = [base.model_copy(deep=True) for _ in range(3)]
    if effect == 'generate':
        styles[arm].extensions.background = Background(mode='generate', asset_id=None,
            blur_strength=0, protect_subject=True)
    else:
        setattr(styles[arm].extensions.restoration, effect,
                '2x' if effect == 'upscale' else 'review')
    monkeypatch.setattr(color_render, 'render_materialized_style',
        lambda *a, **kw: pytest.fail('color called before complete pipeline admission'))
    @contextmanager
    def prepare(style):
        pytest.fail('model acquired before complete pipeline admission')
        yield None
    try:
        with open_holdout_workspace(artifacts, case, artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            with pytest.raises(ValueError, match='HOLDOUT_(COMPLETE_RESTORATION_REQUIRED|RENDER_REQUIRES_EFFECT_PIPELINE)'):
                render_holdout_triplet(workspace, FrozenHoldoutStyles(*styles),
                    person_reliable=False, prepare_source=prepare)
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()
