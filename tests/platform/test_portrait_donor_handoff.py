from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.runtime.executor import OperationFailure
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait.local_result import StagedLocalPortrait
from photo_worker.operations.portrait.donor_handoff import donor_operation_result


def test_donor_handoff_preserves_review_and_only_document():
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    inputs=[ArtifactRef(artifact_id=uuid4(),sha256='b'*64)];calls=[]
    result=donor_operation_result(staged,declared_inputs=inputs,output_budget=100,check_active=lambda:calls.append('active'))
    assert result.outputs==[outputs[-1]] and result.review_required is True and result.bytes_written==100
    assert calls==['active','active']
    outputs[-1].sha256='c'*64
    assert result.outputs[0].sha256=='a'*64


@pytest.mark.parametrize('mode',['review','bool_bytes','budget','duplicate','document','input_alias','revoked'])
def test_donor_handoff_rejects_invalid_or_revoked_result(mode):
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    inputs=[ArtifactRef(artifact_id=uuid4(),sha256='b'*64)];budget=100;calls=[]
    if mode=='review':staged=replace(staged,review_required=False)
    elif mode=='bool_bytes':staged=replace(staged,bytes_written=True)
    elif mode=='budget':budget=99
    elif mode=='duplicate':staged=replace(staged,outputs=(outputs[0],)*5+(outputs[-1],))
    elif mode=='document':staged=replace(staged,document=outputs[0])
    elif mode=='input_alias':inputs=[outputs[0]]
    def active():
        calls.append('active')
        if mode=='revoked' and len(calls)==2:raise ValueError('revoked')
    with pytest.raises(ValueError if mode=='revoked' else OperationFailure):donor_operation_result(staged,declared_inputs=inputs,output_budget=budget,check_active=active)


def test_donor_handoff_stops_before_result_when_initial_authority_is_lost():
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    calls=[]
    def active():
        calls.append('active')
        raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        donor_operation_result(staged,declared_inputs=[],output_budget=0,check_active=active)
    assert calls==['active']


@pytest.mark.parametrize('budget',[True,0,-1,9007199254740992])
def test_donor_handoff_rejects_nonrepresentable_budget(budget):
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    calls=[]
    with pytest.raises(OperationFailure) as caught:
        donor_operation_result(staged,declared_inputs=[ArtifactRef(artifact_id=uuid4(),sha256='b'*64)],
            output_budget=budget,check_active=lambda:calls.append('active'))
    assert caught.value.error.code=='DONOR_RESULT_INVALID'
    assert calls==['active']


def test_handoff_checks_alias_beyond_wire_command_input_limit():
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    inputs=[ArtifactRef(artifact_id=uuid4(),sha256='b'*64) for _ in range(128)]
    inputs.append(outputs[0])
    with pytest.raises(OperationFailure):
        donor_operation_result(staged,declared_inputs=inputs,output_budget=100,check_active=lambda:None)
    inputs[-1]=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    assert donor_operation_result(staged,declared_inputs=inputs,output_budget=100,check_active=lambda:None).review_required
