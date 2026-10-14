from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.manual_mask import RasterizedMask
from photo_worker.operations.portrait import relight_manual_authority as module
from photo_worker.operations.portrait.local_manual_inputs import ManualPairOrigin
from test_relight_inputs import inputs
from test_portrait_manual_admission import context,ref


@pytest.mark.parametrize('protected',[False,True])
@pytest.mark.parametrize('failure',['','binding','root','changed','fence','raster','undeclared','inputs'])
def test_selected_manual_mask_replays_original_companion(monkeypatch,protected,failure):
    params,source,_,subject,guard,*_=inputs()
    target=subject.model_copy(update={'selector':'manual','confidence':1.,'model_lock_id':None})
    guard=guard.model_copy(update={'selector':'manual','confidence':1.,'model_lock_id':None})
    mask=guard if protected else target
    selected=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(mask))
    params=params.model_copy(update={'protected' if protected else 'subject':selected})
    root=RasterizedMask(image=params.base.source,vectors=ref(),target=target,protected=guard,target_png=ref(),protected_png=ref())
    root_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(root))
    origin=ManualPairOrigin(root_ref,ref(),None)
    ctx=context();ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))
    ctx.lease.command.inputs=[selected]+([] if failure=='undeclared' else [root_ref])
    if failure=='binding':mask=mask.model_copy(update={'confidence':.5})
    calls=[];replays=[]
    def resolve(*args,**kwargs):
        calls.append(kwargs)
        if failure=='fence' and len(calls)==2:ctx.lease.fence+=1
        if failure=='inputs' and len(calls)==2:ctx.lease.command.inputs.append(ref())
        return ManualPairOrigin(ref(),origin.params,None) if failure=='changed' and len(calls)==2 else origin
    def replay(*args):
        replays.append(args)
        assert args[4:6]==(target,guard)
        if failure=='raster':raise ValueError('RASTER_VECTOR_MISMATCH')
    monkeypatch.setattr(module,'read_verified_manual_pair',replay)
    artifacts=SimpleNamespace(contract=lambda *args:root.model_copy(update={'image':ref()}) if failure=='root' else root)
    authority=module.RelightManualMaskAuthority(artifacts,SimpleNamespace(resolve=resolve))
    if failure:
        with pytest.raises((ValueError,RuntimeError)):authority.verify(ctx,params,source,mask,protected=protected)
    else:
        assert authority.verify(ctx,params,source,mask,protected=protected) is True
        assert len(calls)==2 and len(replays)==1
        if protected:assert params.subject!=selected


@pytest.mark.parametrize('failure',['','root-sha','params-sha','source','changed','fence','undeclared'])
def test_sql_origin_discovery_replays_documents_before_child_gate(failure):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.manual_mask import MaskRasterParams,ManualMask
    params,source,_,target,guard,*_=inputs()
    vectors=ManualMask(canvas=source.canvas,source_sha256=source.canvas.source_sha256,shapes=[])
    original=MaskRasterParams(image=params.base.source if failure!='source' else ref(),inline_vectors=vectors)
    original_raw=canonical_bytes(original)
    original_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(original))
    root=RasterizedMask(image=params.base.source,vectors=original_ref,target=target,protected=guard,target_png=ref(),protected_png=ref())
    root_raw=canonical_bytes(root);root_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(root))
    record=dict(root=root_ref.model_dump(mode='json'),params=original_ref.model_dump(mode='json'))
    ctx=context();ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))
    ctx.lease.command.inputs=[root_ref]+([] if failure=='undeclared' else [original_ref])
    calls=[];reads=[]
    def lookup(*args):
        calls.append(args)
        if len(calls)==2:
            assert args[3]==root_raw
            if failure=='fence':ctx.lease.fence+=1
            if failure=='changed':return dict(record,root=ref().model_dump(mode='json'))
        return record
    def read(pin,model):
        reads.append(pin)
        raw=root_raw if pin==root_ref else original_raw
        return b'x'+raw[1:] if (failure=='root-sha' and pin==root_ref) or (failure=='params-sha' and pin==original_ref) else raw
    authority=module.SQLRelightManualOrigin(SimpleNamespace(relight_manual_origin=lookup),SimpleNamespace(contract_bytes=read))
    if failure:
        with pytest.raises((ValueError,RuntimeError)):authority.resolve(ctx,params,source,guard,protected=True)
        if failure=='undeclared':assert not reads
    else:
        assert authority.resolve(ctx,params,source,guard,protected=True)==ManualPairOrigin(root_ref,original_ref,None)
        assert len(calls)==2 and calls[0][3] is None
