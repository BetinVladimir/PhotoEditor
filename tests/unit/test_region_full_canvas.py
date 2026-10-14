from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import json

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import StyleV2
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.region.adapter import RenderedRegionCanvas
from photo_worker.operations.region.mapped import prepare_region_full_canvas
from test_layer_mask_inputs import fixture
from test_layer_prepare import scratch


@pytest.mark.parametrize('pinned,legacy,mutate', [(p,l,m) for p in (False,True) for l in (False,True) for m in ('none','style','source','release','profile') if m!='profile' or p])
def test_full_canvas_keeps_verified_inputs_and_releases_after_pipeline(tmp_path, legacy, mutate, pinned):
    image, *_ = fixture(tmp_path)
    image.alpha = ArtifactRef(artifact_id=uuid4(), sha256='d' * 64)
    rgb, alpha = tmp_path / 'rgb.npy', tmp_path / 'alpha.npy'
    np.save(rgb, np.full((1, 2, 3), .5, np.float32))
    np.save(alpha, np.array([[0, 65535]], np.uint16) if legacy else np.array([[0, 1]], np.float32))
    root = Path(__file__).resolve().parents[2]
    raw = json.loads((root / 'docs/implementation/contracts/examples/style-fixed.example.json').read_text())
    raw['quality']['status'] = 'needs_review'
    style = StyleV2.model_validate(raw)
    events = []
    space = scratch(tmp_path)
    class Artifacts:
        scratch = space
        @contextmanager
        def input(self, ref, **kwargs):
            events.append(('open', ref))
            if mutate == 'profile':
                ctx.lease.execution_profile['resource_estimate']['scratch_bytes'] = 200000
            try:
                yield SimpleNamespace(path=rgb if ref == image.pixels else alpha)
            finally:
                events.append(('close', ref))
    @contextmanager
    def pipeline(metadata, effective, original):
        assert metadata==image and effective==style
        if pinned:
            assert metadata is not image and effective is not style
        else:
            assert metadata is image and effective is style
        assert not original.rgb.flags.writeable
        np.testing.assert_array_equal(original.alpha, [[0, 1]])
        events.append('pipeline')
        try:
            if mutate == 'style':
                effective.strength = .123
            elif mutate == 'source':
                metadata.canvas.width = 3
            yield RenderedRegionCanvas(np.array(original.rgb, copy=True), original.alpha)
        finally:
            events.append('release')
            if mutate == 'release':
                effective.strength = .123
    if pinned:
        from importlib import metadata
        import platform, sys
        from photo_worker.contracts.base import canonical_bytes, canonical_hash
        from photo_worker.operations.region.renderer import PinnedRegionRenderer
        from photo_worker.operations.region import runtime_lock
        manifest = runtime_lock.RegionRuntimeLock(schema_version=1,
            python_version='.'.join(map(str, sys.version_info[:3])), implementation='CPython',
            machine=platform.machine(), packages={name: metadata.version(name) for name in ('numpy','pydantic','rfc8785','Pillow')},
            components=runtime_lock.installed_component_hashes())
        path = tmp_path / 'runtime.json'; path.write_bytes(canonical_bytes(manifest))
        delegate = SimpleNamespace(renderer_version='v2.2', backend='cuda', runtime_sha256=canonical_hash(manifest),
            model_lock_sha='a'*64, icc_lock_sha='b'*64, icc_root=tmp_path, supported_modules=frozenset({'color'}),
            verify_runtime=lambda: None, admit_resources=lambda *args: None,
            render=lambda *args: pytest.fail('unmapped delegate render invoked'))
        renderer = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256,
            render_pipeline=lambda ctx, artifacts, params, metadata, effective, original: pipeline(metadata, effective, original),
            max_input_bytes=100000)
        ctx = SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(execution_profile={'resource_estimate':{'scratch_bytes':100000}}))
        prepare = lambda: renderer.render(ctx, Artifacts(), None, image, style)
    else:
        prepare = lambda: prepare_region_full_canvas(Artifacts(), image, style, pipeline, max_input_bytes=100000)
    try:
        if mutate != 'none':
            failure = 'CONFIG_INVALID' if mutate == 'profile' else 'SOURCE_METADATA_CHANGED' if mutate == 'source' else 'EFFECTIVE_STYLE_CHANGED'
            with pytest.raises(OperationFailure if mutate == 'profile' else ValueError, match=failure):
                with prepare():
                    if mutate != 'release':
                        pytest.fail('changed inputs yielded')
        else:
            with prepare() as full:
                assert full.rgb.shape == (1, 2, 3)
                assert not any(isinstance(event, tuple) and event[0] == 'close' for event in events)
        if mutate == 'profile':
            assert 'pipeline' not in events
        else:
            assert events.index('release') < next(i for i, event in enumerate(events) if isinstance(event, tuple) and event[0] == 'close')
        assert len([event for event in events if isinstance(event, tuple) and event[0] == 'close']) == (1 if mutate == 'profile' else 2)
    finally:
        space.cleanup()


@pytest.mark.parametrize('mutate', ['style', 'source'])
def test_input_loading_drift_prevents_pipeline_invocation(tmp_path, mutate):
    image, *_ = fixture(tmp_path)
    rgb = tmp_path / 'rgb.npy'
    np.save(rgb, np.full((1, 2, 3), .5, np.float32))
    root = Path(__file__).resolve().parents[2]
    raw = json.loads((root / 'docs/implementation/contracts/examples/style-fixed.example.json').read_text())
    raw['quality']['status'] = 'needs_review'
    style = StyleV2.model_validate(raw)
    space = scratch(tmp_path)
    closed = []
    class Artifacts:
        scratch = space
        @contextmanager
        def input(self, ref, **kwargs):
            if mutate == 'style':
                style.strength = .123
            else:
                image.canvas.source_sha256 = '0' * 64
            try:
                yield SimpleNamespace(path=rgb)
            finally:
                closed.append(ref)
    def forbidden(*args):
        pytest.fail('render pipeline called with changed inputs')
    try:
        failure = 'EFFECTIVE_STYLE_CHANGED' if mutate == 'style' else 'SOURCE_METADATA_CHANGED'
        with pytest.raises(ValueError, match=failure):
            with prepare_region_full_canvas(Artifacts(), image, style, forbidden, max_input_bytes=100000):
                pytest.fail('changed inputs yielded')
        assert closed == [image.pixels]
    finally:
        space.cleanup()
