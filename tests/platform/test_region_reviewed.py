"""Human selection is a separate mandatory authority from canonical pixels."""
from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4,UUID
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.dust import DustRepairParams,DustReview
from photo_worker.operations.region.reviewed import reviewed_entry,reviewed_base


def dust_entry(*,human=False):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image_ref=ref();proposal=ref()
    selection=DustReview(version='sensor_dust_review_v1',proposal=proposal,base=image_ref,revision=1,
        accepted_candidate_ids=['b'*64] if human else [],rejected_candidate_ids=[])
    params=DustRepairParams(image=image_ref,proposal=proposal,review=selection)
    authority={'selection':selection.model_dump(mode='json')}
    if human:authority.update(review_id=str(uuid4()),decision_hash='c'*64)
    stage=SimpleNamespace(operation='sensor.dust_repair',review_authority=authority)
    image=SimpleNamespace(canvas='canvas',transform_chain='chain',alpha=None)
    effective=SimpleNamespace(extensions=SimpleNamespace(restoration=SimpleNamespace(sensor_dust='review')))
    return effective,(stage,params.model_dump(mode='json'),image,image,{})


@pytest.mark.parametrize('human',[False,True])
def test_exact_live_selection_or_explicit_empty_no_review_continuation(human):
    effective,entry=dust_entry(human=human)
    assert reviewed_entry(effective,[entry],'sensor.dust_repair')[1].review.accepted_candidate_ids==(['b'*64] if human else [])


@pytest.mark.parametrize('change',['missing','duplicate','selection','no_human','unknown','decision_hash','geometry','policy'])
def test_successful_task_never_authorizes_substituted_human_selection(change):
    effective,entry=dust_entry(human=True);entries=[entry]
    if change=='missing':entries=[]
    elif change=='duplicate':entries*=2
    elif change=='selection':entry[0].review_authority['selection']['revision']=2
    elif change=='no_human':
        del entry[0].review_authority['review_id'];del entry[0].review_authority['decision_hash']
    elif change=='unknown':entry[0].review_authority['accepted']=True
    elif change=='decision_hash':entry[0].review_authority['decision_hash']='not-a-hash'
    elif change=='geometry':entries=[(*entry[:3],SimpleNamespace(canvas='other',transform_chain='chain',alpha=None),entry[4])]
    else:effective.extensions.restoration.sensor_dust='conservative_auto'
    with pytest.raises(ValueError):reviewed_entry(effective,entries,'sensor.dust_repair')


def test_review_cannot_be_promoted_to_automatic_policy_by_omitting_stage():
    effective,_=dust_entry()
    with pytest.raises(ValueError,match='STAGE_COVERAGE_MISMATCH'):
        reviewed_entry(effective,[],'sensor.dust_repair')


def base_entry():
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image_ref=ref()
    selection=dict(version='defect_review_v1',proposal=ref().model_dump(mode='json'),image=image_ref.model_dump(mode='json'),
        label_map=ref().model_dump(mode='json'),revision=1,accepted_candidate_ids=[],rejected_candidate_ids=[])
    proof=dict(route='base',review_id=str(uuid4()),decision_hash='b'*64,selection=selection,producer_task_id=str(uuid4()))
    stage=SimpleNamespace(operation='portrait.detail',output=image_ref,review_authority=proof)
    image=SimpleNamespace(canvas='canvas',transform_chain='chain',alpha=None)
    effective=SimpleNamespace(defects=SimpleNamespace(enabled=True,policy='review'))
    return effective,(stage,{},image,image,{})


def test_photographer_base_choice_is_explicit_identity_without_fake_repair_task():
    effective,entry=base_entry()
    assert reviewed_base(effective,[entry]) is entry[3]
    assert reviewed_entry(effective,[entry],'defects.repair') is None


@pytest.mark.parametrize('change',['missing','duplicate','source','producer','hash','extra','policy','repair_conflict'])
def test_base_choice_must_remain_exactly_bound_to_its_image_dag_point(change):
    effective,entry=base_entry();entries=[entry]
    if change=='missing':entries=[]
    elif change=='duplicate':entries*=2
    elif change=='source':entry[0].review_authority['selection']['image']['artifact_id']=str(uuid4())
    elif change=='producer':entry[0].review_authority['producer_task_id']=str(UUID(int=0))
    elif change=='hash':entry[0].review_authority['decision_hash']='invalid'
    elif change=='extra':entry[0].review_authority['automatic']=True
    elif change=='policy':effective.defects.policy='conservative_auto'
    else:entries.append((SimpleNamespace(operation='defects.repair',review_authority=None),{},None,None,{}))
    with pytest.raises(ValueError):reviewed_base(effective,entries)
