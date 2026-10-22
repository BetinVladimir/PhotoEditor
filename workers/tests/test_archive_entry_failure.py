from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.archive.build import _entry_failure
from photo_worker.runtime.gateway import GatewayError
from photo_worker.runtime.materialize import ArtifactTransferError

@pytest.mark.parametrize("error,code",[(GatewayError('ARTIFACT_SIZE_MISMATCH'),'ARTIFACT_HASH_MISMATCH'),(GatewayError('ARTIFACT_MISSING'),'ARTIFACT_MISSING'),(ArtifactTransferError('ARTIFACT_HASH_MISMATCH'),'ARTIFACT_HASH_MISMATCH'),(GatewayError('PRIVATE_DETAIL'),'INPUT_INVALID')])
def test_archive_entry_failure_is_bound_to_declared_artifact(error,code):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    failure=_entry_failure(error,SimpleNamespace(artifact=ref)).error
    assert failure.code==code
    assert failure.args=={'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}
    assert failure.retryable is False

def test_archive_entry_lease_loss_is_not_rewritten():
    error=GatewayError('LEASE_LOST')
    with pytest.raises(GatewayError) as caught:_entry_failure(error,None)
    assert caught.value is error
