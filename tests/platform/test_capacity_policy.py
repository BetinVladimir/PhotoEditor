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
    payload['renew_seconds'] = payload['lease_seconds']
    path.write_text(json.dumps(payload))
    with pytest.raises(ValueError, match='INTERVAL'):
        load_capacity_policy(path)
    with pytest.raises(WorkerConfigurationError, match='PHOTO_CAPACITY_POLICY'):
        WorkerConfig.from_env({**environment(tmp_path), 'PHOTO_CAPACITY_POLICY': str(path)})
