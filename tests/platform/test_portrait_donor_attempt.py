from types import SimpleNamespace
from hashlib import sha256
from copy import deepcopy
from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.v2.portrait_donor_qualification import verify_donor_qualification
from photo_worker.v2.portrait_donor_attempt import donor_attempt_checkpoint
from test_portrait_donor_qualification import fixture


def test_donor_checkpoint_repeats_owned_lease_and_qualification():
    profile,rraw,craw,policy=fixture()
    pin,_=verify_donor_qualification(profile,rraw,craw,policy,check_active=lambda:None)
    body=b'{"schema_version":1}'
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256=sha256(body).hexdigest())),execution_profile=deepcopy(profile))
    calls=[];ready=[True];current=[pin]
    def live(owned,raw):
        assert owned.command.operation=='portrait.open_eyes' and raw==body
        owned.command.operation='changed'
        calls.append('lease');return ready[0]
    def qualified():calls.append('qualification');return current[0]
    checkpoint=donor_attempt_checkpoint(SimpleNamespace(donor_portrait_lease_ready=live),lease,body,qualified,lambda:None)
    lease.command.operation='changed';lease.execution_profile.clear()
    checkpoint();checkpoint()
    assert calls==['lease','qualification']*2
    current[0]=replace(pin,qualification_id=uuid4())
    with pytest.raises(ValueError):checkpoint()
    current[0]=pin
    ready[0]=False
    with pytest.raises(ValueError):checkpoint()
    assert calls[-1]=='lease'


def test_donor_checkpoint_rejects_params_before_sql():
    profile,_,_,_=fixture()
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256='a'*64)),execution_profile=profile)
    def forbidden(*args):raise AssertionError('unexpected SQL')
    with pytest.raises(ValueError):donor_attempt_checkpoint(SimpleNamespace(donor_portrait_lease_ready=forbidden),lease,b'{}',lambda:None,lambda:None)
