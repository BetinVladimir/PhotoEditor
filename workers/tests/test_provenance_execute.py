"""Receipt/profile boundaries; no SDK or cryptographic qualification claimed."""
from hashlib import sha256
from types import SimpleNamespace
from contextlib import nullcontext
from uuid import uuid4

import pytest

from photo_worker.operations.provenance.execute import (
    execute, require_signer_profile, signer_operations, verified_receipt,
)
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.provenance import ProvenanceSignParams, SigningPolicy
from photo_worker.runtime.executor import OperationFailure, TaskCancelled
from photo_worker.runtime.gateway import ArtifactMetadata
from photo_worker.runtime.scratch import ScratchSpace


def test_receipt_binds_both_uploaded_files(tmp_path):
    path = tmp_path / 'signed.jpg'
    path.write_bytes(b'signed-output')
    receipt = dict(manifest=b'manifest', manifest_sha256=sha256(b'manifest').hexdigest(),
                   signed_bytes=13, signed_sha256=sha256(b'signed-output').hexdigest(),
                   validation_sha256='a' * 64)
    manifest, size, identity = verified_receipt(receipt, path)
    assert (manifest, size) == (b'manifest', 13)
    assert identity == (path.stat().st_dev, path.stat().st_ino)
    for change in ({'manifest': b''}, {'manifest_sha256': '0' * 64},
                   {'signed_bytes': True}, {'signed_bytes': 12}, {'signed_sha256': '0' * 64},
                   {'validation_sha256': None}, {'validation_sha256': 'A' * 64}):
        with pytest.raises(ValueError):
            verified_receipt({**receipt, **change}, path)
    path.write_bytes(b'changed-output')
    with pytest.raises(ValueError):
        verified_receipt(receipt, path)


def test_receipt_boolean_cancellation_prevents_file_open(tmp_path, monkeypatch):
    import importlib
    module = importlib.import_module('photo_worker.operations.provenance.execute')
    monkeypatch.setattr(module.os, 'open', lambda *args, **kwargs: pytest.fail('file opened after cancellation'))
    receipt = dict(manifest=b'manifest', manifest_sha256=sha256(b'manifest').hexdigest(),
                   signed_bytes=13, signed_sha256='a' * 64, validation_sha256='b' * 64)
    with pytest.raises(TaskCancelled):
        verified_receipt(receipt, tmp_path / 'signed.jpg', check_active=lambda: False)


def test_receipt_refuses_same_inode_change_after_hash(tmp_path):
    path = tmp_path / 'signed.jpg'
    path.write_bytes(b'signed-output')
    receipt = dict(manifest=b'manifest', manifest_sha256=sha256(b'manifest').hexdigest(),
                   signed_bytes=13, signed_sha256=sha256(b'signed-output').hexdigest(),
                   validation_sha256='a' * 64)
    calls = 0

    def change_after_read():
        nonlocal calls
        calls += 1
        if calls == 5:
            with path.open('ab') as stream:
                stream.write(b'-changed')

    with pytest.raises(ValueError, match='SIGNER_OUTPUT_CHANGED'):
        verified_receipt(receipt, path, check_active=change_after_read)


def test_receipt_boolean_cancellation_after_open_closes_descriptor(tmp_path, monkeypatch):
    import importlib
    import os
    module = importlib.import_module('photo_worker.operations.provenance.execute')
    path = tmp_path / 'signed.jpg'
    path.write_bytes(b'signed-output')
    receipt = dict(manifest=b'manifest', manifest_sha256=sha256(b'manifest').hexdigest(),
                   signed_bytes=13, signed_sha256=sha256(b'signed-output').hexdigest(),
                   validation_sha256='b' * 64)
    original_open = os.open
    opened = []
    live = True

    def cancel_after_open(*args, **kwargs):
        nonlocal live
        descriptor = original_open(*args, **kwargs)
        opened.append(descriptor)
        live = False
        return descriptor

    monkeypatch.setattr(module.os, 'open', cancel_after_open)
    with pytest.raises(TaskCancelled):
        verified_receipt(receipt, path, check_active=lambda: live)
    assert len(opened) == 1
    with pytest.raises(OSError):
        os.fstat(opened[0])


