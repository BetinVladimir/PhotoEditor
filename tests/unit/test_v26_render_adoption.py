from hashlib import sha256
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.render_adoption import verify_render_metadata
from test_layer_adoption_bytes import fixture as layer_fixture


def fixture(*,legacy_alpha=False):
    reader,params,root,objects,_=layer_fixture(legacy_alpha=legacy_alpha)
    def put(value,scope,role):
        raw=canonical_bytes(value);ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
        objects[ref.artifact_id]=(raw,'application/json',scope,role);return ref
    value=parse_json(objects[params.artifact_id][0]);binding=VersionBinding.model_validate_json(canonical_bytes(value['base']))
    recipe=dict(schema_version=3,source=value['base'],base_style=dict(artifact_id=str(uuid4()),sha256='f'*64),
        session_corrections=None,learning_version_id=None,overrides=[],layer_stack=None,renderer_version='v2.2',
        parameter_delta=dict(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0))
    frozen=put(dict(schema_version=1,recipe=recipe,recipe_sha256=canonical_hash(recipe),learning=None,
        scalars=dict(schema_version=1,source=value['base'],base_style=recipe['base_style'],effective_delta=recipe['parameter_delta'],
            renderer_version='v2.2',icc_lock_sha='a'*64,model_lock_sha='b'*64)),'input',None)
    result=parse_json(objects[root.artifact_id][0])
    report=parse_json(objects[UUID(result['report']['artifact_id'])][0])
    report['operation']='render.compose'
    report_ref=put(report,'output','report');result['report']=dict(artifact_id=str(report_ref.artifact_id),sha256=report_ref.sha256)
    root=put(result,'output','document')
    parent=value['proposal_result'];parents=(ArtifactRef(UUID(parent['artifact_id']),parent['sha256']),)
    reader.objects=objects
    return reader,frozen,root,binding,parents,put,result


def test_pinned_full_recipe_same_canvas_masks_and_parent_quality():
    reader,frozen,root,binding,parents,*_=fixture()
    _,result,output=verify_render_metadata(reader,frozen,root,binding,parents)
    assert result.working_image.artifact_id and output.dtype=='float32'


def test_task_source_substitution_rejected():
    reader,frozen,root,binding,parents,*_=fixture()
    binding.asset_version_id=uuid4()
    with pytest.raises(AdoptionInvalid,match='SOURCE_INVALID'):
        verify_render_metadata(reader,frozen,root,binding,parents)


def test_declared_parent_inventory_cannot_be_dropped():
    reader,frozen,root,binding,_,*_=fixture()
    with pytest.raises(AdoptionInvalid,match='REPORT_INVALID'):
        verify_render_metadata(reader,frozen,root,binding,())


def test_actual_output_npy_streams_are_verified_and_closed():
    from photo_worker.v2.render_adoption import verify_render_output_arrays
    reader,frozen,root,binding,parents,*_=fixture()
    verify_render_output_arrays(reader,frozen,root,binding,parents)
    assert reader.opened==reader.closed and reader.opened==3


def test_output_npy_corruption_rejected_without_leaking_stream():
    from photo_worker.v2.render_adoption import verify_render_output_arrays
    reader,frozen,root,binding,parents,put,result=fixture()
    from uuid import UUID
    # Reach the real staged RGB object and corrupt bytes without changing its pin.
    image=reader.read_json(ArtifactRef(UUID(result['working_image']['artifact_id']),result['working_image']['sha256']),1048576)
    pixels=parse_json(image)['pixels']
    ref=ArtifactRef(UUID(pixels['artifact_id']),pixels['sha256'])
    original=reader.iter_bytes
    def corrupt(reference,limit):
        for block in original(reference,limit):
            if reference==ref:yield bytes([block[0]^1])+block[1:]
            else:yield block
    reader.iter_bytes=corrupt
    with pytest.raises(AdoptionInvalid,match='LENS_ARRAY'):
        verify_render_output_arrays(reader,frozen,root,binding,parents)
    assert reader.opened==reader.closed


