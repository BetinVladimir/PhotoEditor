from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash,ArtifactRef as ContractRef
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import TransferCandidate
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_adoption import verify_semantic_geometry
from test_v2_adoption import Reader
from test_image_denoise import Artifacts
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','canvas','confidence','model','transform','reflection','target',
                                     'candidate_role','mask_role','transform_role'])
def test_semantic_adoption_pinned_mask_and_transform(tmp_path,variant):
    a=Artifacts(tmp_path);params,_,_=transfer_documents();reader=Reader()
    def put(value,source='output',role='document'):
        value_ref=ArtifactRef(uuid4(),canonical_hash(value));reader.add(value_ref,body=value.model_dump(mode='json'),source=source,role=role)
        return dict(artifact_id=value_ref.artifact_id,sha256=value_ref.sha256)
    image_ref=put(a.image,'input');binding=params.reference.model_copy(update={'source':ContractRef.model_validate(image_ref),'canvas_hash':canonical_hash(a.image.canvas)})
    params=params.model_copy(update={'reference':binding,'target':binding})
    mask=MaskRef(artifact={'artifact_id':uuid4(),'sha256':'a'*64},canvas=a.image.canvas,transform_chain=a.image.transform_chain,
        selector='manual',confidence=.8,model_lock_id=params.model_lock_sha,protected=False)
    if variant=='canvas':mask.canvas=mask.canvas.model_copy(update={'space_id':'wrong'})
    if variant=='confidence':mask.confidence=.7
    if variant=='model':mask.model_lock_id='f'*64
    transform=ResolvedTransform(schema_version=1,from_canvas_hash=binding.canvas_hash,to_canvas_hash=binding.canvas_hash,
        matrix=[1,0,0,0,1,0,0,0,1],kind='homography')
    if variant=='transform':transform.to_canvas_hash='f'*64
    if variant=='reflection':transform.matrix[0]=-1
    candidate=TransferCandidate(schema_version=1,target=binding,
        mask=put(mask,role='document' if variant=='mask_role' else 'mask'),
        transform=put(transform,role='document' if variant=='transform_role' else 'transform'),
        confidence=.8,state='proposed',reason=None)
    if variant=='target':candidate.target=candidate.target.model_copy(update={'asset_version_id':uuid4()})
    def typed(value):return ArtifactRef(value['artifact_id'],value['sha256'])
    params_ref=typed(put(params,'input'));root=typed(put(candidate,role='mask' if variant=='candidate_role' else 'document'))
    if variant=='valid':verify_semantic_geometry(reader,params_ref,root)
    else:
        with pytest.raises(AdoptionInvalid,match='SEMANTIC_ADOPTION_'):verify_semantic_geometry(reader,params_ref,root)
