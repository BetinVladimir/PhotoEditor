from copy import deepcopy
from dataclasses import asdict
from pathlib import Path
from types import SimpleNamespace
import json
from hashlib import sha256
import pytest
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.operations.semantic import exposure_installation as module
from test_exposure_qualification_report import exposure_fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure', [None, 'measurement', 'profile', 'params', 'lease', 'revoked', 'actual'])
def test_sql_cpu_installation_binds_original_profile_params_and_live_report(monkeypatch, failure):
    policy, _, report_bytes, pin = exposure_fixture()
    pin_value = json.loads(json.dumps(asdict(pin), default=str))
    value, _, _ = source_fixture()
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(value))
    raw = canonical_bytes(edit)
    profile = dict(backend='cpu', renderer_version='v2.2', qualification=pin_value)
    command = SimpleNamespace(operation='semantic.local_exposure', operation_version='1',
        resource_class='cpu', params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command, execution_profile=profile, fence=1),
        check_active=lambda: None)
    reads = []
    def loader(db, lease, params, objects, bucket, *, frozen_pin, check_active):
        assert params == raw and frozen_pin == pin and bucket == 'owned'
        def load():
            check_active(); reads.append(1)
            if failure == 'revoked' and len(reads) > 1:
                raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
            return pin_value
        return load
    if failure != 'actual':
        monkeypatch.setattr(module, 'exposure_worker_qualification_loader', loader)
    db = SimpleNamespace(exposure_report_evidence=lambda *a: None,
        admit_semantic_local_exposure=lambda *a: False,
        local_exposure_protection_origin=lambda *a: False)
    from io import BytesIO
    streams = []
    def report_evidence(*args):
        reads.append(1)
        return dict(pin=pin_value, policy=policy, report=dict(artifact_id=str(pin.report.artifact_id),
            sha256=pin.report.sha256, bucket='owned', object_key='report', object_version='v1',
            bytes=len(report_bytes), media_type='application/json'))
    def get_report(bucket, key, *, version_id):
        assert (bucket, key, version_id) == ('owned', 'report', 'v1')
        stream = BytesIO(report_bytes); streams.append(stream); return stream
    objects = SimpleNamespace(get_object=get_report)
    protection_ref = dict(artifact_id=str(pin.calibration.artifact_id), sha256=pin.calibration.sha256)
    if failure == 'actual':
        db.exposure_report_evidence = report_evidence
        def protection_evidence(owner, body):
            assert owner == ctx.lease and body == raw
            return protection_ref
        db.exposure_protection_pin = protection_evidence
    measurements = dict(model_lock_sha=pin.model_lock_sha, runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha, dataset_sha=pin.dataset_sha)
    if failure == 'measurement':
        measurements['runtime_lock_sha'] = '0'*64
    factory = module.sql_local_exposure_factory(database=db,
        objects=objects, bucket='owned',
        icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'),
        resolve_protection=None if failure == 'actual' else lambda edit: None, **measurements)
    artifacts = SimpleNamespace(contract_bytes=lambda *a: raw)
    if failure == 'measurement':
        with pytest.raises(AdoptionInvalid, match='QUALIFICATION_CHANGED'):
            factory(ctx, artifacts)
        assert not reads
        return
    installation = factory(ctx, artifacts)
    supplied = deepcopy(profile)
    if failure == 'profile':
        supplied['qualification']['policy_sha'] = '0'*64
    elif failure == 'params':
        edit = edit.model_copy(update={'ev': edit.ev + .1})
    elif failure == 'lease':
        ctx.lease.fence += 1
    if failure not in (None, 'actual'):
        with pytest.raises(AdoptionInvalid):
            installation.verify_runtime(edit, supplied)
        assert len(reads) == (2 if failure == 'revoked' else 1)
    else:
        assert installation.verify_runtime(edit, supplied) is True
        assert installation.verify_runtime(edit, supplied) is True
        assert len(reads) == (6 if failure == 'actual' else 3)
        if failure == 'actual':
            assert len(streams) == 3 and all(stream.closed for stream in streams)
            assert installation.resolve_protection(edit).model_dump(mode='json') == protection_ref
            with pytest.raises(AdoptionInvalid, match='SCOPE_CHANGED'):
                installation.resolve_protection(edit.model_copy(update={'ev': edit.ev+.1}))
            db.exposure_protection_pin = lambda *args: None
            with pytest.raises(AdoptionInvalid, match='ORIGIN_STALE'):
                installation.resolve_protection(edit)
