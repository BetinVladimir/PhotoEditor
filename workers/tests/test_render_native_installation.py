from pathlib import Path
from types import SimpleNamespace
import pytest
from photo_worker.v2.render_native_installation import NativeRenderInstallation
from photo_worker.v2.render_qualification_pin import RenderQualificationPin


def installation(monkeypatch, *, accepted=True):
    monkeypatch.setattr('photo_worker.v2.render_native_installation.load_render_runtime_lock', lambda path, sha: None)
    pin = RenderQualificationPin(model_lock_sha='a'*64, runtime_lock_sha='b'*64,
        backend='mps', device_profile_sha='c'*64, dataset_sha='d'*64, calibration_sha='e'*64)
    return NativeRenderInstallation(runtime_path=Path('/tmp/approved-render.lock'), qualification_pin=pin,
        icc_lock_sha='f'*64, verify_qualification=lambda _: accepted,
        provider_peak=dict(ram_bytes=1 << 30, scratch_bytes=1 << 30, vram_bytes=0, output_bytes=0),
        max_input_bytes=1 << 30, max_mask_bytes=1 << 28, max_tile_bytes=512*512*256)


@pytest.mark.parametrize('answer', [False, None, 1])
def test_native_installation_requires_literal_current_qualification_before_worker_or_replay(monkeypatch, answer):
    installed = installation(monkeypatch, accepted=answer)
    with pytest.raises(ValueError, match='QUALIFICATION_UNAVAILABLE'): installed.worker(object())
    with pytest.raises(ValueError, match='QUALIFICATION_UNAVAILABLE'): installed.replay(Path('/tmp'), 1 << 30)


def test_worker_and_replay_share_exact_native_inventory_runtime_and_module_coverage(monkeypatch):
    installed = installation(monkeypatch)
    inputs = SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(overrides=[])))
    profile = {'backend': 'mps', 'tile_size': 512}
    context = SimpleNamespace(lease=SimpleNamespace(execution_profile=profile), check_active=lambda: None)
    worker = installed.assembly().worker(context, object(), inputs)
    scoped = SimpleNamespace(execution_profile=lambda: profile)
    replay = installed.assembly().replay(object(), inputs, scoped, check_active=lambda: None)
    expected = {('defects.auto_repair', '1'), ('sensor.dust_auto_repair', '1'), ('portrait.detail', '1')}
    assert {(op.operation, op.version) for op in worker['operators']} == expected
    assert {(op.operation, op.version, op.runtime_sha256) for op in worker['operators']} == {(op.operation, op.version, op.runtime_sha256) for op in replay['operators']}
    assert worker['preparation'] == replay['preparation']
    assert worker['preparation']['supported_modules'] == frozenset({'color', 'strength'})
    assert worker['scalar_overrides'] == replay['scalar_overrides'] == ()
    assert callable(worker['measure']) and 'measure' not in replay


def test_sql_task_context_is_required_before_native_provider_installation(monkeypatch):
    installed = installation(monkeypatch)
    class Database:
        def render_cache_context(self, lease, raw): return None
    renderer = installed.worker(Database())
    monkeypatch.setattr('photo_worker.v2.render_native_installation.read_render_document', lambda *args: b'{}')
    context = SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(params=object())), check_active=lambda: None)
    from photo_worker.runtime.executor import OperationFailure
    with pytest.raises(OperationFailure): renderer.load_providers(context, object(), object())


def test_native_replay_refuses_missing_or_invalid_scratch_policy(monkeypatch):
    installed = installation(monkeypatch)
    with pytest.raises(ValueError, match='SCRATCH_INVALID'): installed.replay(None, 1 << 30)
    with pytest.raises(ValueError, match='SCRATCH_INVALID'): installed.replay(Path('/tmp'), True)