def test_gateway_rejects_swapped_signed_file_before_network(tmp_path):
    from photo_worker.runtime.gateway import Gateway, GatewayError
    path = tmp_path/'signed.jpg'
    path.write_bytes(b'signed-output')
    identity = (path.stat().st_dev, path.stat().st_ino)
    path.rename(tmp_path/'old-signed.jpg')
    path.write_bytes(b'signed-output')
    gateway = Gateway.__new__(Gateway)
    gateway._roles = frozenset({'signed_export'})
    gateway._max_output = 512 << 20
    gateway._check = lambda: None
    gateway._connection = lambda: pytest.fail('network reached after inode swap')
    with pytest.raises(GatewayError, match='ARTIFACT_CHANGED'):
        gateway.upload(path, 'signed_export', 'image/jpeg', expected_identity=identity)


def test_signer_requires_exact_pool_device_and_installed_pins():
    policy = SimpleNamespace(sdk_lock_sha='a' * 64, certificate_chain_sha='b' * 64,
                             trust_bundle_sha='c' * 64)
    profile = dict(backend='cpu', renderer_version='v2.2', execution_pool='signer',
                   sdk_lock_sha=policy.sdk_lock_sha, certificate_chain_sha=policy.certificate_chain_sha,
                   trust_bundle_sha=policy.trust_bundle_sha, max_input_json_bytes=16<<20, resource_estimate=dict(vram_bytes=0,ram_bytes=32<<20,
                       scratch_bytes=1<<30,output_bytes=528<<20,pixel_count=0))
    require_signer_profile(profile, policy)
    for change in ({'execution_pool': 'cpu'}, {'backend': 'mps'}, {'renderer_version': 'v1'},
                   {'sdk_lock_sha': 'd' * 64}, {'certificate_chain_sha': 'd' * 64},
                   {'trust_bundle_sha': 'd' * 64}, {'resource_estimate': {'vram_bytes': False}},
                   {'resource_estimate': {'vram_bytes': 1}}):
        with pytest.raises(ValueError):
            require_signer_profile({**profile, **change}, policy)


def test_no_operator_means_no_registered_operation():
    assert signer_operations(None, None) == []


@pytest.mark.parametrize('mode', ['success', 'wrong_type', 'wrong_mime', 'changed_version',
                                 'wrong_policy', 'corrupt_receipt', 'missing_validation', 'cancelled', 'changed_signed_upload',
                                 'changed_manifest_upload', 'changed_source_after_upload',
                                 'cancel_signed_upload', 'cancel_manifest_upload', 'cancel_root_upload',
                                 'mutate_metadata_ref', 'mutate_contract_ref', 'cancel_initial_metadata',
                                 'cancel_verify_metadata', 'cancel_final_metadata', 'boolean_inactive',
                                 'boolean_cancel_signed_upload'])
