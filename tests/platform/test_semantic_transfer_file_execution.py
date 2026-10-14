"""Joined pinned file loading/native transfer; catalog authority is controlled."""
from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import cv2
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage,MaskRef
from photo_worker.operations.semantic.load_inputs import PinnedSemanticArrayLoader
from photo_worker.operations.semantic.transfer import execute_transfer
from photo_worker.operations.semantic.resources import semantic_transfer_resources
from photo_worker.runtime.scratch import ScratchSpace
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('protected',[False,True])
def test_pinned_files_to_native_candidate_closure(tmp_path,protected):
    params,intent,calibration=transfer_documents();documents={};paths={};events=[];outputs=[];sizes=[]
    def doc(value):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value));documents[ref.artifact_id]=value;return ref
    def array(value,name):
        path=tmp_path/(name+'.npy');np.save(path,value,allow_pickle=False)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest());paths[ref.artifact_id]=path;return ref
    rng=np.random.default_rng(106)
    source=np.repeat(rng.random((256,256),dtype=np.float32)[:,:,None],3,axis=2)
    target=cv2.warpAffine(source,np.array([[1,0,4],[0,1,6]],float),(256,256))
    chain=TransformChain.create([])
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24')
    def image(value,name):
        return WorkingImage(pixels=array(value,name),canvas=Canvas(width=256,height=256,orientation=1,
            space_id=name,source_sha256=sha256(value.tobytes()).hexdigest()),color=color,dtype='float32',alpha=None,transform_chain=chain)
    reference=image(source,'reference');destination=image(target,'target')
    reference_ref=doc(reference);target_ref=doc(destination)
    params=params.model_copy(update={'reference':params.reference.model_copy(update={'source':reference_ref,'canvas_hash':canonical_hash(reference.canvas)}),
        'target':params.target.model_copy(update={'source':target_ref,'canvas_hash':canonical_hash(destination.canvas)}),'calibration':doc(calibration)})
    selected=np.zeros((256,256),np.float32);selected[50:100,60:110]=1
    def mask(value,image,name,is_protected):
        return doc(MaskRef(artifact=array(value,name),canvas=image.canvas,transform_chain=chain,selector='manual',
            confidence=1,model_lock_id=None,protected=is_protected))
    zero=np.zeros_like(selected);target_protection=zero.copy()
    if protected:target_protection[75,85]=.001
    intent=intent.model_copy(update={'reference':params.reference,'reference_mask':mask(selected,reference,'selected',False)})
    params=params.model_copy(update={'intent':doc(intent)})
    loader=PinnedSemanticArrayLoader(mask(zero,reference,'source-protection',True),
        mask(target_protection,destination,'target-protection',True),verify_inputs=lambda *args:events.append('inputs'))
    @contextmanager
    def input(ref,**kwargs):
        path=paths[ref.artifact_id];assert sha256(path.read_bytes()).hexdigest()==ref.sha256
        events.append('map')
        try:yield SimpleNamespace(path=path)
        finally:events.append('release')
    def stage(path,owner,role,media):
        owner.validate_file(path);value=np.load(path,allow_pickle=False)
        assert value[75,85]>.9 and media=='application/x-npy'
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
        outputs.append((role,ref,None));sizes.append(path.stat().st_size);return ref
    def stage_document(value,role):
        ref=doc(value);outputs.append((role,ref,value));sizes.append(len(canonical_bytes(value)));return ref
    costs=semantic_transfer_resources((256,256),(256,256));costs['scratch_bytes']+=16*1024*1024
    costs['ram_bytes']+=16*1024*1024
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,costs['scratch_bytes'],min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch,params=lambda model:params,contract=lambda ref,model:documents[ref.artifact_id],
            input=input,stage=stage,stage_contract=stage_document)
        ctx=SimpleNamespace(check_active=lambda:None,progress=lambda *args:None,lease=SimpleNamespace(execution_profile={'resource_estimate':costs}))
        result=execute_transfer(ctx,artifacts,load_arrays=loader,verify_runtime=lambda:events.append('runtime'),
            verify_scene=lambda *args:events.append('scene'))
        assert events[0]=='runtime' and events.count('map')==events.count('release')==5
        assert result.bytes_written==sum(sizes) and result.outputs==[outputs[-1][1]]
        candidate=outputs[-1][2]
        if protected:
            assert candidate.state=='unsupported' and not result.review_required
            assert [role for role,_,_ in outputs]==['document']
        else:
            assert candidate.state=='proposed' and result.review_required
            assert [role for role,_,_ in outputs]==['mask_pixels','mask','transform','document']
            assert candidate.mask==outputs[1][1] and candidate.transform==outputs[2][1]
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(np.load(paths[reference.pixels.artifact_id]),source)
    np.testing.assert_array_equal(np.load(paths[destination.pixels.artifact_id]),target)
