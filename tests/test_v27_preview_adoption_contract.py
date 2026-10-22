from uuid import uuid4,uuid5
import pytest
from photo_worker.contracts.semantic_preview import ShownExposureAdoptionInput


def fixture():
    preview=uuid4();target=uuid4()
    ref=lambda:{'artifact_id':uuid4(),'sha256':'a'*64}
    return dict(schema_version=1,tenant_id=uuid4(),child_run_id=uuid4(),target_id=target,
        approved_edit=ref(),preview_run_id=preview,
        preview_task_id=uuid5(preview,'semantic.preview_local_exposure:'+str(target)),
        preview_result_id=uuid4(),preview_attempt=1,preview_fence=1,presentation=ref(),
        numerical_evidence=ref(),working_image=ref(),pixels=ref(),model_lock_sha='a'*64,
        runtime_lock_sha='b'*64,device_profile_sha='c'*64)


@pytest.mark.parametrize('change',['task','alias','zero','attempt_bool','fence_overflow'])
def test_shown_adoption_rejects_unbound_attempt(change):
    value=fixture()
    if change=='task':value['preview_task_id']=uuid4()
    if change=='alias':value['pixels']=value['working_image']
    if change=='zero':value['preview_attempt']=0
    if change=='attempt_bool':value['preview_attempt']=True
    if change=='fence_overflow':value['preview_fence']=2**63
    with pytest.raises(ValueError):ShownExposureAdoptionInput.model_validate(value)


def test_shown_adoption_roundtrips_frozen_identity():
    value=ShownExposureAdoptionInput.model_validate(fixture())
    assert ShownExposureAdoptionInput.model_validate_json(value.model_dump_json())==value
