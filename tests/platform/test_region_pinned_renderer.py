from contextlib import contextmanager
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.operations.region.renderer import PinnedRegionRenderer
from photo_worker.runtime.executor import OperationFailure
from test_region_runtime_lock import lock


def provider(tmp_path):
    pinned = lock()
    path = tmp_path / 'runtime.json'
    path.write_bytes(canonical_bytes(pinned))
    events = []
    @contextmanager
    def render(*args):
        events.append('render')
        try:
            yield object()
        finally:
            events.append('release')
    delegate = SimpleNamespace(renderer_version='v2.2', runtime_sha256=canonical_hash(pinned),
        model_lock_sha='a' * 64, icc_lock_sha='b' * 64, backend='cuda', icc_root=tmp_path,
        supported_modules=frozenset({'color'}), verify_runtime=lambda: events.append('qualify'),
        admit_resources=lambda *args: events.append('admit'), render=render)
    return delegate, path, events


def test_pinned_provider_verifies_real_build_and_retains_context_lifetime(tmp_path):
    delegate, path, events = provider(tmp_path)
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    ctx = SimpleNamespace(check_active=lambda: None)
    installed.admit_resources(ctx, None)
    with installed.render(ctx, None, None, None, None):
        assert events[-1] == 'render'
    assert events == ['qualify', 'qualify', 'admit', 'qualify', 'render', 'release']
    assert installed.backend == 'cuda'


def test_changed_manifest_fails_before_provider_admission(tmp_path):
    delegate, path, events = provider(tmp_path)
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    path.write_bytes(b'{}')
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        installed.admit_resources(SimpleNamespace(check_active=lambda: None), None)
    assert events == ['qualify']


def test_provider_pin_change_inside_context_releases_resources(tmp_path):
    delegate, path, events = provider(tmp_path)
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        with installed.render(SimpleNamespace(check_active=lambda: None), None, None, None, None):
            delegate.model_lock_sha = 'c' * 64
    assert events[-1] == 'release'


def test_provider_verification_cannot_leave_a_changed_runtime_manifest(tmp_path):
    delegate, path, events = provider(tmp_path)
    def qualify():
        events.append('qualify')
        path.write_bytes(b'{}')
    delegate.verify_runtime = qualify
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    assert events == ['qualify']


@pytest.mark.parametrize('change_after_render', [False, True])
def test_pinned_provider_runs_region_adapter_and_checks_before_staging(tmp_path, change_after_render):
    from uuid import uuid4
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.region.adapter import execute
    from test_region_preview_adapter import Renderer, Artifacts, Admission, context
    from test_region_preview_presentation import fixture
    pinned = lock()
    path = tmp_path / 'runtime.json'
    path.write_bytes(canonical_bytes(pinned))
    delegate = Renderer()
    delegate.backend = 'cuda'
    delegate.runtime_sha256 = canonical_hash(pinned)
    original = delegate.render
    @contextmanager
    def render(*args):
        with original(*args) as canvas:
            yield canvas
        if change_after_render:
            path.write_bytes(b'{}')
    delegate.render = render
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    params, body = fixture()
    params.source.bytes = len(body)
    admission = Admission()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 8*1024*1024, min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch, params, body)
        if change_after_render:
            with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
                execute(context(installed), artifacts, admission, installed)
            assert artifacts.outputs == {}
        else:
            result = execute(context(installed), artifacts, admission, installed)
            assert len(result.outputs) == 2
        assert admission.calls == 1
        assert artifacts.reads == ['params', 'recipe', 'source', 'style']
        assert 'release' in delegate.events


@pytest.mark.parametrize('budget', [None, True, 0, -1, 2**53, '100000'])
def test_mapped_renderer_rejects_unadmitted_scratch_before_input_io(tmp_path, budget):
    delegate, path, events = provider(tmp_path)
    def forbidden(*args):
        pytest.fail('pipeline called before scratch admission')
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256,
        render_pipeline=forbidden, max_input_bytes=100000)
    ctx = SimpleNamespace(check_active=lambda: None,
        lease=SimpleNamespace(execution_profile={'resource_estimate':{'scratch_bytes':budget}}))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        with installed.render(ctx, None, None, None, None):
            pytest.fail('unadmitted inputs yielded')
    assert events == ['qualify', 'qualify']