@pytest.mark.parametrize('value',[.5,float('nan'),1.1])
def test_rehashed_nonbinary_changed_mask_rejected(value):
    from io import BytesIO
    import numpy as np
    from photo_worker.v2.render_adoption import verify_render_output_arrays
    reader,frozen,root,binding,parents,put,result=fixture()
    mask=parse_json(reader.objects[UUID(result['changed_mask']['artifact_id'])][0])
    stream=BytesIO();np.save(stream,np.full((2,2),value,np.float32),allow_pickle=False)
    raw=stream.getvalue();pixels=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    reader.objects[pixels.artifact_id]=(raw,'application/x-npy','output','mask_pixels')
    mask['artifact']=dict(artifact_id=str(pixels.artifact_id),sha256=pixels.sha256)
    ref=put(mask,'output','mask');result['changed_mask']=dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256)
    root=put(result,'output','document')
    with pytest.raises(AdoptionInvalid,match='VALUES_INVALID'):
        verify_render_output_arrays(reader,frozen,root,binding,parents)
    assert reader.opened==reader.closed


def test_exact_final_change_coverage_proof_and_cleanup(tmp_path):
    from photo_worker.v2.render_adoption import verify_render_changes
    reader,frozen,root,binding,parents,*_=fixture()
    verify_render_changes(reader,frozen,root,binding,parents,scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert reader.opened==reader.closed
    assert not list(tmp_path.iterdir())


def test_rehashed_binary_mask_with_wrong_coverage_rejected(tmp_path):
    from io import BytesIO
    import numpy as np
    from photo_worker.v2.render_adoption import verify_render_changes
    reader,frozen,root,binding,parents,put,result=fixture()
    mask=parse_json(reader.objects[UUID(result['changed_mask']['artifact_id'])][0])
    stream=BytesIO();np.save(stream,np.zeros((2,2),np.float32),allow_pickle=False)
    raw=stream.getvalue();ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    reader.objects[ref.artifact_id]=(raw,'application/x-npy','output','mask_pixels')
    mask['artifact']=dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256)
    maskref=put(mask,'output','mask');result['changed_mask']=dict(artifact_id=str(maskref.artifact_id),sha256=maskref.sha256)
    root=put(result,'output','document')
    with pytest.raises(AdoptionInvalid,match='CHANGED_MASK_INVALID'):
        verify_render_changes(reader,frozen,root,binding,parents,scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


def test_changed_protected_pixel_rejected_with_actual_pinned_mask(tmp_path):
    from io import BytesIO
    import numpy as np
    from photo_worker.v2.render_adoption import verify_render_changes
    reader,frozen,root,binding,parents,put,result=fixture()
    protection=parse_json(reader.objects[UUID(result['changed_mask']['artifact_id'])][0])
    stream=BytesIO();np.save(stream,np.full((2,2),.001,np.float32),allow_pickle=False)
    raw=stream.getvalue();ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    reader.objects[ref.artifact_id]=(raw,'application/x-npy','input',None)
    protection.update(artifact=dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256),protected=True,selector='manual')
    maskref=put(protection,'input',None)
    recipe=parse_json(reader.objects[frozen.artifact_id][0])
    recipe['recipe']['protected_mask']=dict(artifact_id=str(maskref.artifact_id),sha256=maskref.sha256)
    recipe['recipe_sha256']=canonical_hash(recipe['recipe']);frozen=put(recipe,'input',None)
    with pytest.raises(AdoptionInvalid,match='PROTECTED_PIXEL_CHANGED'):
        verify_render_changes(reader,frozen,root,binding,parents,scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


def test_legacy_alpha_normalization_is_explicit_and_output_remains_float32(tmp_path):
    from photo_worker.v2.render_adoption import verify_render_changes
    reader,frozen,root,binding,parents,*_=fixture(legacy_alpha=True)
    verify_render_changes(reader,frozen,root,binding,parents,scratch_root=tmp_path,max_bytes=100000,
        max_tile_bytes=4096,alpha_mode='normalize_uint16')
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


def test_legacy_alpha_pin_cannot_be_reused_in_float32_output(tmp_path):
    from photo_worker.v2.render_adoption import verify_render_changes
    reader,frozen,root,binding,parents,put,result=fixture(legacy_alpha=True)
    source=parse_json(reader.objects[binding.source.artifact_id][0])
    output=parse_json(reader.objects[UUID(result['working_image']['artifact_id'])][0])
    output['alpha']=source['alpha'];image=put(output,'output','working_image')
    result['working_image']=dict(artifact_id=str(image.artifact_id),sha256=image.sha256)
    root=put(result,'output','document')
    with pytest.raises(AdoptionInvalid,match='ALPHA_REUSE_INVALID'):
        verify_render_changes(reader,frozen,root,binding,parents,scratch_root=tmp_path,max_bytes=100000,
            max_tile_bytes=4096,alpha_mode='normalize_uint16')
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())