def test_adapter_publishes_only_bound_complete_result(tmp_path, monkeypatch, mode):
    # The bridge is intentionally doubled: this proves adapter coordination,
    # scratch ownership and refusal boundaries, not cryptographic validity.
    import importlib
    module = importlib.import_module('photo_worker.operations.provenance.execute')
    source = tmp_path / 'source.png'
    source.write_bytes(b'\x89PNG\r\n\x1a\nfixture')
    def ref(data):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(data).hexdigest())
    policy_id = uuid4()
    policy_raw = canonical_bytes(dict(schema_version=1, id=str(policy_id), revision='1',
        sdk_lock_sha='a'*64, key_handle='test-only', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off'))
    lineage_raw = canonical_bytes(dict(schema_version=1, source_hashes=['d'*64], actions=[]))
    params = ProvenanceSignParams(schema_version=1, export=ref(source.read_bytes()),
        policy=ref(policy_raw), lineage=ref(lineage_raw),
        signing_policy_id=uuid4() if mode == 'wrong_policy' else policy_id)
    policy = SigningPolicy.model_validate_json(policy_raw)
    profile = dict(backend='cpu', renderer_version='v2.2', execution_pool='signer',
        sdk_lock_sha=policy.sdk_lock_sha, certificate_chain_sha=policy.certificate_chain_sha,
        trust_bundle_sha=policy.trust_bundle_sha, max_input_json_bytes=16<<20, resource_estimate=dict(vram_bytes=0,ram_bytes=32<<20,
                       scratch_bytes=1<<30,output_bytes=528<<20,pixel_count=0))
    cancelled = False
    def active():
        if cancelled:
            if mode in ('boolean_inactive', 'boolean_cancel_signed_upload'):
                return False
            raise TaskCancelled('cancelled')
        if mode == 'boolean_inactive':
            return False
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),
                          check_active=active, progress=lambda *args: None)
    uploaded = []
    metadata_calls = 0
    def metadata(reference):
        nonlocal metadata_calls, cancelled
        metadata_calls += 1
        if mode == 'mutate_metadata_ref':
            reference.sha256 = 'f' * 64
        if (mode == 'cancel_initial_metadata' and metadata_calls == 1 or
                mode == 'cancel_verify_metadata' and metadata_calls == 2 or
                mode == 'cancel_final_metadata' and metadata_calls == 3):
            cancelled = True
        return ArtifactMetadata(len(source.read_bytes()),
            'image/jpeg' if mode == 'wrong_mime' else 'image/png',
            ('f' if (mode == 'changed_version' and metadata_calls > 1 or
                     mode == 'changed_source_after_upload' and metadata_calls > 2) else 'e') * 64)
    def stage(path, reservation, role, media, *, expected_identity=None):
        nonlocal cancelled
        active()
        reservation.validate_file(path)
        if role == 'signed_export':
            assert expected_identity == (path.stat().st_dev, path.stat().st_ino)
        else:
            assert expected_identity is None
        data = path.read_bytes()
        if (mode == 'changed_signed_upload' and role == 'signed_export' or
                mode == 'changed_manifest_upload' and role == 'manifest'):
            data += b'changed-between-verification-and-upload'
        value = ref(data)
        uploaded.append((role, media, value, data))
        if (mode in ('cancel_signed_upload', 'boolean_cancel_signed_upload') and role == 'signed_export'
                or mode == 'cancel_manifest_upload' and role == 'manifest'):
            cancelled = True
        return value
    def stage_contract(value, role):
        nonlocal cancelled
        data = canonical_bytes(value)
        root = ref(data)
        uploaded.append((role, 'application/json', root, data))
        if mode == 'cancel_root_upload':
            cancelled = True
        return root
    def bridge(input_path, output_path, mime, source_sha, lineage, raw_policy, operator, *, check_active):
        nonlocal cancelled
        check_active()
        assert input_path == source and source_sha == params.export.sha256
        assert lineage == lineage_raw and raw_policy == policy_raw and mime == 'image/png'
        output_path.write_bytes(b'signed-fixture')
        cancelled = mode == 'cancelled'
        return dict(manifest=b'manifest', manifest_sha256=sha256(b'manifest').hexdigest(),
                    signed_sha256='0'*64 if mode == 'corrupt_receipt' else sha256(b'signed-fixture').hexdigest(),
                    signed_bytes=len(b'signed-fixture'),
                    validation_sha256=None if mode == 'missing_validation' else 'a'*64)
    def contract_bytes(reference, model):
        raw = policy_raw if reference == params.policy else lineage_raw
        if mode == 'mutate_contract_ref':
            reference.sha256 = 'f' * 64
        return raw
    monkeypatch.setattr(module, 'sign_staged', bridge)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 600 << 20, min_free_bytes=0) as scratch:
        artifacts = SimpleNamespace(scratch=scratch, params=lambda _: params,
            declared_contract_refs=lambda name, maximum: () if mode == 'wrong_type' else
                ({'QualifiedExport': params.export, 'SigningLineage': params.lineage,
                  'SigningPolicy': params.policy}[name],),
            contract_bytes=contract_bytes,
            input_metadata=metadata, input=lambda _: nullcontext(SimpleNamespace(path=source)),
            stage=stage, stage_contract=stage_contract)
        if mode not in ('success', 'mutate_metadata_ref', 'mutate_contract_ref'):
            with pytest.raises((OperationFailure, ValueError, TaskCancelled)):
                execute(ctx, artifacts, object())
            if mode in ('cancel_signed_upload', 'boolean_cancel_signed_upload', 'cancel_manifest_upload', 'cancel_root_upload', 'cancel_final_metadata'):
                expected = {'cancel_signed_upload': ['signed_export'],
                            'boolean_cancel_signed_upload': ['signed_export'],
                            'cancel_manifest_upload': ['signed_export', 'manifest'],
                            'cancel_root_upload': ['signed_export', 'manifest', 'document'],
                            'cancel_final_metadata': ['signed_export', 'manifest']}[mode]
                assert [item[0] for item in uploaded] == expected
            elif mode in ('changed_signed_upload','changed_manifest_upload','changed_source_after_upload'):
                assert uploaded and all(item[0] != 'document' for item in uploaded)
            else:
                assert uploaded == []
        else:
            result = execute(ctx, artifacts, object())
            assert [item[0] for item in uploaded] == ['signed_export', 'manifest', 'document']
            assert result.outputs == [uploaded[-1][2]]
            assert result.bytes_written == sum(len(item[3]) for item in uploaded)
            assert params.export.sha256 == sha256(source.read_bytes()).hexdigest()
            assert params.policy.sha256 == sha256(policy_raw).hexdigest()
            assert params.lineage.sha256 == sha256(lineage_raw).hexdigest()


