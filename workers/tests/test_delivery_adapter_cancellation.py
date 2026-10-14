from types import SimpleNamespace
from unittest.mock import Mock, patch
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.delivery import DeliveryManifest, V2DeliveryAssembleParams
from photo_worker.operations.delivery.adapter import DeliveryInstallation, execute


@pytest.mark.parametrize('phase', ['params', 'first_admission', 'input', 'compute', 'final_admission', 'stage'])
def test_assembly_lease_loss_stops_at_each_adapter_boundary(phase):
    lost = False
    failure = RuntimeError('test lease lost')
    events = []
    ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    params = V2DeliveryAssembleParams(schema_version=1, assembly=ref)
    result = DeliveryManifest(schema_version=1, assembly_sha=ref.sha256,
        snapshot_digest='b' * 64, slot_count=1, ready_count=1, omitted_count=0, shards=[ref])

    def event(name, value):
        nonlocal lost
        events.append(name)
        if name == phase:
            lost = True
        return value

    def active():
        if lost:
            raise failure

    ctx = SimpleNamespace(check_active=active, lease=SimpleNamespace(
        execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu'},
        command=SimpleNamespace(resource_class='cpu', operation_version='1', operation='v2.delivery.assemble')))
    admission_count = 0

    def admit(context, candidate):
        nonlocal admission_count
        admission_count += 1
        event('first_admission' if admission_count == 1 else 'final_admission', None)

    artifacts = Mock()
    artifacts.params.side_effect = lambda model: event('params', params)
    artifacts.contract_bytes.side_effect = lambda candidate, model: event('input', b'{}')
    artifacts.stage_contract.side_effect = lambda candidate, role: event('stage', ref)
    installation = DeliveryInstallation(admission=SimpleNamespace(admit=admit))
    with patch('photo_worker.operations.delivery.adapter.assemble_delivery',
               side_effect=lambda *args: event('compute', result)):
        with pytest.raises(RuntimeError) as raised:
            execute(ctx, artifacts, installation, operation='v2.delivery.assemble')
    assert raised.value is failure
    order = ['params', 'first_admission', 'input', 'compute', 'final_admission', 'stage']
    assert events == order[:order.index(phase) + 1]
    assert artifacts.stage_contract.call_count == int(phase == 'stage')
