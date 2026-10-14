from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.portrait_donor_manual_authority import donor_manual_authority
from test_portrait_donor_manual import manual_case


def test_donor_manual_origin_repeated_and_owned():
    args,_=manual_case();params,source,target,guard,root,root_raw,original,producer=args[:8]
    body=canonical_bytes(params)
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256=sha256(body).hexdigest())))
    expected=(root.artifact_id,root.sha256,original.artifact_id,original.sha256)
    calls=[];ready=[True]
    def live(owned,raw,target_pixel,guard_pixel,vectors):
        assert raw==body and target_pixel.artifact_id!=guard_pixel.artifact_id and vectors is None
        owned.command.operation='changed';calls.append(True)
        return expected if ready[0] else None
    authority=donor_manual_authority(SimpleNamespace(donor_manual_origin=live),lease,body,source,target,guard,root,original,producer,lambda:None)
    lease.command.operation='changed'
    assert authority(params,root,original) is True and authority(params,root,original) is True
    changed=root.model_copy(deep=True);changed.artifact_id=uuid4()
    with pytest.raises(ValueError):authority(params,changed,original)
    assert len(calls)==2
    ready[0]=False
    with pytest.raises(ValueError):authority(params,root,original)
    assert len(calls)==3
