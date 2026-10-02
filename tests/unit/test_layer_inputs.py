from contextlib import contextmanager
from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from test_layer_parents import setup
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.layers.inputs import prepare_layer_inputs
from photo_worker.operations.layers.result import stage_layer_result


@pytest.mark.parametrize('normalize',[False,True])
@pytest.mark.parametrize('blend,proposal_generated,expected',[
    ('normal',False,[[0,0]]),('multiply',False,[[0,1]]),('screen',False,[[0,1]]),('normal',True,[[0,1]])])
def test_verified_inputs_prepare_and_stage_generated_closure(tmp_path,blend,proposal_generated,expected,normalize):
    params,documents,artifacts,coverage=setup(tmp_path)
    params.blend=blend
    proposal_parent=documents[params.proposal_result.artifact_id]
    if not proposal_generated:proposal_parent.generated_mask=None
    source=documents[params.base.source.artifact_id]
    pixels=tmp_path/'rgb.npy';np.save(pixels,np.ones((1,2,3),np.float32),allow_pickle=False)
    source_mask=documents[documents[params.source_result.artifact_id].generated_mask.artifact_id]
    paths={source.pixels.artifact_id:pixels,source_mask.artifact.artifact_id:coverage}
    if normalize:
        alpha_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
        source.alpha=alpha_ref
        documents[proposal_parent.working_image.artifact_id].alpha=alpha_ref
        alpha_path=tmp_path/'legacy-alpha.npy'
        np.save(alpha_path,np.array([[0,32768]],np.uint16),allow_pickle=False)
        paths[alpha_ref.artifact_id]=alpha_path
    (tmp_path/'scratch').mkdir()
    space=ScratchSpace(tmp_path/'scratch',uuid4(),uuid4(),1,65536,min_free_bytes=0)
    artifacts.scratch=space;values={};roles=[]
    @contextmanager
    def input(reference,**kwargs):
        artifacts.opens+=1
        try:yield SimpleNamespace(path=paths[reference.artifact_id])
        finally:artifacts.closes+=1
    def put(body,role,value):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest());values[ref.artifact_id]=value;roles.append(role);return ref
    def stage(path,reservation,role,media):
        reservation.validate_file(path);assert media=='application/x-npy'
        return put(path.read_bytes(),role,np.load(path,allow_pickle=False))
    artifacts.input=input;artifacts.stage=stage
    artifacts.stage_contract=lambda value,role:put(canonical_bytes(value),role,value)
    try:
        from photo_worker.operations.layers.resources import layer_resources
        mode='normalize_uint16' if normalize else 'require_float32'
        profile={'resource_estimate':layer_resources(2,1,mask_refs=1,mask_nodes=1,tile_size=256,alpha_mode=mode),'layer_alpha_mode':mode}
        with prepare_layer_inputs(params,artifacts,max_tile_bytes=4096,tile_size=256,execution_profile=profile) as (parents,files):
            staged=stage_layer_result(artifacts,params.base.source,parents.source,params.base,files,
                source_result=parents.source_result,proposal_result=parents.proposal_result)
            result=values[staged.document.artifact_id]
            generated=values[result.generated_mask.artifact_id]
            np.testing.assert_array_equal(values[generated.artifact.artifact_id],expected)
            assert generated.canvas==source.canvas and generated.transform_chain==source.transform_chain
            if blend=='normal':
                assert (result.working_image==params.base.source)==(not normalize)
                assert files.any_changed is False
                np.testing.assert_array_equal(values[values[result.changed_mask.artifact_id].artifact.artifact_id],0)
            if normalize:
                image=values[result.working_image.artifact_id]
                assert image.alpha!=source.alpha
                np.testing.assert_array_equal(values[image.alpha.artifact_id],np.array([[0,32768]],np.float32)/np.float32(65535))
                assert files.alpha_converted is True
                assert 'alpha' in roles
                np.testing.assert_array_equal(np.load(alpha_path,allow_pickle=False),[[0,32768]])
            assert 'mask_pixels' in roles and len(staged.outputs)>=6
            before=len(roles)
            foreign=proposal_parent.model_copy(update={'generated_mask':ArtifactRef(artifact_id=uuid4(),sha256='d'*64)})
            with pytest.raises(ValueError,match='GENERATED_BINDING_INVALID'):
                stage_layer_result(artifacts,params.base.source,parents.source,params.base,files,
                    source_result=parents.source_result,proposal_result=foreign)
            assert len(roles)==before
        assert artifacts.opens==artifacts.closes
        assert not list(space.directory.rglob('*.npy'))
        if normalize:
            strict={'resource_estimate':layer_resources(2,1,mask_refs=1,mask_nodes=1,tile_size=256),
                    'layer_alpha_mode':'require_float32'}
            with pytest.raises(ValueError,match='ARRAY_LAYOUT_INVALID'):
                with prepare_layer_inputs(params,artifacts,max_tile_bytes=4096,tile_size=256,execution_profile=strict):
                    pytest.fail('implicit uint16 conversion')
            assert artifacts.opens==artifacts.closes
            assert not list(space.directory.rglob('*.npy'))
        with pytest.raises(ValueError,match='CLOSED'):parents.read_source_generated(0,0,1,1)
    finally:space.cleanup()


def test_resource_admission_fails_before_any_array_download(tmp_path):
    from photo_worker.operations.layers.resources import layer_resources
    from photo_worker.runtime.executor import OperationFailure
    params,documents,artifacts,path=setup(tmp_path)
    estimate=layer_resources(2,1,mask_refs=1,mask_nodes=1)
    estimate['ram_bytes']-=1
    with pytest.raises(OperationFailure) as caught:
        with prepare_layer_inputs(params,artifacts,max_tile_bytes=4096,execution_profile={'resource_estimate':estimate}):pytest.fail('unadmitted arrays')
    assert caught.value.error.code=='RAM_ADMISSION_EXCEEDED' and artifacts.opens==0
