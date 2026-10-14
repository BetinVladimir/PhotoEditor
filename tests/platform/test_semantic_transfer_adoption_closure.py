from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash,ArtifactRef as ContractRef
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import TransferCandidate
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid,prepare_v2_adoption
from test_v2_adoption import Reader
from test_image_denoise import Artifacts
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','role','missing','unreachable','unsupported','params_missing','canvas','scratch_missing','empty','range','shape'])
def test_candidate_adoption_typed_mask_transform_closure(tmp_path,variant):
    a=Artifacts(tmp_path);params,_,_=transfer_documents();reader=Reader()
    def put(value,role,source='output'):
        ref=ArtifactRef(uuid4(),canonical_hash(value));reader.add(ref,body=value.model_dump(mode='json'),role=role,source=source);return ref
    source=put(a.image,None,'input')
    binding=params.reference.model_copy(update={'source':ContractRef(artifact_id=source.artifact_id,sha256=source.sha256),'canvas_hash':canonical_hash(a.image.canvas)})
    params=params.model_copy(update={'reference':binding,'target':binding})
    params_ref=put(params,None,'input')
    if variant=='unsupported':
        candidate=TransferCandidate(schema_version=1,target=params.target,mask=None,transform=None,
            confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
        root=put(candidate,'document')
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='semantic.transfer',version='1',root=root,reader=reader,params_ref=params_ref)
        assert len(prepared.inventory)==2;return
    from io import BytesIO
    from hashlib import sha256
    from dataclasses import replace
    import numpy as np
    data=np.ones((20,24),np.float32)
    if variant=='empty':data[:]=0
    if variant=='range':data[0,0]=1.1
    if variant=='shape':data=data[:10]
    stream=BytesIO();np.save(stream,data,allow_pickle=False);body=stream.getvalue()
    pixels=ArtifactRef(uuid4(),sha256(body).hexdigest());reader.add(pixels,role='mask_pixels',media='application/x-npy')
    reader.rows[pixels]=replace(reader.rows[pixels],bytes=len(body))
    reader.iter_bytes=lambda ref,max_bytes:iter([body])
    scratch=tmp_path/'adoption';scratch.mkdir()
    mask=MaskRef(artifact={'artifact_id':pixels.artifact_id,'sha256':pixels.sha256},canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='manual',confidence=.8,model_lock_id=params.model_lock_sha,protected=False)
    if variant=='canvas':mask.canvas=mask.canvas.model_copy(update={'space_id':'wrong'})
    mask_ref=put(mask,'mask')
    transform=ResolvedTransform(schema_version=1,from_canvas_hash=params.reference.canvas_hash,
        to_canvas_hash=params.target.canvas_hash,matrix=[1,0,0,0,1,0,0,0,1],kind='homography')
    transform_ref=put(transform,'transform' if variant!='role' else 'mask')
    candidate=TransferCandidate(schema_version=1,target=params.target,
        mask={'artifact_id':mask_ref.artifact_id,'sha256':mask_ref.sha256},
        transform={'artifact_id':transform_ref.artifact_id,'sha256':transform_ref.sha256},confidence=.8,state='proposed',reason=None)
    root=put(candidate,'document')
    if variant=='missing':del reader.rows[pixels]
    if variant=='unreachable':reader.add(ArtifactRef(uuid4(),'f'*64),role='mask_pixels',media='application/x-npy')
    def run():return prepare_v2_adoption(task_id=uuid4(),operation='semantic.transfer',version='1',root=root,reader=reader,
        params_ref=None if variant=='params_missing' else params_ref,
        layer_scratch_root=None if variant=='scratch_missing' else scratch,layer_scratch_bytes=len(body))
    if variant=='valid':
        prepared=run()
        assert {row['artifact_id'] for row in prepared.inventory}=={str(ref.artifact_id) for ref in (root,source,mask_ref,pixels,transform_ref)}
    else:
        with pytest.raises(AdoptionInvalid):run()
    assert not list(scratch.iterdir())

