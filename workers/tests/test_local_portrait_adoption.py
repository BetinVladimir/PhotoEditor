"""Authored document-gate boundary scenarios; not photo-quality qualification."""
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.local_portrait_adoption import verify_local_portrait_documents


class NoReader:
    def evidence(self, _):
        raise AssertionError('reader must not be used before context validation/cancellation')


@pytest.mark.parametrize('operation,params', [('unknown', object()), ('portrait.hair', None), ('portrait.clothing', None)])
def test_local_portrait_requires_exact_operation_and_params_before_io(operation, params):
    with pytest.raises(AdoptionInvalid, match='LOCAL_PORTRAIT_CONTEXT_REQUIRED'):
        verify_local_portrait_documents(NoReader(), operation, params, object(), check_active=lambda: None)


def test_local_portrait_cancellation_precedes_evidence_io():
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        verify_local_portrait_documents(NoReader(), 'portrait.hair', object(), object(), check_active=cancelled)

def test_local_portrait_false_authority_precedes_evidence_io():
    with pytest.raises(AdoptionInvalid, match='LOCAL_PORTRAIT_INACTIVE'):
        verify_local_portrait_documents(NoReader(), 'portrait.hair', object(), object(), check_active=lambda: False)


def document_fixture(operation, change_image=None):
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import canonical_bytes, canonical_hash
    from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence
    documents, evidence = {}, {}
    def external():
        value={'artifact_id':str(uuid4()), 'sha256':'a'*64}
        ref=ArtifactRef.parse(value)
        evidence[ref]=ArtifactEvidence(ref,'application/x-npy',48,None,'input')
        return value
    def publish(body, source, role):
        raw=canonical_bytes(body);ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
        documents[ref]=raw;evidence[ref]=ArtifactEvidence(ref,'application/json',len(raw),role,source)
        return {'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}
    canvas=dict(width=2,height=2,orientation=1,space_id='fixture',source_sha256='b'*64)
    chain=dict(steps=[],hash=canonical_hash({'steps':[]}))
    image=dict(pixels=external(),canvas=canvas,color=dict(profile_id='linear_srgb_v1',icc_sha256='c'*64,encoding='linear'),dtype='float32',alpha=external(),transform_chain=chain)
    source=publish(image,'input',None)
    def mask(selector, protected, source, role):
        binary=external()
        if source=='output':
            ref=ArtifactRef.parse(binary)
            evidence[ref]=ArtifactEvidence(ref,'application/x-npy',16,'mask_pixels','output')
        return publish(dict(artifact=binary,canvas=canvas,transform_chain=chain,object_id=None,selector=selector,confidence=1,model_lock_id=None,protected=protected),source,role)
    base=dict(asset_version_id=str(uuid4()),source=source,canvas_hash=canonical_hash(canvas))
    params=dict(schema_version=1,base=base,strength=.5,mask=mask('target',False,'input',None),protected=mask('protected',True,'input',None),hue_delta_degrees=0,saturation_factor=1)
    params['detail' if operation=='portrait.hair' else 'wrinkle_strength']=.3
    params_ref=publish(params,'input',None)
    pixels=external();pixel_ref=ArtifactRef.parse(pixels)
    evidence[pixel_ref]=ArtifactEvidence(pixel_ref,'application/x-npy',48,'pixels','output')
    candidate=dict(image,pixels=pixels)
    if change_image is not None:
        change_image(candidate)
    output=publish(candidate,'output','working_image')
    review=operation=='portrait.clothing'
    report=publish(dict(schema_version=1,status='review' if review else 'pass',operation=operation,skipped_count=0,reason='wrinkle_correction' if review else None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[base],registrations=[]),'output','report')
    root=publish(dict(schema_version=1,working_image=output,report=report,changed_mask=mask('changed',False,'output','mask'),generated_mask=None),'output','document')
    class Reader:
        def evidence(self, ref): return evidence[ref]
        def read_json(self, ref, maximum): return documents[ref]
    return Reader(),ArtifactRef.parse(params_ref),ArtifactRef.parse(root)


@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_local_portrait_exact_document_closure(operation):
    reader,params,root=document_fixture(operation)
    result=verify_local_portrait_documents(reader,operation,params,root,check_active=lambda: True)
    assert result[0].base.source.sha256
    assert result[1].alpha == result[3].alpha
    assert result[4].operation == operation

@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_local_portrait_rejects_versioned_document_byte_corruption(operation):
    reader,params,root=document_fixture(operation)
    original=reader.read_json
    reader.read_json=lambda ref, maximum: original(ref,maximum)+b' ' if ref==params else original(ref,maximum)
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_BYTES_INVALID'):
        verify_local_portrait_documents(reader,operation,params,root,check_active=lambda: True)


@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_local_portrait_rejects_input_artifact_disguised_as_output(operation):
    from dataclasses import replace
    reader,params,root=document_fixture(operation)
    original=reader.evidence
    reader.evidence=lambda ref: replace(original(ref),source='input') if ref==root else original(ref)
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_EVIDENCE_INVALID'):
        verify_local_portrait_documents(reader,operation,params,root,check_active=lambda: True)

@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_local_portrait_rejects_undeclared_binary_input(operation):
    from dataclasses import replace
    reader,params,root=document_fixture(operation)
    original=reader.evidence
    def evidence(ref):
        value=original(ref)
        return replace(value,source='output') if value.media_type=='application/x-npy' and value.source=='input' else value
    reader.evidence=evidence
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_INPUT_UNDECLARED'):
        verify_local_portrait_documents(reader,operation,params,root,check_active=lambda: True)

@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
@pytest.mark.parametrize('field', ['alpha','color','dtype'])
def test_local_portrait_rejects_rehashed_metadata_changes(operation,field):
    def change(image):
        if field=='alpha':
            image['alpha']=None
        elif field=='color':
            image['color']=dict(image['color'],icc_sha256='d'*64)
        else:
            image['dtype']='uint16'
    # Publication computes new correct hashes for the altered document and root.
    # The gate must enforce semantics even when storage integrity is valid.
    reader,params,root=document_fixture(operation,change)
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_IMAGE_CHANGED'):
        verify_local_portrait_documents(reader,operation,params,root,check_active=lambda: True)


@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_general_adoption_requires_budget_before_local_portrait_io(operation):
    from uuid import uuid4
    from photo_worker.v2.adoption import prepare_v2_adoption
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_SCRATCH_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation=operation,version='1',
            root=object(),reader=NoReader(),params_ref=object(),
            schemas={},operations={'operations':[{'id':operation,'version':'1'}]})


