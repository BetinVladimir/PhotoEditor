from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait.local_manual_inputs import ManualPairOrigin,read_verified_manual_pair


class NoIO:
    def __getattr__(self,name):raise AssertionError('unexpected IO: '+name)


def test_manual_origin_must_be_declared_before_io():
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source,root,params,target,guard,vectors=[ref() for _ in range(6)]
    origin=ManualPairOrigin(root,params,vectors)
    required=[source,root,params,target,guard,vectors]
    for missing in required:
        ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(inputs=[r for r in required if r!=missing])))
        with pytest.raises(ValueError,match='PORTRAIT_MANUAL_INPUT_UNDECLARED'):
            read_verified_manual_pair(ctx,NoIO(),source,None,SimpleNamespace(artifact=target),SimpleNamespace(artifact=guard),origin)
