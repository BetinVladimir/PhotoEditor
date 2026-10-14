from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2 import exposure_adoption as module
from test_v27_exposure_loader import loader_fixture


@pytest.mark.parametrize('deny', [None, 'admission', 'origin'])
def test_binding_composition_requires_authorities(tmp_path, monkeypatch, deny):
    edit, protection, artifacts, _ = loader_fixture(tmp_path)
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    protection = ContractRef.model_validate_json(canonical_bytes(protection))
    source = WorkingImage.model_validate_json(artifacts.contract_bytes(edit.source.source, WorkingImage))
    output = source.model_copy(deep=True)
    output.pixels = ContractRef(artifact_id=uuid4(),sha256='f'*64)
    body = canonical_bytes(output);root = ArtifactRef(uuid4(),sha256(body).hexdigest())
    params_raw = canonical_bytes(edit);params = ArtifactRef(uuid4(),sha256(params_raw).hexdigest())
    class Reader:
        def evidence(self, ref):
            if ref == ArtifactRef(output.pixels.artifact_id,output.pixels.sha256):
                return ArtifactEvidence(ref,'application/x-npy',100,'pixels','output')
            raw = self.read_json(ref,1048576)
            return ArtifactEvidence(ref,'application/json',len(raw),
                'working_image' if ref == root else None,'output' if ref == root else 'input')
        def read_json(self, ref, limit):
            if ref == root:return body
            if ref == params:return params_raw
            return artifacts.contract_bytes(ref,None)
    replay=[]
    import photo_worker.v2.exposure_pixels as pixels
    monkeypatch.setattr(pixels,'verify_exposure_pixels',lambda *a,**k:replay.append(True))
    options=dict(protection_ref=protection,verify_admission=lambda edit:deny!='admission',
        verify_origin=lambda *args:deny!='origin',verify_color=lambda color:True,
        scratch_root=tmp_path,max_bytes=2*1024**2,check_active=lambda:None)
    if deny:
        with pytest.raises(AdoptionInvalid):module.verify_local_exposure(Reader(),params,root,**options)
        assert replay == []
    else:
        result=module.verify_local_exposure(Reader(),params,root,**options)
        assert result == output and replay == [True]


