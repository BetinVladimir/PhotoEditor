from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.layer_adoption import verify_layer_metadata


def test_missing_authoritative_parameter_evidence_stops_reads():
    ref=ArtifactRef(__import__('uuid').uuid4(),'a'*64)
    class Reader:
        def evidence(self,received):
            assert received==ref
            return SimpleNamespace(ref=ref,source='output',media_type='application/json')
        def read_json(self,*args):pytest.fail('read unscoped parameter')
    with pytest.raises(AdoptionInvalid):verify_layer_metadata(Reader(),ref,ref)


def test_evidence_verifier_accepts_staged_result_and_rejects_review_downgrade(tmp_path,monkeypatch):
    from test_layer_adapter import worker_fixture
    from photo_worker.operations.layers.adapter import execute
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    from uuid import uuid4
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,'review')
    try:
        result=execute(ctx,artifacts,admission)
        raw=canonical_bytes(params);params_ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
        class Reader:
            def body(self,ref):
                if ref==params_ref:return raw
                value=published.get(ref.artifact_id)
                if value is None:value=artifacts.contract(ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256),object)
                return canonical_bytes(value)
            def evidence(self,ref):
                body=self.body(ref)
                return SimpleNamespace(ref=ref,source='output' if ref.artifact_id in published else 'input',media_type='application/json',bytes=len(body))
            def read_json(self,ref,limit):return self.body(ref)
        # Parent fixture hashes are synthetic; isolate metadata wiring here.
        from photo_worker.v2 import layer_adoption
        monkeypatch.setattr(layer_adoption,'_document',lambda reader,ref,model,source:model.model_validate_json(reader.body(ref)))
        root=ArtifactRef(result.outputs[0].artifact_id,result.outputs[0].sha256)
        verify_layer_metadata(Reader(),params_ref,root)
        output=published[root.artifact_id]
        report=published[output.report.artifact_id]
        published[output.report.artifact_id]=report.model_copy(update={'status':'pass','reason':None})
        with pytest.raises(AdoptionInvalid):verify_layer_metadata(Reader(),params_ref,root)
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('value',[0.,1.,.5,float('nan')])
def test_output_binary_mask_stream_validation(monkeypatch,value):
    from io import BytesIO
    from uuid import uuid4
    import numpy as np
    from photo_worker.v2 import layer_adoption
    array=BytesIO();np.save(array,np.array([[value]],dtype='<f4'),allow_pickle=False)
    body=array.getvalue();pixels=ArtifactRef(uuid4(),sha256(body).hexdigest())
    mask_ref=ArtifactRef(uuid4(),'b'*64);image_ref=ArtifactRef(uuid4(),'c'*64)
    image=SimpleNamespace(canvas=SimpleNamespace(height=1,width=1),pixels=ArtifactRef(uuid4(),'d'*64),alpha=None)
    result=SimpleNamespace(working_image=image_ref,changed_mask=mask_ref,generated_mask=None)
    monkeypatch.setattr(layer_adoption,'verify_layer_metadata',lambda *a,**k:(object(),result))
    monkeypatch.setattr(layer_adoption,'_document',lambda reader,ref,*a:image if ref==image_ref else SimpleNamespace(artifact=pixels))
    class Reader:
        closed=False
        def evidence(self,ref):
            if ref==pixels:return SimpleNamespace(ref=ref,source='output',role='mask_pixels',media_type='application/x-npy',bytes=len(body))
            return SimpleNamespace(source='input')
        def iter_bytes(self,ref,limit):
            assert ref==pixels and limit==len(body)
            try:
                for start in range(0,len(body),7):yield body[start:start+7]
            finally:self.closed=True
    reader=Reader()
    if value in (0.,1.):layer_adoption.verify_layer_output_arrays(reader,mask_ref,image_ref)
    else:
        with pytest.raises(AdoptionInvalid):layer_adoption.verify_layer_output_arrays(reader,mask_ref,image_ref)
    assert reader.closed


def test_composition_proof_orchestrates_mask_pixels_and_lineage(tmp_path,monkeypatch):
    import numpy as np
    from uuid import uuid4
    from test_layer_adapter import worker_fixture
    from photo_worker.operations.layers.adapter import execute
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    from photo_worker.v2 import layer_adoption,array_evidence
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,'review')
    try:
        result=execute(ctx,artifacts,admission)
        params_ref=ArtifactRef(uuid4(),'a'*64);root=ArtifactRef(result.outputs[0].artifact_id,result.outputs[0].sha256)
        class Reader:
            def evidence(self,ref):return SimpleNamespace(source='output' if ref.artifact_id in published else 'input')
        def document(reader,ref,model,scope):
            if ref==params_ref:return params
            if ref.artifact_id in published:return published[ref.artifact_id]
            return artifacts.contract(ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256),model)
        monkeypatch.setattr(layer_adoption,'_document',document)
        class Arrays:
            def __init__(self,*args):pass
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def allocate_mask(self,shape):return np.zeros(shape,np.float32)
            def alpha(self,ref,shape,*,mode):
                assert mode=='require_float32'
                return None if ref is None else self.array(ref,shape,source='input',unit=True)
            def array(self,ref,shape,**kwargs):
                if ref.artifact_id in published:return published[ref.artifact_id]
                with artifacts.input(ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256)) as local:
                    return np.load(local.path,allow_pickle=False)
        monkeypatch.setattr(array_evidence,'ArrayEvidenceSession',Arrays)
        layer_adoption.verify_layer_composition(Reader(),params_ref,root,scratch_root=tmp_path,max_bytes=1000000)
        output=published[root.artifact_id];image=published[output.working_image.artifact_id]
        published[image.pixels.artifact_id][0,1,0]=.1
        with pytest.raises(AdoptionInvalid):
            layer_adoption.verify_layer_composition(Reader(),params_ref,root,scratch_root=tmp_path,max_bytes=1000000)
    finally:artifacts.scratch.cleanup()
