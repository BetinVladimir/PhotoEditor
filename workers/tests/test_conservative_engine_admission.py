from types import SimpleNamespace
import pytest
from photo_worker.contracts.conservative import DefectAutoRepairParams
from photo_worker.operations.conservative import prepare_conservative_repair
from test_conservative_selection import ref


def params():
    return DefectAutoRepairParams(policy='conservative_auto_v1', image=ref(1),
        proposal=ref(2), proposal_report=ref(3), resolved=ref(4))


@pytest.mark.parametrize('backend,ram', [('mps', 1 << 30), ('cuda', 1 << 30), ('cpu', 0), ('cpu', True)])
def test_direct_auto_task_requires_cpu_and_real_resource_admission_before_io(backend, ram):
    class Artifacts:
        def contract(self, *args): raise AssertionError('must reject before input IO')
    with pytest.raises(ValueError, match='RESOURCE'):
        with prepare_conservative_repair(Artifacts(), params(), execution_profile={
                'backend': backend, 'resource_estimate': {'ram_bytes': ram}}):
            raise AssertionError('must not yield output')


def test_auto_replay_requires_pinned_runtime_even_with_valid_profile():
    class Artifacts:
        def contract(self, *args): raise AssertionError('must reject before input IO')
    with pytest.raises(ValueError, match='RUNTIME_REQUIRED'):
        with prepare_conservative_repair(Artifacts(), params(), current=SimpleNamespace(),
                execution_profile={'backend': 'mps', 'resource_estimate': {'ram_bytes': 1 << 30}}):
            raise AssertionError('must not yield output')


def test_revoked_runtime_stops_replay_before_reading_source():
    class Artifacts:
        def contract(self, *args): raise AssertionError('revoked runtime may not read source')
    def revoked(): raise RuntimeError('qualification revoked')
    with pytest.raises(RuntimeError, match='qualification revoked'):
        with prepare_conservative_repair(Artifacts(), params(), current=SimpleNamespace(), verify_runtime=revoked,
                execution_profile={'backend': 'mps', 'resource_estimate': {'ram_bytes': 1 << 30}}):
            raise AssertionError('must not yield output')
