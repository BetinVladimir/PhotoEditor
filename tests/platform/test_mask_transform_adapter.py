from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef,TransformChain,ResizeTransform,Rect
from photo_worker.operations.mask.transform import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_mask_raster_adapter import MaskArtifacts
from test_image_color_adapter import context


def setup(root,labels):
    a=MaskArtifacts(root);canvas=a.image.canvas
    step=ResizeTransform(kind='resize',from_canvas=canvas,to_canvas=canvas,pixel_centers='half_pixel',invertibility='invertible',valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height))
    chain=TransformChain.create([step])
    target=a.image.model_copy(update={'transform_chain':chain})
    values=np.full((canvas.height,canvas.width),7 if labels else .4,np.uint16 if labels else np.float32)
    mask=MaskRef(artifact=a.array(root,'source-mask',values),canvas=canvas,transform_chain=TransformChain.create([]),object_id=uuid4(),selector='manual',confidence=.9,model_lock_id=None,protected=True)
    a.raw=dict(mask=mask.model_dump(mode='json'),target=a.add(target).model_dump(mode='json'),step=step.model_dump(mode='json'),dtype='uint16' if labels else 'float32')
    ctx=context(a);ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    return a,ctx,mask,chain

@pytest.mark.parametrize('labels',[False,True])
def test_transform_task_preserves_provenance_and_publishes(tmp_path,labels):
    a,ctx,mask,chain=setup(tmp_path,labels);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    manifest=a.staged[0][1]
    assert manifest.mask.transform_chain==chain
    assert manifest.mask.object_id==mask.object_id and manifest.mask.confidence==.9 and manifest.mask.protected
    assert len(result.outputs)==3 and ctx.snapshot_progress()==dict(done=2,total=2)
    assert np.allclose(a.outputs['transformed_mask'],7 if labels else .4,atol=1e-6)
    assert (manifest.guided_radius is None)==labels


def test_target_chain_mismatch_rejected_before_io(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    a.raw['target']=a.image_ref.model_dump(mode='json')
    with pytest.raises(OperationFailure) as e:execute(ctx,a)
    assert e.value.error.code=='INPUT_INVALID' and not a.uploads



def test_transform_regions_share_exact_geometry_preserve_identity_and_deduplicate(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    regional=mask.model_copy(update={'protected':False,'selector':'sky'})
    a.raw['regions']=[{'region_id':'sky-1','mask':regional.model_dump(mode='json')},
                      {'region_id':'sky-2','mask':regional.model_copy(update={'confidence':.8,'selector':'manual'}).model_dump(mode='json')}]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    manifest=a.staged[0][1]
    assert len(result.outputs)==5
    assert [r.region_id for r in manifest.regions]==['sky-1','sky-2']
    first,second=manifest.regions
    assert first.mask.artifact==second.mask.artifact
    assert first.mask.transform_chain==chain and second.mask.transform_chain==chain
    assert first.mask.object_id==regional.object_id and first.mask.confidence==.9
    assert second.mask.confidence==.8 and second.mask.selector=='manual'
    assert not first.mask.protected and not second.mask.protected
    assert first.png==second.png


@pytest.mark.parametrize('change',[{'protected':True},{'confidence':.74},{'selector':'person'}])
def test_transform_regions_reject_unapproved_source_before_io(tmp_path,change):
    a,ctx,mask,chain=setup(tmp_path,False)
    regional=mask.model_copy(update={'protected':False,'selector':'sky',**change})
    a.raw['regions']=[{'region_id':'sky-1','mask':regional.model_dump(mode='json')}]
    with pytest.raises(OperationFailure) as exc:execute(ctx,a)
    assert exc.value.error.code=='INPUT_INVALID' and not a.uploads


def test_transform_region_source_chain_must_match_primary(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    regional=mask.model_copy(update={'protected':False,'selector':'sky','transform_chain':chain})
    a.raw['regions']=[{'region_id':'sky-1','mask':regional.model_dump(mode='json')}]
    with pytest.raises(OperationFailure) as exc:execute(ctx,a)
    assert exc.value.error.code=='INPUT_INVALID' and not a.uploads



def test_transform_region_wire_output_limit_refuses_before_io(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    regional=mask.model_copy(update={'protected':False,'selector':'manual'})
    a.raw['regions']=[{'region_id':f'r-{i}','mask':regional.model_copy(update={'artifact':regional.artifact.model_copy(update={'artifact_id':uuid4()})}).model_dump(mode='json')} for i in range(63)]
    with pytest.raises(OperationFailure) as exc:execute(ctx,a)
    assert exc.value.error.code=='INPUT_INVALID' and not a.uploads


def test_transform_region_cancel_does_not_publish_canonical_root(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    regional=mask.model_copy(update={'protected':False,'selector':'manual'})
    a.raw['regions']=[{'region_id':'r','mask':regional.model_dump(mode='json')}]
    original=ctx.check_active
    def active():
        if len(a.uploads)>=2:raise OperationFailure('CANCELLED')
        original()
    ctx.check_active=active
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with pytest.raises(OperationFailure):execute(ctx,a)
    assert a.staged==[]


def test_transform_portrait_and_protection_preserve_independent_probability_provenance(tmp_path):
    a,ctx,mask,chain=setup(tmp_path,False)
    portrait=mask.model_copy(update={'protected':False,'selector':'teeth','confidence':.6})
    a.raw['regions']=[{'region_id':'teeth','purpose':'portrait_teeth','mask':portrait.model_dump(mode='json')},
                      {'region_id':'protect','purpose':'protected','mask':mask.model_dump(mode='json')}]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    manifest=a.staged[0][1]
    teeth,protection=manifest.regions
    assert teeth.purpose=='portrait_teeth' and teeth.mask.selector=='teeth' and not teeth.mask.protected
    assert protection.purpose=='protected' and protection.mask.protected
    assert teeth.mask.artifact==protection.mask.artifact
    assert teeth.mask.confidence==.6 and protection.mask.confidence==.9
    assert teeth.mask.transform_chain==chain and protection.mask.transform_chain==chain
    assert len(result.outputs)==5


@pytest.mark.parametrize('purpose',['inpaint_target','generation_target'])
def test_transform_creative_targets_retain_exact_manual_role(tmp_path,purpose):
    a,ctx,mask,chain=setup(tmp_path,False)
    target=mask.model_copy(update={'protected':False,'selector':'manual'})
    a.raw['regions']=[{'region_id':'creative','purpose':purpose,'mask':target.model_dump(mode='json')}]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(ctx,a)
    transformed=a.staged[0][1].regions[0]
    assert transformed.purpose==purpose and transformed.mask.selector=='manual'
    assert not transformed.mask.protected and transformed.mask.transform_chain==chain