@pytest.mark.parametrize('method', ['verify_runtime', 'admit_resources', 'render'])
def test_provider_callback_replacement_fails_before_execution(tmp_path, method):
    delegate, path, events = provider(tmp_path)
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    def replaced(*args):
        pytest.fail('unqualified replacement executed')
    setattr(delegate, method, replaced)
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        installed.admit_resources(SimpleNamespace(check_active=lambda: None), None)
    assert events == ['qualify']


def test_provider_callback_change_inside_render_releases_context(tmp_path):
    delegate, path, events = provider(tmp_path)
    installed = PinnedRegionRenderer(delegate, path, delegate.runtime_sha256)
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        with installed.render(SimpleNamespace(check_active=lambda: None), None, None, None, None):
            delegate.render = lambda *args: pytest.fail('replacement executed')
    assert events[-1] == 'release'


def test_pinned_provider_accepts_fresh_bound_method_objects(tmp_path):
    delegate, path, events = provider(tmp_path)
    class BoundProvider:
        def verify_runtime(self):
            events.append('bound-qualify')
        def admit_resources(self, *args):
            events.append('bound-admit')
        def render(self, *args):
            return delegate.render(*args)
    bound = BoundProvider()
    for field in ('renderer_version', 'runtime_sha256', 'model_lock_sha',
                  'icc_lock_sha', 'backend', 'icc_root', 'supported_modules'):
        setattr(bound, field, getattr(delegate, field))
    installed = PinnedRegionRenderer(bound, path, bound.runtime_sha256)
    ctx = SimpleNamespace(check_active=lambda: None)
    installed.admit_resources(ctx, None)
    with installed.render(ctx, None, None, None, None):
        assert events[-1] == 'render'
    assert events == ['bound-qualify', 'bound-qualify', 'bound-admit',
                      'bound-qualify', 'render', 'release']


def test_provider_cannot_mutate_admitted_region_params(tmp_path):
    from test_region_preview_presentation import fixture
    delegate,path,events=provider(tmp_path)
    params,_=fixture()
    before=canonical_bytes(params)
    @contextmanager
    def render(ctx,artifacts,owned,source,style):
        owned.source.bytes+=1
        try:yield object()
        finally:events.append('release-mutated')
    delegate.render=render
    installed=PinnedRegionRenderer(delegate,path,delegate.runtime_sha256)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        with installed.render(SimpleNamespace(check_active=lambda:None),None,params,None,None):
            pytest.fail('changed region parameters yielded')
    assert canonical_bytes(params)==before and events[-1]=='release-mutated'


def test_provider_resource_admission_cannot_replace_region_params(tmp_path):
    from test_region_preview_presentation import fixture
    delegate,path,events=provider(tmp_path)
    params,_=fixture();before=canonical_bytes(params)
    def admit(ctx,owned):owned.source.bytes+=1
    delegate.admit_resources=admit
    installed=PinnedRegionRenderer(delegate,path,delegate.runtime_sha256)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        installed.admit_resources(SimpleNamespace(check_active=lambda:None),params)
    assert canonical_bytes(params)==before


@pytest.mark.parametrize('stage',['admit','render'])
def test_region_provider_cannot_replace_unmapped_task_profile(tmp_path,stage):
    delegate,path,events=provider(tmp_path)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'tile_size':512}))
    def admit(ctx,params):ctx.lease.execution_profile['tile_size']=256
    @contextmanager
    def render(ctx,*args):
        ctx.lease.execution_profile['tile_size']=256
        try:yield object()
        finally:events.append('profile-release')
    if stage=='admit':delegate.admit_resources=admit
    else:delegate.render=render
    installed=PinnedRegionRenderer(delegate,path,delegate.runtime_sha256)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        if stage=='admit':installed.admit_resources(ctx,None)
        else:
            with installed.render(ctx,None,None,None,None):pytest.fail('changed profile yielded')
    if stage=='render':assert events[-1]=='profile-release'
