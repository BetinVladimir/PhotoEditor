from hashlib import sha256
from io import BytesIO
import json
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.manual_mask import RasterizedMask,MaskRasterParams
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.portrait_donor_replay import verify_donor_reconstruction
from photo_worker.v2.portrait_open_eyes_material import prepare_open_eyes_material_patch
from photo_worker.operations.portrait.donor_files import write_donor_patch_files
from photo_worker.operations.portrait.donor_result import stage_donor_result
from photo_worker.runtime.scratch import ScratchSpace
from test_portrait_donor_manual import manual_case
from test_portrait_open_eyes_stage import stage_case
from test_portrait_donor_runtime_lock import current_runtime


@pytest.mark.parametrize('mode',['ok','alpha','wrong_output','revoked','scratch','ram'])
def test_original_manual_material_algorithm_and_reconstruction(tmp_path,mode):
    args,maskreader=manual_case();params,raw,target,guard,oldroot,rootraw,oldproducer,producerraw,*_=args
    _,_,_,_,proof,faces=stage_case()
    bodies={};metadata={}
    def store(raw,role=None,origin='input',mime='application/json',ref=None):
        ref=ref or ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        ref.sha256=sha256(raw).hexdigest();bodies[ref.artifact_id]=raw
        metadata[ref.artifact_id]=(role,origin,mime)
        return ref
    image=WorkingImage.model_validate_json(raw);image.alpha=None
    stream=BytesIO();np.save(stream,np.ones((100,100,3),np.float32));pixels=store(stream.getvalue(),mime='application/x-npy')
    image.pixels=pixels
    if mode=='alpha':
        stream=BytesIO();np.save(stream,np.linspace(0.2,1,10000,dtype=np.float32).reshape(100,100))
        image.alpha=store(stream.getvalue(),mime='application/x-npy')
    raw=canonical_bytes(image)
    store(raw,ref=params.base.source);store(raw,ref=params.donor.source)
    proof=json.loads(proof);proof['source']=params.base.model_dump(mode='json');proof['donor']=params.donor.model_dump(mode='json')
    proofraw=canonical_bytes(proof);store(proofraw,ref=params.confirmation)
    facebodies=[]
    for body,binding,ref in zip(faces,(params.base,params.donor),(params.source_faces,params.donor_faces)):
        value=json.loads(body);value['binding']=binding.model_dump(mode='json');body=canonical_bytes(value)
        store(body,ref=ref);facebodies.append(body)
    store(target,ref=params.eyes_mask);store(guard,ref=params.protected)
    for doc in (target,guard):
        value=json.loads(doc)['artifact'];ref=ArtifactRef.model_validate_json(canonical_bytes(value))
        from photo_worker.v2.lens_bypass import _ref
        store(b''.join(maskreader.iter_bytes(_ref(ref),1000000)),mime='application/x-npy',ref=ref)
    producer=MaskRasterParams.model_validate_json(producerraw);producer.image=params.base.source
    producerref=store(canonical_bytes(producer))
    root=RasterizedMask.model_validate_json(rootraw);root.image=params.base.source;root.vectors=producerref
    rootref=store(canonical_bytes(root));paramref=store(canonical_bytes(params))
    class Reader:
        def evidence(self,ref):
            role,origin,mime=metadata[ref.artifact_id]
            return ArtifactEvidence(ref,mime,len(bodies[ref.artifact_id]),role,origin)
        def read_json(self,ref,maximum):return bodies[ref.artifact_id]
        def staged_outputs(self):
            from photo_worker.v2.adoption import ArtifactRef as EvidenceRef
            return {EvidenceRef(key,sha256(bodies[key]).hexdigest()) for key,value in metadata.items() if value[1]=='output'}
        def iter_bytes(self,ref,maximum):
            raw=bodies[ref.artifact_id]
            for i in range(0,len(raw),65536):yield raw[i:i+65536]
    reader=Reader()
    class Artifacts:
        def stage(self,path,reservation,role,mime):return store(path.read_bytes(),role,'output',mime)
        def stage_contract(self,value,role):return store(canonical_bytes(value),role,'output')
    for name in ('material','mask','output','stage'):(tmp_path/name).mkdir()
    with ScratchSpace(tmp_path/'stage',uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_open_eyes_material_patch(params,raw,raw,proofraw,*facebodies,reader,
                np.ones((100,100),np.float32),np.zeros((100,100),np.float32),source_origin=(0,0),
                min_face_edge_px=40,scratch_root=tmp_path/'material',max_bytes=1000000,
                check_active=lambda:None,authority=lambda *args:True,alpha_mode='require_float32') as (patch,material):
            with write_donor_patch_files(material.source_rgb,patch,(0,0),scratch,target=np.ones((100,100),np.float32),
                    protected=np.zeros((100,100),np.float32),check_active=lambda:None) as files:
                staged=stage_donor_result(Artifacts(),params,raw,files,color_warning=patch.color.warning,check_active=lambda:None)
    if mode=='wrong_output':
        result=json.loads(bodies[staged.document.artifact_id]);outref=ArtifactRef.model_validate_json(canonical_bytes(result['working_image']))
        out=json.loads(bodies[outref.artifact_id]);pixelref=ArtifactRef.model_validate_json(canonical_bytes(out['pixels']))
        stream=BytesIO();rgb=np.ones((100,100,3),np.float32);rgb[10,10]=0.5;np.save(stream,rgb)
        newpixel=store(stream.getvalue(),'pixels','output','application/x-npy');out['pixels']=newpixel.model_dump(mode='json')
        newimage=store(canonical_bytes(out),'working_image','output');result['working_image']=newimage.model_dump(mode='json')
        maskref=ArtifactRef.model_validate_json(canonical_bytes(result['changed_mask']))
        maskdoc=json.loads(bodies[maskref.artifact_id])
        stream=BytesIO();changed=np.zeros((100,100),np.float32);changed[10,10]=1;np.save(stream,changed)
        maskdoc['artifact']=store(stream.getvalue(),'mask_pixels','output','application/x-npy').model_dump(mode='json')
        result['changed_mask']=store(canonical_bytes(maskdoc),'mask','output').model_dump(mode='json')
        stagedroot=store(canonical_bytes(result),'document','output')
    else:stagedroot=staged.document
    lock=current_runtime().model_copy(deep=True)
    from photo_worker.operations.portrait.donor_resources import donor_portrait_resources
    profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':donor_portrait_resources(100,100,100,100,100,100)}
    def run():return verify_donor_reconstruction(reader,_ref(paramref),_ref(stagedroot),manual_root_ref=rootref,
        producer_ref=producerref,resource_profile=profile,ram_max_bytes=1 if mode=='ram' else 1<<30,min_face_edge_px=40,runtime_lock=lock,runtime_sha=canonical_hash(lock),
        mask_scratch_root=tmp_path/'mask',mask_max_bytes=1000000,material_scratch_root=tmp_path/'material',
        material_max_bytes=1000000,output_scratch_root=tmp_path/'output',output_max_bytes=1 if mode=='scratch' else 1000000,
        authority=lambda *args:mode!='revoked',producer_authority=lambda *args:True,check_active=lambda:None)
    if mode in ('ok','alpha'):
        assert run()[0]==params
        from photo_worker.v2.adoption import prepare_v2_adoption
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='portrait.open_eyes',version='1',
            root=_ref(stagedroot),reader=reader,params_ref=_ref(paramref))
        assert len(prepared.inventory)>=6
    else:
        with pytest.raises(ValueError,match='DONOR_REPLAY_MISMATCH' if mode=='wrong_output' else 'DONOR_ADOPTION_SCRATCH_REQUIRED' if mode=='scratch' else 'DONOR_ADOPTION_RAM_REQUIRED' if mode=='ram' else 'DONOR_AUTHORITY'):run()
    assert all(not (tmp_path/name).exists() or not list((tmp_path/name).iterdir()) for name in ('material','mask','output'))
    assert not any(path.is_file() for path in (tmp_path/'stage').rglob('*'))
