import json
from pathlib import Path

import pytest
from jsonschema import Draft202012Validator
from photo_worker.runtime.capacity import Capacity
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.service import (
    WorkerConfig,
    WorkerConfigurationError,
    _capacity_sample,
)
from test_worker_service import environment

POLICY = Path(__file__).resolve().parents[2] / 'configs/capacity-rtx3060.json'


def test_rtx_policy_is_valid_and_caps_advertised_resources(tmp_path):
    schema_path = POLICY.parents[1] / 'docs/implementation-v2/contracts/schemas.json'
    schema = json.loads(schema_path.read_text())
    Draft202012Validator(schema['$defs']['CapacityPolicy']).validate(json.loads(POLICY.read_text()))
    policy = load_capacity_policy(POLICY)
    assert len(policy.sha256) == 64
    sample = Capacity(40_000_000_000, 20_000_000_000, 12_000_000_000)
    assert policy.cap_measurement(sample, resource_class='gpu') == Capacity(
        policy.photo_ram_budget_bytes, sample.scratch_bytes, policy.gpu_budget_bytes)
    assert policy.cap_measurement(sample, resource_class='cpu').ram_bytes == policy.control_ram_budget_bytes
    assert policy.cap_measurement(sample, resource_class='gpu', scratch_total_bytes=100_000_000_000).scratch_bytes == 5_000_000_000
    config = WorkerConfig.from_env({**environment(tmp_path), 'PHOTO_CAPACITY_POLICY': str(POLICY)})
    assert config.capacity_policy is not None
    assert _capacity_sample(config, sample).ram_bytes == policy.photo_ram_budget_bytes


def test_policy_rejects_unknown_fields_and_unsafe_intervals(tmp_path):
    payload = json.loads(POLICY.read_text())
    payload['extra'] = True
    path = tmp_path / 'policy.json'
    path.write_text(json.dumps(payload))
    with pytest.raises(ValueError):
        load_capacity_policy(path)
    payload.pop('extra')
    payload['lease_seconds'] = payload['renew_seconds']
    path.write_text(json.dumps(payload))
    with pytest.raises(ValueError, match='INTERVAL'):
        load_capacity_policy(path)
    with pytest.raises(WorkerConfigurationError, match='PHOTO_CAPACITY_POLICY'):
        WorkerConfig.from_env({**environment(tmp_path), 'PHOTO_CAPACITY_POLICY': str(path)})


def test_metal_verification_policy_uses_measured_capacity_without_ten_gb_ceiling():
    policy = load_capacity_policy(POLICY.with_name('capacity-metal-verification.json'))
    assert policy.gpu_budget_bytes == 2**53 - 1
    for measured, expected in ((20_000_000_000, 20_000_000_000), (4_000_000_000, 4_000_000_000)):
        sample = Capacity(32_000_000_000, 100_000_000_000, measured)
        assert policy.cap_measurement(sample, resource_class='gpu').vram_bytes == expected


@pytest.mark.parametrize('measured,resource_class,total', [
    (Capacity(-1, 100, 0), 'cpu', None),
    (Capacity(True, 100, 0), 'cpu', None),
    (Capacity(100, 1.5, 0), 'cpu', None),
    (Capacity(100, 100, 2**53), 'gpu', None),
    (Capacity(100, 100, 0), 'unknown', None),
    (Capacity(100, 100, 0), 'cpu', True),
    (Capacity(100, 100, 0), 'cpu', 0),
    (Capacity(100, 100, 0), 'cpu', 99),
    (Capacity(100, 100, 0), 'cpu', 2**53),
    (None, 'cpu', None),
])
def test_policy_refuses_invalid_measurements_before_advertising(measured, resource_class, total):
    policy = load_capacity_policy(POLICY)
    with pytest.raises(ValueError, match='CAPACITY_MEASUREMENT_INVALID'):
        policy.cap_measurement(measured, resource_class=resource_class, scratch_total_bytes=total)


def test_policy_reports_exhausted_resources_without_inventing_capacity():
    policy = load_capacity_policy(POLICY.with_name('capacity-metal-verification.json'))
    assert policy.cap_measurement(Capacity(0, 0, 0), resource_class='gpu', scratch_total_bytes=1) == Capacity(0, 0, 0)
    assert policy.cap_measurement(Capacity(100, 10, 100), resource_class='gpu', scratch_total_bytes=100).scratch_bytes == 0


