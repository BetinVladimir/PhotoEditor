from uuid import uuid4
import pytest
from photo_worker.algorithms.fitting import identity_color
from photo_worker.algorithms.style_aggregate import aggregate_color_fits
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.color_fit import ColorFit,ColorFitParams,FitSamplingEvidence,FitCheckpoint
from photo_worker.contracts.style import Disabled,LocalColor
from photo_worker.contracts.pair_effects import BeautyFit,BeautyFitParams,DefectEvidence,DefectFitParams
from photo_worker.contracts.images import Canvas,TransformChain,MaskRef
from photo_worker.contracts.style_aggregate import StyleAggregateParams
from photo_worker.operations.style.aggregate import execute
from photo_worker.runtime.executor import OperationFailure
from test_asset_adapters import context_for


def fit(value:float,*,local:bool=False,warning:bool=False)->ColorFit:
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    canvas=Canvas(width=8,height=8,orientation=1,space_id='after',source_sha256='e'*64)
    person=MaskRef(artifact=ref(),canvas=canvas,transform_chain=TransformChain.create([]),selector='person',confidence=1.,model_lock_id=None,protected=False) if local else None
    params=ColorFitParams(pair_id=uuid4(),before=ref(),after=ref(),alignment=ref(),person=person,local_requested=local)
    global_color=identity_color();global_color.gains_rgb=[value,value,value]
    local_value=LocalColor(enabled=True,person_ev=.1,background_ev=-.1,person_saturation=1.1,background_saturation=.9,mask_policy_id='person_guided_v1') if local else Disabled(enabled=False)
    sampling=FitSamplingEvidence(split_digest='b'*64,holdout_blocks=[1],train_digest='c'*64,holdout_digest='d'*64,
        train_seed=42,holdout_seed=43,warp_version='linear_bilinear_global_v1',train_count=10,holdout_count=10)
    return ColorFit(params=params,global_color=global_color,local=local_value,holdout_loss=value,median_delta_e=value,p95_delta_e=value,
        initialization_index=0,optimizer_version='color_fit_adam_v1',backend='cpu',sampling=sampling,
        history=[FitCheckpoint(stage='parameters',step=1,holdout_loss=value)],warnings=['alignment_review_required'] if warning else [])


def test_equal_pair_median_and_local_reliability():
    first,second,third=fit(.8,local=True),fit(1.2,local=True),fit(1.,local=False)
    global_color,local,evidence=aggregate_color_fits([first,second,third])
    assert global_color.gains_rgb==[1.,1.,1.]
    assert local.enabled and local.person_ev==pytest.approx(.1)
    assert evidence['accepted_local_fits']==2 and evidence['median_holdout_loss']==1.
    assert evidence['pair_ids']==[str(first.params.pair_id),str(second.params.pair_id),str(third.params.pair_id)]


def test_warning_excludes_local_and_duplicate_pair_rejected():
    warned=fit(1.,local=True,warning=True);plain=fit(1.,local=False)
    _,local,evidence=aggregate_color_fits([warned,plain])
    assert not local.enabled and evidence['warnings']==['alignment_review_required']
    with pytest.raises(ValueError,match='DUPLICATE_PAIR_FIT'):aggregate_color_fits([warned,warned])


class Artifacts:
    def __init__(self):
        self.contracts={};self.staged=[]
        values=[fit(.8),fit(1.2)]
        refs=[]
        for value in values:
            ref=ArtifactRef(artifact_id=uuid4(),sha256='f'*64);refs.append(ref);self.contracts[ref.artifact_id]=value
        self.value=StyleAggregateParams(style_id='portrait-v1',color_fits=refs,session_ids=['a','b'],
            pair_manifest_sha256='1'*64,runtime_lock_id='2'*64)
    def params(self,model):return self.value
    def contract(self,ref,model):return self.contracts[ref.artifact_id]
    def stage_contract(self,value,role):
        self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256='3'*64)


def test_aggregate_adapter_publishes_strict_candidate_and_report():
    artifacts=Artifacts();ctx=context_for(artifacts.value.color_fits[0],'style.aggregate')
    ctx.lease.execution_profile['resource_estimate']={'ram_bytes':536870912}
    result=execute(ctx,artifacts)
    assert [role for role,_ in artifacts.staged]==['style_candidate','aggregate_report']
    style=artifacts.staged[0][1];report=artifacts.staged[1][1]
    assert style.schema_version=='2.0.0' and style.quality.status=='needs_review' and not style.local.enabled
    assert report.pair_count==2 and report.session_count==2 and result.review_required
    assert ctx.snapshot_progress()==dict(done=4,total=4)


def test_aggregate_adapter_rejects_wrong_backend_before_artifacts():
    artifacts=Artifacts();ctx=context_for(artifacts.value.color_fits[0],'style.aggregate');ctx.lease.execution_profile['backend']='mps'
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='CONFIG_INVALID' and not artifacts.staged


@pytest.mark.parametrize('kind',['beauty','defects'])
def test_aggregate_rejects_effect_evidence_from_another_pair(kind):
    artifacts=Artifacts();color=artifacts.contracts[artifacts.value.color_fits[0].artifact_id];ref=ArtifactRef(artifact_id=uuid4(),sha256='9'*64)
    if kind=='beauty':
        params=BeautyFitParams.model_construct(pair_id=uuid4(),before=color.params.before,after=color.params.after)
        effect=BeautyFit.model_construct(params=params,beauty=Disabled(enabled=False),confidence=0.,faces=[],search_digest='8'*64,warnings=[])
        artifacts.value=artifacts.value.model_copy(update={'beauty_fits':[ref]})
    else:
        params=DefectFitParams.model_construct(pair_id=color.params.pair_id,before=ArtifactRef(artifact_id=uuid4(),sha256='7'*64),after=color.params.after)
        effect=DefectEvidence.model_construct(params=params,defects=Disabled(enabled=False),confidence=0.,residual_components=0,improved_components=0,median_residual_reduction=0.,evidence_digest='8'*64,warnings=[])
        artifacts.value=artifacts.value.model_copy(update={'defect_evidence':[ref]})
    artifacts.contracts[ref.artifact_id]=effect;ctx=context_for(artifacts.value.color_fits[0],'style.aggregate');ctx.lease.execution_profile['resource_estimate']={'ram_bytes':536870912}
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and not artifacts.staged


def test_aggregate_rejects_duplicate_effect_pair_even_with_distinct_artifacts():
    artifacts=Artifacts();color=artifacts.contracts[artifacts.value.color_fits[0].artifact_id];refs=[]
    for digest in ('7','8'):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=digest*64);params=BeautyFitParams.model_construct(pair_id=color.params.pair_id,before=color.params.before,after=color.params.after)
        artifacts.contracts[ref.artifact_id]=BeautyFit.model_construct(params=params,beauty=Disabled(enabled=False),confidence=0.,faces=[],search_digest='9'*64,warnings=[]);refs.append(ref)
    artifacts.value=artifacts.value.model_copy(update={'beauty_fits':refs});ctx=context_for(artifacts.value.color_fits[0],'style.aggregate');ctx.lease.execution_profile['resource_estimate']={'ram_bytes':536870912}
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and not artifacts.staged