@pytest.mark.parametrize('operation', ['portrait.hair','portrait.clothing'])
def test_general_adoption_runs_physical_gate_before_publication(operation,monkeypatch,tmp_path):
    from uuid import uuid4
    from photo_worker.v2.adoption import prepare_v2_adoption
    from photo_worker.v2 import local_portrait_pixels
    reader,params,root=document_fixture(operation)
    reached=[]
    def reject(reader_arg,params_arg,source_arg,root_arg,image_arg,**options):
        reached.append((params_arg,source_arg,root_arg,image_arg,options))
        raise AdoptionInvalid('PHYSICAL_REJECTION_WITNESS')
    monkeypatch.setattr(local_portrait_pixels,'verify_local_portrait_pixels',reject)
    with pytest.raises(AdoptionInvalid,match='PHYSICAL_REJECTION_WITNESS'):
        prepare_v2_adoption(task_id=uuid4(),operation=operation,version='1',
            root=root,reader=reader,params_ref=params,layer_scratch_root=tmp_path,
            layer_scratch_bytes=1024*1024,schemas={},
            operations={'operations':[{'id':operation,'version':'1'}]})
    assert len(reached)==1
    assert reached[0][0].base.source.sha256
    assert reached[0][4]['scratch_root']==tmp_path
    assert reached[0][4]['max_bytes']==1024*1024
