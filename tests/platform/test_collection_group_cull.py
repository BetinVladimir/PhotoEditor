from datetime import datetime,timezone,timedelta
import hashlib
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.collection import CullParams,CullRecommendations,GroupParams,SceneFeatures,SceneGroups
from photo_worker.operations.collection.group import execute as group
from photo_worker.operations.collection.cull import execute as cull
from test_asset_adapters import context_for


def feature(index,angle,when,eyes='open'):
    vector=np.zeros(384);vector[0]=np.cos(angle);vector[1]=np.sin(angle)
    return SceneFeatures(item_id=uuid4(),image=ArtifactRef(artifact_id=uuid4(),sha256=hex(index+1)[2:]*64 if index<15 else 'a'*64),content_sha256=hashlib.sha256(str(index).encode()).hexdigest(),phash64=f'{index:016x}',dino=vector.tolist(),stats20=[.1*index]*20,captured_at=when,sharpness=10+index,blur=.1*index,clipping=.02*index,eyes=eyes)


class Artifacts:
    def __init__(self,values,params):
        self.contracts={};self.staged=[];self.value=params;self.refs=[]
        for v in values:
            r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(v)).hexdigest());self.refs.append(r);self.contracts[r.artifact_id]=v
    def params(self,model):return self.value
    def contract(self,r,model):return self.contracts[r.artifact_id]
    def stage_contract(self,value,role):self.staged.append((role,value));r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest());self.contracts[r.artifact_id]=value;return r


def test_group_complete_link_prevents_similarity_chaining():
    now=datetime.now(timezone.utc);values=[feature(0,0,now),feature(1,.25,now+timedelta(seconds=1)),feature(2,.5,now+timedelta(seconds=2))]
    a=Artifacts([],None)
    for v in values:r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(v)).hexdigest());a.refs.append(r);a.contracts[r.artifact_id]=v
    a.value=GroupParams(features=a.refs);ctx=context_for(a.refs[0],'collection.group');ctx.lease.execution_profile['backend']='cpu';group(ctx,a);result=a.staged[0][1]
    # A-B and B-C can be close while A-C is not: complete-link cannot put all three together.
    assert result.scene_count>=2 and len({i.item_id for i in result.items})==3


def test_cull_preserves_manual_pick_and_flags_eye_review():
    now=datetime.now(timezone.utc);values=[feature(0,0,now,'unknown'),feature(1,.05,now,'closed'),feature(2,.1,now,'open')];a=Artifacts([],None)
    for v in values:r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(v)).hexdigest());a.refs.append(r);a.contracts[r.artifact_id]=v
    a.value=GroupParams(features=a.refs);ctx=context_for(a.refs[0],'collection.group');groups_ref=group(ctx,a).outputs[0];groups=a.contracts[groups_ref.artifact_id]
    a.value=CullParams(groups=groups_ref,features=a.refs,target_count=1,manual_picks=[values[1].item_id]);ctx=context_for(groups_ref,'collection.cull');cull(ctx,a);result=a.staged[-1][1];CullRecommendations.model_validate_json(canonical_bytes(result))
    selected=[i for i in result.items if i.selected];assert len(selected)==1 and selected[0].item_id==values[1].item_id and selected[0].manual
    assert 'unknown_eye_state' in result.warnings and 'closed_eye_review' in result.warnings
