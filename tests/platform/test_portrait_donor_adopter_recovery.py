from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.service import Adopter
from test_v2_adopter_service import ReadConnection,WriteConnection


@pytest.mark.parametrize('code',['DONOR_ADOPTION_INSTALLATION_REQUIRED','DONOR_ADOPTION_RUNTIME_CHANGED',
    'DONOR_ADOPTION_CONTEXT_STALE','DONOR_ADOPTION_CONTEXT_CHANGED'])
def test_donor_configuration_or_snapshot_failure_keeps_candidate_pending(code):
    tenant,run,task=uuid4(),uuid4(),uuid4()
    read,write=ReadConnection(tenant,run,task),WriteConnection()
    adopter=Adopter(read,write,object(),'bucket')
    def denied(*args):raise AdoptionInvalid(code)
    adopter.coordinator=SimpleNamespace(adopt=denied)
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':1,'retry':1}
    assert read.unlocked and not write.rejected


@pytest.mark.parametrize('code',['DONOR_REPLAY_MISMATCH','DONOR_ADOPTION_CHANGED_MASK_INVALID'])
def test_verified_donor_output_corruption_remains_terminal(code):
    tenant,run,task=uuid4(),uuid4(),uuid4()
    read,write=ReadConnection(tenant,run,task),WriteConnection('failed')
    adopter=Adopter(read,write,object(),'bucket')
    def denied(*args):raise AdoptionInvalid(code)
    adopter.coordinator=SimpleNamespace(adopt=denied,context=lambda *args:SimpleNamespace(result_id=uuid4()))
    assert adopter.run_once()=={'adopted':0,'rejected':1,'finalized':1,'retry':0}
    assert read.unlocked and write.rejected