@pytest.mark.parametrize('name,value', [
    ('ram_bytes',False),('ram_bytes',(16<<20)-1),
    ('scratch_bytes',(1<<30)-1),('output_bytes',(528<<20)-1),
    ('pixel_count',1),('pixel_count',False),('ram_bytes',2**53),
])
def test_signer_repeats_backend_resource_envelope(name,value):
    policy=SimpleNamespace(sdk_lock_sha='a'*64,certificate_chain_sha='b'*64,trust_bundle_sha='c'*64)
    estimate=dict(vram_bytes=0,ram_bytes=32<<20,scratch_bytes=1<<30,output_bytes=528<<20,pixel_count=0)
    estimate[name]=value
    profile=dict(backend='cpu',renderer_version='v2.2',execution_pool='signer',
        sdk_lock_sha=policy.sdk_lock_sha,certificate_chain_sha=policy.certificate_chain_sha,
        trust_bundle_sha=policy.trust_bundle_sha,max_input_json_bytes=16<<20,resource_estimate=estimate)
    with pytest.raises(ValueError,match='SIGNER_RESOURCE_BINDING'):require_signer_profile(profile,policy)


@pytest.mark.parametrize('maximum',[None,False,0,(1<<20)-1,(128<<20)+1])
def test_signer_refuses_invalid_input_json_budget(maximum):
    policy=SimpleNamespace(sdk_lock_sha='a'*64,certificate_chain_sha='b'*64,trust_bundle_sha='c'*64)
    profile=dict(backend='cpu',renderer_version='v2.2',execution_pool='signer',
        sdk_lock_sha=policy.sdk_lock_sha,certificate_chain_sha=policy.certificate_chain_sha,
        trust_bundle_sha=policy.trust_bundle_sha,max_input_json_bytes=maximum,
        resource_estimate=dict(vram_bytes=0,ram_bytes=128<<20,scratch_bytes=1<<30,output_bytes=528<<20,pixel_count=0))
    with pytest.raises(ValueError,match='SIGNER_RESOURCE_BINDING'):require_signer_profile(profile,policy)
