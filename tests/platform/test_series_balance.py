import hashlib
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.series import RegionStats,SeriesParams,SeriesStats
from photo_worker.operations.series.balance import execute
from test_asset_adapters import context_for


def stats(rgb,confidence=1):
    return SeriesStats(item_id=uuid4(),image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),regions=[RegionStats(region_id='neutral-wall',mean_rgb=rgb,mean_y=sum(rgb)/3,coverage=.2,confidence=confidence)],clipping_fraction=0,warnings=[])


class Artifacts:
    def __init__(self,values,reference=None):
        self.contracts={};self.staged=[];refs=[]
        for value in values:r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest());refs.append(r);self.contracts[r.artifact_id]=value
        self.value=SeriesParams(scene_id='scene-one',stats=refs,reference_item_id=reference)
    def params(self,model):return self.value
    def contract(self,r,model):return self.contracts[r.artifact_id]
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def test_series_balance_applies_bounded_improving_correction():
    reference=stats([.4,.5,.6]);other=stats([.32,.4,.48]);a=Artifacts([reference,other],reference.item_id);ctx=context_for(a.value.stats[0],'series.balance');result=execute(ctx,a);correction=next(x for x in a.staged[0][1].items if x.item_id==other.item_id)
    assert correction.accepted and correction.reason=='applied' and abs(correction.applied_ev)<=.35
    assert all(.95<=v<=1.05 for v in correction.applied_gains) and not result.review_required


def test_series_balance_low_confidence_is_identity():
    reference=stats([.4,.5,.6]);other=stats([.3,.4,.5],.5);a=Artifacts([reference,other],reference.item_id);ctx=context_for(a.value.stats[0],'series.balance');result=execute(ctx,a);correction=next(x for x in a.staged[0][1].items if x.item_id==other.item_id)
    assert not correction.accepted and correction.reason=='low_confidence' and correction.applied_ev==0 and correction.applied_gains==[1,1,1]
    assert result.review_required
