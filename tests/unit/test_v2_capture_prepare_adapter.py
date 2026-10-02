from types import SimpleNamespace as NS
from uuid import UUID

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.capture import prepare_adapter
from photo_worker.runtime.executor import OperationFailure


def test_prepare_adapter_returns_root_and_complete_child_inventory(monkeypatch):
    refs = [ArtifactRef(artifact_id=UUID(int=i), sha256='a' * 64) for i in range(1, 6)]
    from photo_worker.operations.session.contracts import VersionBinding
    binding = VersionBinding(asset_version_id=UUID(int=9), source=refs[0], canvas_hash='b' * 64)
    params = NS(model_lock_sha='c' * 64, calibration=refs[0], binding=binding)
    ctx = NS(lease=NS(execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu',
             'resource_estimate': {'ram_bytes': 2 * 1024**3}}), check_active=lambda: None)
    prepared = NS(proxy=NS(image_ref=refs[1]), faces_ref=refs[2], outputs=tuple(refs[1:]), bytes_written=123)
    monkeypatch.setattr(prepare_adapter, 'prepare_capture_inputs', lambda *args: prepared)
    class Artifacts:
        document = None
        def params(self, kind): return params
        def contract(self, ref, kind): return NS()
        def stage_contract(self, document, role):
            assert role == 'document'
            self.document = document
            return refs[0]
    artifacts = Artifacts()
    result = prepare_adapter.execute(ctx, artifacts, NS(lock=NS(lock_id='c' * 64)), 'cpu')
    assert result.outputs == [refs[0], *refs[1:]]
    assert artifacts.document.binding == binding
    assert result.bytes_written == 123 + len(canonical_bytes(artifacts.document))


def test_prepare_adapter_requires_admitted_memory_before_inputs():
    ctx = NS(lease=NS(execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu',
             'resource_estimate': {'ram_bytes': 0}}))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        prepare_adapter.execute(ctx, None, None, 'cpu')
