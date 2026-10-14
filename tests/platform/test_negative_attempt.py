from types import SimpleNamespace
from copy import deepcopy
from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.negative_qualification import NegativeQualificationPin
from photo_worker.v2.negative_attempt import negative_attempt_checkpoint
from test_negative_qualification import fixture


def test_negative_attempt_owns_lease_and_rechecks_current_qualification():
    p,_,_,value,_=fixture();pin=NegativeQualificationPin.from_value(value)
    from hashlib import sha256
    raw=canonical_bytes(p)
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(raw).hexdigest())),
        execution_profile={'negative_qualification':deepcopy(value),'negative_runtime_sha':pin.runtime_lock_sha})
    calls=[];ready=[True];current=[pin]
    def live(owned,body):
        assert owned.command.operation=='optical.negative' and body==raw
        owned.command.operation='mutated';calls.append('lease');return ready[0]
    def qualified():calls.append('qualification');return current[0]
    checkpoint=negative_attempt_checkpoint(SimpleNamespace(negative_lease_ready=live),lease,raw,qualified,lambda:None)
    lease.command.operation='mutated';lease.execution_profile.clear()
    checkpoint();checkpoint();assert calls==['lease','qualification']*2
    current[0]=replace(pin,qualification_id=uuid4())
    with pytest.raises(ValueError,match='NEGATIVE_QUALIFICATION_CHANGED'):checkpoint()
    ready[0]=False
    with pytest.raises(ValueError,match='NEGATIVE_ATTEMPT_STALE'):checkpoint()
    assert calls[-1]=='lease'