@pytest.mark.parametrize('heartbeat', [0, 11, 100, True, 1.5])
def test_policy_rejects_heartbeat_not_supported_by_reporter(tmp_path, heartbeat):
    payload=json.loads(POLICY.read_text())
    payload['heartbeat_seconds']=heartbeat
    payload['offline_seconds']=1000
    path=tmp_path/'unsupported-heartbeat.json'
    path.write_text(json.dumps(payload))
    with pytest.raises(ValueError):
        load_capacity_policy(path)


@pytest.mark.parametrize('heartbeat', [1, 10])
def test_policy_heartbeat_boundaries_match_reporter(tmp_path, heartbeat):
    from photo_worker.runtime.capacity import CapacityReporter
    payload=json.loads(POLICY.read_text())
    payload['heartbeat_seconds']=heartbeat
    path=tmp_path/'supported-heartbeat.json'
    path.write_text(json.dumps(payload))
    policy=load_capacity_policy(path)
    reporter=CapacityReporter(lambda sample:True,lambda:Capacity(0,0,0),lambda:None,
        interval_seconds=policy.heartbeat_seconds)
    assert reporter.interval==heartbeat


def test_policy_file_replacement_during_read_is_refused(tmp_path,monkeypatch):
    import os
    import photo_worker.runtime.capacity_policy as module
    path=tmp_path/'policy.json';path.write_bytes(POLICY.read_bytes())
    replacement=tmp_path/'replacement.json';replacement.write_bytes(POLICY.read_bytes())
    original=os.read;swapped=False
    def read(descriptor,size):
        nonlocal swapped
        chunk=original(descriptor,size)
        if not swapped:
            swapped=True;os.replace(replacement,path)
        return chunk
    monkeypatch.setattr(module.os,'read',read)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_CHANGED'):load_capacity_policy(path)


def test_policy_loader_refuses_fifo_without_blocking(tmp_path):
    import os
    path=tmp_path/'policy.fifo';os.mkfifo(path)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_PATH_INVALID'):load_capacity_policy(path)


def test_policy_loader_refuses_symlink_and_oversized_file(tmp_path):
    path=tmp_path/'policy.json';path.write_bytes(POLICY.read_bytes())
    link=tmp_path/'link.json';link.symlink_to(path)
    with pytest.raises((OSError,ValueError)):load_capacity_policy(link)
    path.write_bytes(b' '*65_537)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_PATH_INVALID'):load_capacity_policy(path)


def test_policy_loader_refuses_foreign_owner_and_writable_file(tmp_path):
    from photo_worker.runtime.capacity_policy import trusted_capacity_owner
    assert trusted_capacity_owner(0, 1001)
    assert trusted_capacity_owner(1001, 1001)
    assert not trusted_capacity_owner(1002, 1001)
    path=tmp_path/'policy.json'
    path.write_bytes(POLICY.read_bytes())
    path.chmod(0o666)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_PATH_INVALID'):
        load_capacity_policy(path)


def test_policy_loader_refuses_unsafe_parent_directory(tmp_path):
    parent=tmp_path/'installed';parent.mkdir()
    path=parent/'policy.json';path.write_bytes(POLICY.read_bytes())
    alias=tmp_path/'alias';alias.symlink_to(parent,target_is_directory=True)
    with pytest.raises((OSError,ValueError)):
        load_capacity_policy(alias/'policy.json')
    parent.chmod(0o777)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_PATH_INVALID'):
        load_capacity_policy(path)


def test_policy_loader_refuses_parent_replacement_during_read(tmp_path,monkeypatch):
    import os
    import photo_worker.runtime.capacity_policy as module
    parent=tmp_path/'installed';parent.mkdir()
    path=parent/'policy.json';path.write_bytes(POLICY.read_bytes())
    original=os.read;swapped=False
    def read(descriptor,size):
        nonlocal swapped
        chunk=original(descriptor,size)
        if chunk and not swapped:
            swapped=True
            parent.rename(tmp_path/'retired')
            parent.mkdir()
            path.write_bytes(POLICY.read_bytes())
        return chunk
    monkeypatch.setattr(module.os,'read',read)
    with pytest.raises(ValueError,match='CAPACITY_POLICY_CHANGED'):
        load_capacity_policy(path)
    assert swapped


