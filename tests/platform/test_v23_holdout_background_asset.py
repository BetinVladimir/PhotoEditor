"""Pin handoff tests; decoder/SQL/native qualification is independent."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import Background
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
from photo_worker.operations.learning import background_asset
from test_style_resolve import profile


@pytest.mark.parametrize('mode', ['valid', 'missing', 'style', 'portable', 'icc', 'mode', 'decoded_icc'])
def test_holdout_background_asset_requires_exact_pins_and_owns_scope(monkeypatch, mode):
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode='asset', asset_id='installed',
                                             blur_strength=0, protect_subject=True)
    style_ref = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    binding = FrozenBackgroundAsset(schema_version=1, portable_asset_id='installed',
        style_version_id=uuid4(), style=style_ref, source=ArtifactRef(artifact_id=uuid4(), sha256='b'*64),
        expected_bytes=10, media_type='image/png', suffix='.png', assume_profile=None, work_icc_sha='c'*64)
    if mode == 'missing':binding = None
    if mode == 'style':style_ref = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    if mode == 'portable':style.extensions.background.asset_id = 'different'
    if mode == 'mode':style.extensions.background.mode = 'keep'
    events = []
    execution = {'resource_estimate': {'ram_bytes': 123}}
    retained = {'ram_bytes': 1, 'scratch_bytes': 2}
    @contextmanager
    def normalize(artifacts, owned, registry, **kwargs):
        events.append('open')
        assert owned is not binding
        assert kwargs['execution_profile'] is not execution
        assert kwargs['retained_resources'] is not retained
        result = SimpleNamespace(image=SimpleNamespace(profile_sha256='d'*64 if mode=='decoded_icc' else 'c'*64))
        try:yield result
        finally:events.append('close')
    monkeypatch.setattr(background_asset, 'normalize_frozen_background', normalize)
    def invoke():
        with background_asset.open_holdout_background_asset(object(), style, style_ref, binding, object(),
                work_icc_sha='d'*64 if mode=='icc' else 'c'*64, execution_profile=execution,
                retained_resources=retained) as value:
            events.append('consumer')
            assert value.image.profile_sha256 == 'c'*64
    if mode == 'valid':
        invoke()
        assert events == ['open', 'consumer', 'close']
    else:
        with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_ASSET_'):invoke()
        assert events == (['open', 'close'] if mode=='decoded_icc' else [])


@pytest.mark.parametrize('alpha_kind', ['none', 'uint16', 'float32'])
def test_asset_holdout_tiles_match_production_geometry_and_linear_composition(alpha_kind):
    import numpy as np
    from photo_worker.v2.render_background_decode import NormalizedBackground
    from photo_worker.algorithms.background import asset_geometry, render_asset_tile, solid
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode='asset', asset_id='installed', blur_strength=0, protect_subject=True)
    style_ref = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    binding = FrozenBackgroundAsset(schema_version=1, portable_asset_id='installed',
        style_version_id=uuid4(), style=style_ref, source=ArtifactRef(artifact_id=uuid4(), sha256='b'*64),
        expected_bytes=10, media_type='image/png', suffix='.png', assume_profile=None, work_icc_sha='c'*64)
    rng = np.random.default_rng(2026)
    original = rng.random((9, 601, 3), dtype=np.float32)
    target = (original*.8).astype(np.float32)
    before = target.copy()
    alpha = None if alpha_kind=='none' else np.full((9,601), 20000 if alpha_kind=='uint16' else .3,
        dtype=np.uint16 if alpha_kind=='uint16' else np.float32)
    protected = np.zeros((9,601), np.float32);protected[2,510:515] = .01
    source = SimpleNamespace(rgb=original, alpha=alpha, protected=protected)
    subject = rng.random((9,601), dtype=np.float32)
    backdrop = rng.random((13,23,3), dtype=np.float32)
    normalized = NormalizedBackground(SimpleNamespace(profile_sha256='c'*64, height=13, width=23), backdrop)
    output_alpha = np.zeros((9,601), np.float32)
    geometry = asset_geometry(23,13,601,9,1,.5,.5)
    original_alpha = np.ones((9,601),np.float32) if alpha is None else alpha.astype(np.float32)/np.float32(65535) if alpha_kind=='uint16' else alpha
    expected = solid(before, subject*original_alpha, render_asset_tile(backdrop,(9,601),(0,9,0,601),geometry))
    expected[protected>0] = original[protected>0]
    expected_alpha = np.ones((9,601),np.float32);expected_alpha[protected>0] = original_alpha[protected>0]
    background_asset.render_holdout_asset_background(source,target,output_alpha,style,style_ref,binding,
        normalized,subject=subject,confidence=.9,work_icc_sha='c'*64)
    assert np.array_equal(target.view(np.uint32), expected.view(np.uint32))
    assert np.array_equal(output_alpha.view(np.uint32), expected_alpha.view(np.uint32))