@pytest.mark.parametrize('failure', [None, 'pixel', 'stream', 'final_admission'])
def test_complete_graph_with_real_hashed_array_streams(tmp_path, failure):
    import io
    import numpy as np
    from photo_worker.contracts.base import parse_json
    from test_v27_exposure_geometry import geometry_fixture
    from photo_worker.operations.optical.color import to_working_linear
    from photo_worker.operations.semantic.local_exposure import apply_local_exposure
    from photo_worker.operations.semantic.exposure_encoding import restore_local_exposure_encoding
    edit, candidate, intent, source, mask, transform, _ = geometry_fixture()
    source=parse_json(source);mask=parse_json(mask);candidate=parse_json(candidate);intent=parse_json(intent)
    documents={};binary={};evidence={}
    def store(raw, source_kind, role, media, aid=None):
        ref=ArtifactRef(aid or uuid4(),sha256(raw).hexdigest())
        evidence[ref]=ArtifactEvidence(ref,media,len(raw),role,source_kind)
        (documents if media=='application/json' else binary)[ref]=raw
        return dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256)
    def array(values, source_kind, role=None):
        stream=io.BytesIO();np.save(stream,values,allow_pickle=False)
        return store(stream.getvalue(),source_kind,role,'application/x-npy')
    before=np.full((8,8,3),.5,np.float32);weights=np.zeros((8,8),np.float32);weights[:,4:]=.5
    protected=np.zeros((8,8),np.float32)
    source['pixels']=array(before,'input')
    mask['artifact']=array(weights,'input')
    protection=dict(mask);protection['protected']=True;protection['artifact']=array(protected,'input')
    protection_ref=store(canonical_bytes(protection),'input',None,'application/json')
    edit['source']['source']=store(canonical_bytes(source),'input',None,'application/json')
    edit['mask']=store(canonical_bytes(mask),'input',None,'application/json')
    edit['transform']=store(transform,'input',None,'application/json')
    candidate.update(target=edit['source'],mask=edit['mask'],transform=edit['transform'])
    intent.update(reference=edit['source'],reference_mask=edit['mask'])
    edit['candidate']=store(canonical_bytes(candidate),'input',None,'application/json')
    edit['intent']=store(canonical_bytes(intent),'input',None,'application/json')
    params=store(canonical_bytes(edit),'input',None,'application/json')
    color=WorkingImage.model_validate_json(canonical_bytes(source)).color
    linear=to_working_linear(before,color)
    changed=apply_local_exposure(linear,weights,protected,ev=edit['ev'],strength=edit['strength'],max_bytes=128*1024**2)
    result=restore_local_exposure_encoding(before,changed,weights,protected,color,max_bytes=128*1024**2,check_active=lambda:None)
    if failure=='pixel':result[0,0,0]=.51
    output=dict(source);output['pixels']=array(result,'output','pixels')
    root=store(canonical_bytes(output),'output','working_image','application/json')
    def ref(value):
        from uuid import UUID
        return ArtifactRef(UUID(value['artifact_id']),value['sha256'])
    class Reader:
        def evidence(self,value):return evidence[value]
        def read_json(self,value,limit):return documents[value]
        def iter_bytes(self,value,size):
            raw=binary[value]
            if failure=='stream':raw=raw[:-1]
            for pos in range(0,len(raw),17):yield raw[pos:pos+17]
    calls=[]
    def admission(edit):
        calls.append(True)
        return not (failure=='final_admission' and len(calls)==3)
    options=dict(protection_ref=protection_ref,verify_admission=admission,
        verify_origin=lambda *args:True,verify_color=lambda color:True,
        scratch_root=tmp_path,max_bytes=2*1024**2,check_active=lambda:None)
    if failure:
        with pytest.raises(AdoptionInvalid):module.verify_local_exposure(Reader(),ref(params),ref(root),**options)
    else:
        actual=module.verify_local_exposure(Reader(),ref(params),ref(root),**options)
        assert actual.pixels.sha256==output['pixels']['sha256'] and len(calls)==3
        from photo_worker.v2.adoption import prepare_v2_adoption
        report=dict(schema_version=1,status='pass',operation='semantic.local_exposure',skipped_count=0,
            reason=None,outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,
            sources=[edit['source']],registrations=[])
        report_ref=store(canonical_bytes(report),'output','report','application/json')
        result=dict(schema_version=1,working_image=root,report=report_ref,changed_mask=edit['mask'],generated_mask=None)
        result_ref=store(canonical_bytes(result),'output','document','application/json')
        # The WorkingImage root previously had the correct working_image role.
        image_evidence=evidence[ref(root)]
        evidence[ref(root)]=ArtifactEvidence(ref(root),'application/json',image_evidence.bytes,'working_image','output')
        Reader.staged_outputs=lambda self:{value for value,metadata in evidence.items() if metadata.source=='output'}
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='semantic.local_exposure',version='1',
            root=ref(result_ref),reader=Reader(),params_ref=ref(params),
            layer_scratch_root=tmp_path,layer_scratch_bytes=2*1024**2,
            exposure_protection=protection_ref,exposure_admission=admission,
            exposure_origin=lambda *args:True,exposure_color=lambda color:True,check_active=lambda:None)
        expected_ids={value['artifact_id'] for value in (result_ref,root,output['pixels'],report_ref,edit['source']['source'],edit['mask'])}
        assert {item['artifact_id'] for item in prepared.inventory}==expected_ids
        assert len(prepared.digest)==64
    assert not list(tmp_path.glob('adoption-*'))