@pytest.mark.parametrize('renew', [0,11,True,1.5])
def test_policy_rejects_renewal_not_supported_by_executor(tmp_path,renew):
    payload=json.loads(POLICY.read_text());payload['renew_seconds']=renew
    path=tmp_path/'renew.json';path.write_text(json.dumps(payload))
    with pytest.raises(ValueError):load_capacity_policy(path)


def test_built_lifecycle_installs_policy_renewal_before_io(monkeypatch):
    from types import SimpleNamespace
    from photo_worker.runtime.capacity_policy import CapacityPolicy
    import photo_worker.runtime.service as service
    payload=json.loads(POLICY.read_text());payload['renew_seconds']=3
    policy=CapacityPolicy.model_validate(payload)
    executor=SimpleNamespace(heartbeat_seconds=10)
    candidate=SimpleNamespace(capacity_policy=policy,resource_class='gpu',backend='mps')
    def install(config):
        assert executor.heartbeat_seconds==3
        raise RuntimeError('stop before hardware IO')
    monkeypatch.setattr(service,'_install_metal_budget',install)
    with pytest.raises(RuntimeError,match='stop before hardware IO'):
        service._run_built_worker(candidate,object(),SimpleNamespace(executor=executor))


@pytest.mark.parametrize('heartbeat,offline,renew,lease', [
    (10, 10, 10, 60), (10, 9, 10, 60),
    (10, 30, 10, 10), (10, 30, 10, 9),
    (1, 1, 1, 2), (1, 2, 1, 1),
])
def test_policy_interval_relations_reject_valid_scalar_fields(tmp_path, heartbeat, offline, renew, lease):
    payload = json.loads(POLICY.read_text())
    payload.update(heartbeat_seconds=heartbeat, offline_seconds=offline,
                   renew_seconds=renew, lease_seconds=lease)
    path = tmp_path / 'incoherent-intervals.json'
    path.write_text(json.dumps(payload))
    with pytest.raises(ValueError, match='CAPACITY_POLICY_INTERVAL_INVALID'):
        load_capacity_policy(path)


@pytest.mark.parametrize('heartbeat,offline,renew,lease', [
    (1, 2, 1, 2), (10, 11, 10, 11), (10, 30, 10, 60), (3, 120, 2, 300),
])
def test_policy_retains_operator_interval_values_in_exact_sha(tmp_path, heartbeat, offline, renew, lease):
    from photo_worker.contracts.base import canonical_hash
    payload = json.loads(POLICY.read_text())
    payload.update(heartbeat_seconds=heartbeat, offline_seconds=offline,
                   renew_seconds=renew, lease_seconds=lease)
    path = tmp_path / 'coherent-intervals.json'
    path.write_text(json.dumps(payload))
    policy = load_capacity_policy(path)
    assert (policy.heartbeat_seconds, policy.offline_seconds,
            policy.renew_seconds, policy.lease_seconds) == (heartbeat, offline, renew, lease)
    assert policy.sha256 == canonical_hash(payload)
    for field in ('heartbeat_seconds', 'offline_seconds', 'renew_seconds', 'lease_seconds'):
        changed = dict(payload)
        changed[field] += 1
        assert canonical_hash(changed) != policy.sha256


@pytest.mark.parametrize('total,fraction,reserved', [
    (9007199254740987, 0.15, 1351079888211149),
    (9007199254740991, 0.5, 4503599627370496),
    (101, 0.15, 16),
    (100, 0.15, 15),
    (1, 0.05, 1),
])
def test_scratch_floor_uses_exact_decimal_ceiling(total, fraction, reserved):
    from photo_worker.runtime.capacity_policy import CapacityPolicy
    payload = json.loads(POLICY.read_text())
    payload['scratch_free_floor_fraction'] = fraction
    policy = CapacityPolicy.model_validate_json(json.dumps(payload))
    measured = Capacity(100, total, 0)
    assert policy.cap_measurement(
        measured, resource_class='archive', scratch_total_bytes=total,
    ).scratch_bytes == total - reserved


def test_scratch_decimal_ceiling_does_not_depend_on_decimal_context():
    from decimal import localcontext
    policy = load_capacity_policy(POLICY)
    with localcontext() as context:
        context.prec = 2
        assert policy.cap_measurement(
            Capacity(100, 9007199254740987, 0), resource_class='archive',
            scratch_total_bytes=9007199254740987,
        ).scratch_bytes == 7656119366529838
