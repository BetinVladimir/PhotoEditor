"""Exact output ledger scenarios; synthetic staged refs do not prove physical IO."""
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.operations.semantic.preview_retouch_output_closure import RetouchOutputClosure
from test_v27_retouch_preview_result_binding import fixture


def setup():
    params, display_ref, display_raw, proof_ref, proof_raw, image_ref, image_raw, source_raw = fixture()
    source = parse_json(source_raw);display = parse_json(display_raw);image = parse_json(image_raw)
    inputs = [params[key] for key in ('plan','intent','candidate','protection')]+[params['source']['source'],source['pixels']]
    if source['alpha'] is not None:inputs.append(source['alpha'])
    ledger = RetouchOutputClosure(inputs,max_output_bytes=1048576)
    rows = [('pixels',image['pixels'],100,None),
        ('working_image',image_ref,len(image_raw),image_raw),
        ('preview_evidence',proof_ref,len(proof_raw),proof_raw),
        *[(role,display[role],100,None) for role in ('before','after','affected','protected')],
        ('preview_presentation',display_ref,len(display_raw),display_raw)]
    return ledger, rows, params, source_raw, display_ref


@pytest.mark.parametrize('failure',[None,'missing','unknown','duplicate','alias','bytes','root','input_sha'])
def test_output_ledger_requires_exact_reachable_set(failure):
    ledger, rows, params, source_raw, root = setup()
    if failure == 'missing':rows.pop(3)
    if failure == 'input_sha':ledger._inputs[ArtifactRef.model_validate_json(canonical_bytes(params['plan'])).artifact_id] = '0'*64
    total = sum(row[2] for row in rows)
    def run():
        for role,ref,size,document in rows:ledger.record(role,ref,size,document=document)
        if failure == 'unknown':ledger.record('extra',ArtifactRef(artifact_id=uuid4(),sha256='a'*64),1)
        if failure == 'duplicate':ledger.record('pixels',rows[0][1],100)
        if failure == 'alias':
            other = RetouchOutputClosure([params['plan']],max_output_bytes=1048576)
            other.record('pixels',params['plan'],100)
        result = ledger.verify(params,source_raw,presentation_ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64) if failure == 'root' else root,
            expected_bytes=total+1 if failure == 'bytes' else total)
        assert result == total
    if failure:
        with pytest.raises(ValueError):run()
    else:run()


@pytest.mark.parametrize('failure',[None,'missing','total','upload_sha','changed_file','late_admission'])
def test_complete_boundary_records_actual_delegated_stage_calls(tmp_path,monkeypatch,failure):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.semantic_preview import RetouchPreviewNumericalEvidence,RetouchPreviewPresentation
    from photo_worker.operations.semantic import preview_retouch_complete as module
    params,display_ref,display_raw,proof_ref,proof_raw,image_ref,image_raw,source_raw = fixture()
    image,proof,display = map(parse_json,(image_raw,proof_raw,display_raw))
    binary = {role:role.encode() for role in ('pixels','before','after','affected','protected')}
    image['pixels']['sha256'] = sha256(binary['pixels']).hexdigest()
    for role in ('before','after','affected','protected'):display[role]['sha256'] = sha256(binary[role]).hexdigest()
    image_raw = canonical_bytes(image);image_ref.sha256 = sha256(image_raw).hexdigest()
    proof.update(pixels=image['pixels'],working_image=image_ref.model_dump(mode='json'))
    proof_raw = canonical_bytes(proof);proof_ref.sha256 = sha256(proof_raw).hexdigest()
    display.update(pixels=image['pixels'],working_image=image_ref.model_dump(mode='json'),numerical_evidence=proof_ref.model_dump(mode='json'))
    display_raw = canonical_bytes(display);display_ref.sha256 = sha256(display_raw).hexdigest()
    refs = {role:ArtifactRef.model_validate_json(canonical_bytes(image['pixels'] if role == 'pixels' else display[role])) for role in binary}
    docs = [('working_image',WorkingImage.model_validate_json(canonical_bytes(image)),image_ref),
        ('preview_evidence',RetouchPreviewNumericalEvidence.model_validate_json(canonical_bytes(proof)),proof_ref),
        ('preview_presentation',RetouchPreviewPresentation.model_validate_json(canonical_bytes(display)),display_ref)]
    total = sum(map(len,binary.values()))+sum(len(canonical_bytes(value)) for _,value,_ in docs)
    source = parse_json(source_raw)
    declared = [params[key] for key in ('plan','intent','candidate','protection')]+[params['source']['source'],source['pixels']]
    if source['alpha'] is not None:declared.append(source['alpha'])
    calls = [];denied = False
    class Artifacts:
        def contract_bytes(self,*a):return source_raw
        def stage(self,path,reservation,role,media_type,**kwargs):
            calls.append(role)
            if failure == 'changed_file' and role == 'pixels':path.write_bytes(b'changed')
            if failure == 'upload_sha' and role == 'pixels':return ArtifactRef(artifact_id=uuid4(),sha256='0'*64)
            return refs[role]
        def stage_contract(self,value,role):
            nonlocal denied
            calls.append(role)
            if failure=='late_admission' and role=='preview_presentation':denied=True
            return next(ref for name,_,ref in docs if name == role)
    def complete(artifacts,*a,**kwargs):
        for role,payload in binary.items():
            if failure == 'missing' and role == 'affected':continue
            path = tmp_path/(role+'.bin');path.write_bytes(payload)
            artifacts.stage(path,None,role,'application/x-npy' if role == 'pixels' else 'image/png')
        for role,value,_ in docs:artifacts.stage_contract(value,role)
        return module.CompleteRetouchPreview(display_ref,proof_ref,total+1 if failure == 'total' else total)
    monkeypatch.setattr(module,'_execute_complete_retouch_preview',complete)
    def run():return module.execute_complete_retouch_preview(Artifacts(),params,lambda:None,
        declared_inputs=declared,icc_root=None,icc_lock_sha='a'*64,srgb_sha='b'*64,
        max_ram_bytes=4000000000,max_output_bytes=1048576,verify_admission=lambda _:not denied,
        verify_origin=lambda *a:True,verify_color=lambda _:True,verify_runtime=lambda:True,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError,match='ADMISSION_DENIED' if failure=='late_admission' else 'CLOSURE_INVALID'):run()
    else:
        assert run().bytes_written == total and len(calls) == 8
