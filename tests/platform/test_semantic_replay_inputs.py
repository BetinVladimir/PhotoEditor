from io import BytesIO
from hashlib import sha256
from dataclasses import replace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_hash,ArtifactRef as ContractRef
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import TransferCandidate
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_replay_inputs import verify_pinned_semantic_replay
from test_v2_adoption import Reader
from test_image_denoise import Artifacts
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','authority','mutate','source_mask'])
def test_pinned_intent_and_protection_documents_feed_replay(tmp_path,variant):
    a=Artifacts(tmp_path);params,intent,calibration=transfer_documents();reader=Reader();bodies={};reads=[]
    def document(value,source,role=None):
        ref=ArtifactRef(uuid4(),canonical_hash(value));reader.add(ref,body=value.model_dump(mode='json'),source=source,role=role)
        return ref
    def typed(ref):return ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256)
    image_ref=document(a.image,'input')
    binding=params.reference.model_copy(update={'source':typed(image_ref),'canvas_hash':canonical_hash(a.image.canvas)})
    pixels=np.zeros((20,24),np.float32);pixels[5:10,6:11]=.5
    def mask(data,source,protected=False):
        stream=BytesIO();np.save(stream,data,allow_pickle=False);body=stream.getvalue()
        ref=ArtifactRef(uuid4(),sha256(body).hexdigest());bodies[ref]=body
        reader.add(ref,source=source,role='mask_pixels' if source=='output' else None,media='application/x-npy')
        reader.rows[ref]=replace(reader.rows[ref],bytes=len(body))
        return MaskRef(artifact=typed(ref),canvas=a.image.canvas,transform_chain=a.image.transform_chain,
            selector='manual',confidence=.8,model_lock_id=params.model_lock_sha,protected=protected)
    selected=mask(pixels,'input')
    if variant=='source_mask':selected.canvas=selected.canvas.model_copy(update={'space_id':'wrong'})
    intent=intent.model_copy(update={'reference':binding,'reference_mask':typed(document(selected,'input'))})
    params=params.model_copy(update={'reference':binding,'target':binding,'intent':typed(document(intent,'input')),
        'calibration':typed(document(calibration,'input'))})
    target=mask(pixels,'output');mask_ref=document(target,'output','mask')
    transform=ResolvedTransform(schema_version=1,from_canvas_hash=binding.canvas_hash,to_canvas_hash=binding.canvas_hash,
        matrix=[1,0,0,0,1,0,0,0,1],kind='homography')
    transform_ref=document(transform,'output','transform')
    candidate=TransferCandidate(schema_version=1,target=binding,mask=typed(mask_ref),transform=typed(transform_ref),confidence=.8,state='proposed',reason=None)
    root=document(candidate,'output','document');params_ref=document(params,'input')
    guards=[document(mask(np.zeros_like(pixels),'input',True),'input') for _ in range(2)]
    def blocks(ref,max_bytes):reads.append(ref);return iter([bodies[ref]])
    reader.iter_bytes=blocks;events=[]
    def authority(*values):
        events.append('authority')
        if variant=='authority':raise AdoptionInvalid('SEMANTIC_AUTHORITY_STALE')
        if variant=='mutate':values[3].canvas.width=8;values[4][0].protected=False
    scratch=tmp_path/'scratch';scratch.mkdir()
    def run():return verify_pinned_semantic_replay(reader,params_ref,root,source_protection_ref=guards[0],
        target_protection_ref=guards[1],verify_authority=authority,scratch_root=scratch,max_bytes=65536,max_ram_bytes=128*1024*1024)
    if variant in ('valid','mutate'):
        assert run()==25 and len(reads)==4 and events==['authority']
    else:
        with pytest.raises(AdoptionInvalid):run()
        assert reads==[]
    assert not list(scratch.iterdir())
