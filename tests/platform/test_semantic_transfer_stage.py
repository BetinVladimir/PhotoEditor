from uuid import uuid4
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.algorithms.semantic_transfer import PlanarMatch
from photo_worker.operations.semantic.prepare_transfer import PreparedPlanarTransfer,prepare_planar_mask_transfer,planar_mask_resources
from photo_worker.operations.semantic.stage_transfer import stage_planar_transfer
from test_image_denoise import Artifacts
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['proposed','unsupported','binding'])
def test_transfer_stager_complete_typed_closure(tmp_path,variant):
    a=Artifacts(tmp_path);params,_,_=transfer_documents()
    params=params.model_copy(update={'reference':params.reference.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)}),
        'target':params.target.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)})})
    if variant=='binding':params=params.model_copy(update={'target':params.target.model_copy(update={'canvas_hash':'f'*64})})
    stages=[]
    def document(value,role):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value));stages.append((role,value,ref));return ref
    def pixels(path,owner,role,media):
        owner.validate_file(path)
        assert media=='application/x-npy'
        array=np.load(path,allow_pickle=False)
        assert array.dtype==np.float32 and np.all(array==1)
        ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64);stages.append((role,None,ref));return ref
    artifacts=SimpleNamespace(stage=pixels,stage_contract=document)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        if variant=='unsupported':
            prepared=PreparedPlanarTransfer(PlanarMatch(None,0,'NO_PLANAR_CORRESPONDENCE'),None)
            result=stage_planar_transfer(artifacts,prepared,params,a.image,a.image)
            assert [role for role,_,_ in stages]==['document']
            assert result.candidate.state=='unsupported' and result.candidate.mask is None
            assert result.bytes_written==len(canonical_bytes(result.candidate))
        else:
            source=np.ones((20,24),np.float32);zero=np.zeros_like(source)
            with prepare_planar_mask_transfer(source,np.eye(3),source.shape,source_protected=zero,
                    target_protected=zero,scratch=scratch,
                    execution_profile={'resource_estimate':planar_mask_resources(source.shape,source.shape)}) as mask:
                prepared=PreparedPlanarTransfer(PlanarMatch(np.eye(3),1,None),mask)
                if variant=='binding':
                    with pytest.raises(ValueError,match='BINDING_INVALID'):
                        stage_planar_transfer(artifacts,prepared,params,a.image,a.image)
                    assert stages==[]
                else:
                    result=stage_planar_transfer(artifacts,prepared,params,a.image,a.image)
                    assert [role for role,_,_ in stages]==['mask_pixels','mask','transform','document']
                    assert stages[1][1].artifact==stages[0][2]
                    assert result.candidate.mask==stages[1][2] and result.candidate.transform==stages[2][2]
                    assert result.candidate.target==params.target and len(result.outputs)==4
                    expected=mask.path.stat().st_size+sum(len(canonical_bytes(value)) for _,value,_ in stages if value is not None)
                    assert result.bytes_written==expected
        assert not list(scratch.directory.iterdir())
