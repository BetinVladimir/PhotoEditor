"""Installer callback wiring with synthetic authority, not runtime qualification."""
from contextlib import contextmanager
from copy import deepcopy
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4, uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.operations.semantic import preview_retouch_installation as module


def fixture(monkeypatch):
    root = Path(__file__).resolve().parents[1]
    raw = (root/'backend/internal/semanticreview/testdata/retouch_preview_params.json').read_bytes().strip()
    params = SemanticRetouchPreviewInput.model_validate_json(raw)
    context_body = (root/'backend/internal/semanticreview/testdata/retouch_preview_context.json').read_bytes().strip()
    context_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(context_body).hexdigest())
    ref = lambda: dict(artifact_id=str(uuid4()), sha256='f'*64)
    pin = dict(policy_id=str(uuid4()), qualification_id=str(uuid4()), issuer_id=str(uuid4()),
        policy_sha='a'*64, report=ref(), calibration=ref(), backend='cpu', dataset_sha='b'*64,
        model_lock_sha=params.model_lock_sha, runtime_lock_sha=params.runtime_lock_sha,
        device_profile_sha=params.device_profile_sha)
    profile = dict(qualification=pin, backend='cpu', renderer_version='v2.2',
        retouch_context=context_ref.model_dump(mode='json'), model_lock_id=params.model_lock_sha,
        resource_estimate=dict(ram_bytes=1 << 33, vram_bytes=0),
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    params_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    run = uuid4()
    command = SimpleNamespace(operation='semantic.preview_retouch_region', operation_version='1',
        resource_class='cpu', tenant_id=params.tenant_id, run_id=run,
        task_id=uuid5(run, 'semantic.preview_retouch_region:'+str(params.target_id)),
        params=params_ref, inputs=[params_ref, context_ref, params.plan, params.intent,
            params.candidate, params.source.source, params.protection])
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command, execution_profile=profile, fence=7),
        check_active=lambda:None)
    calls = []
    def admitted(owner, body):
        assert body == raw and owner == ctx.lease
        owner.fence = 99
        calls.append('admit')
        return True
    db = SimpleNamespace(admit_semantic_retouch_preview=admitted,
        retouch_preview_report_evidence=lambda *a:None,
        retouch_preview_landmark_origin=lambda *a:True,
        retouch_preview_protection_origin=lambda *a:True)
    def contract_bytes(supplied, model):
        if supplied == context_ref:
            calls.append('context')
            return context_body
        assert supplied == params_ref and model is SemanticRetouchPreviewInput
        calls.append('params')
        return raw
    artifacts = SimpleNamespace(contract_bytes=contract_bytes,
        declared_input_refs=lambda maximum:tuple(ctx.lease.command.inputs))
    options = dict(database=db, objects=SimpleNamespace(get_object=lambda *a:None), bucket='photos',
        icc_registry=None, max_ram_bytes=1 << 34,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    monkeypatch.setattr(module, 'local_exposure_icc_verifier', lambda _:lambda color:True)
    def qualification_loader(database, lease, objects, bucket, **kwargs):
        assert kwargs['params'] == params and kwargs['context_ref'] == context_ref
        assert kwargs['context_raw'] == context_body
        def load():
            kwargs['check_active']()
            calls.append('report')
        return load
    monkeypatch.setattr(module, 'retouch_qualification_loader', qualification_loader)
    captured = {}
    @contextmanager
    def execute(ctx, artifacts, owned, context, **callbacks):
        assert owned == params and context == context_ref
        captured.update(callbacks)
        yield 'computed'
    monkeypatch.setattr(module, 'execute_artifact_retouch_preview', execute)
    return ctx, artifacts, options, calls, captured, params, context_ref, context_body


def test_installer_connects_owned_callbacks_and_rechecks_authority(monkeypatch):
    ctx, artifacts, options, calls, captured, params, ref, body = fixture(monkeypatch)
    install = module.sql_retouch_preview_factory(**options)
    assert not calls
    execute = install(ctx, artifacts)
    with execute() as result:
        assert result == 'computed'
    assert captured['max_ram_bytes'] == 1 << 33
    assert captured['verify_qualification'](deepcopy(ctx.lease), params, ref, body) is True
    assert calls[-3:] == ['admit', 'report', 'admit']
    assert captured['verify_face_origin'](deepcopy(ctx.lease), params.intent, params.source) is True
    assert captured['verify_protection_origin'](params.source, params.protection, params.intent) is True
    assert ctx.lease.fence == 7
    options['database'].admit_semantic_retouch_preview = lambda *a:1
    assert captured['verify_admission'](deepcopy(ctx.lease), params) is False


@pytest.mark.parametrize('failure', ['operation', 'backend', 'params_missing', 'context_missing', 'duplicate', 'ram_bool', 'ram_limit', 'vram_bool', 'task_id'])
def test_installer_refuses_invalid_task_before_authority(monkeypatch, failure):
    ctx, artifacts, options, calls, _, _, ref, _ = fixture(monkeypatch)
    command, profile = ctx.lease.command, ctx.lease.execution_profile
    if failure == 'operation': command.operation = 'semantic.preview_exposure'
    if failure == 'backend': profile['backend'] = 'mps'
    if failure == 'params_missing': command.inputs.remove(command.params)
    if failure == 'context_missing': command.inputs.remove(ref)
    if failure == 'duplicate': command.inputs.append(command.inputs[0])
    if failure == 'ram_bool': profile['resource_estimate']['ram_bytes'] = True
    if failure == 'ram_limit': profile['resource_estimate']['ram_bytes'] = options['max_ram_bytes']+1
    if failure == 'vram_bool': profile['resource_estimate']['vram_bytes'] = False
    if failure == 'task_id': command.task_id = uuid4()
    with pytest.raises(ValueError):module.sql_retouch_preview_factory(**options)(ctx, artifacts)
    assert 'admit' not in calls


def test_installed_executor_refuses_lease_replacement(monkeypatch):
    ctx, artifacts, options, _, _, _, _, _ = fixture(monkeypatch)
    execute = module.sql_retouch_preview_factory(**options)(ctx, artifacts)
    ctx.lease.fence += 1
    with pytest.raises(AdoptionInvalid, match='LEASE_CHANGED'):execute()


@pytest.mark.parametrize('key,value', [('database',None), ('objects',None), ('bucket',''),
    ('max_ram_bytes',True), ('runtime_lock_sha','bad')])
def test_factory_refuses_incomplete_deployment(monkeypatch, key, value):
    _, _, options, _, _, _, _, _ = fixture(monkeypatch)
    options[key] = value
    with pytest.raises(ValueError, match='INSTALLATION_REQUIRED'):module.sql_retouch_preview_factory(**options)


def test_installer_uses_full_index_when_command_has_only_transport_roots(monkeypatch):
    ctx, artifacts, options, _, _, _, _, _ = fixture(monkeypatch)
    full = tuple(ref.model_copy(deep=True) for ref in ctx.lease.command.inputs)
    transport = [ctx.lease.command.params.model_copy(deep=True)]
    ctx.lease.command.inputs = transport
    artifacts.declared_input_refs = lambda maximum:full
    install = sql_factory(options)
    assert callable(install(ctx, artifacts))
    assert ctx.lease.command.inputs == transport


def sql_factory(options):
    return module.sql_retouch_preview_factory(**options)


@pytest.mark.parametrize('location', ['profile', 'qualification'])
@pytest.mark.parametrize('key', ['model_lock_sha', 'runtime_lock_sha', 'device_profile_sha', 'dataset_sha'])
def test_installer_refuses_each_frozen_pin_mismatch_before_admission(monkeypatch, location, key):
    ctx, artifacts, options, calls, _, _, _, _ = fixture(monkeypatch)
    profile = ctx.lease.execution_profile
    target = profile if location == 'profile' else profile['qualification']
    target[key] = '0' * 64
    with pytest.raises(AdoptionInvalid, match='QUALIFICATION_CHANGED'):
        module.sql_retouch_preview_factory(**options)(ctx, artifacts)
    assert calls == []


@pytest.mark.parametrize('loss', ['cancel', 'lease', 'admission'])
def test_installer_rechecks_authority_after_context_read_before_runtime_report(monkeypatch, loss):
    ctx, artifacts, options, calls, _, _, context_ref, _ = fixture(monkeypatch)
    original_read = artifacts.contract_bytes
    cancelled = False
    def active():
        if cancelled: raise InterruptedError('cancelled')
    ctx.check_active = active
    def read(ref, model):
        nonlocal cancelled
        raw = original_read(ref, model)
        if ref == context_ref:
            if loss == 'cancel': cancelled = True
            if loss == 'lease': ctx.lease.fence += 1
            if loss == 'admission':
                options['database'].admit_semantic_retouch_preview = lambda *args: False
        return raw
    artifacts.contract_bytes = read
    with pytest.raises((AdoptionInvalid, InterruptedError)):
        module.sql_retouch_preview_factory(**options)(ctx, artifacts)
    assert 'context' in calls and 'report' not in calls
