import pytest
from uuid import uuid4
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef, prepare_v2_adoption
from photo_worker.v2.shown_background_cleanup_adoption import verify_shown_background_cleanup_output


class NoIO:
    def evidence(self, *args):
        raise AssertionError('unauthorized evidence IO')


def test_background_cleanup_adopter_requires_own_live_context():
    with pytest.raises(AdoptionInvalid, match='CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='semantic.adopt_shown_background_cleanup',version='1',
            root=ArtifactRef(uuid4(),'a'*64),reader=NoIO(),schemas={'$defs':{}},
            operations={'operations':[{'id':'semantic.adopt_shown_background_cleanup','version':'1','output':'WorkingImage'}]})


def test_background_cleanup_output_verifier_stops_on_inactive_context():
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_shown_background_cleanup_output(NoIO(),None,None,verify_admission=lambda raw:True,check_active=lambda:False)


@pytest.mark.parametrize('failure',[None,'revoke_between_pixels','changed_color','changed_pixels_sha','wrong_pixel_role','wrong_pixel_size','late_admission',
    'params_source','root_source','root_role','original_media','copied_media',
    'json_size_float','json_size_large','json_size_small','raw_changed','revoke_json',
    'lost_shown_evidence','lost_shown_read','lost_output_evidence','lost_output_read','lost_original_evidence','lost_copied_evidence',
    'generic','generic_corrupt','generic_late_admission','generic_extra_output'])
def test_background_cleanup_output_verifier_exact_copy_evidence(tmp_path,failure):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes,parse_json
    from photo_worker.v2.adoption import ArtifactEvidence
    from test_v27_background_cleanup_preview_adopt_execute import copy_documents_fixture
    params,documents,path=copy_documents_fixture(tmp_path)
    shown_raw=documents[str(params.working_image.artifact_id)]
    output=parse_json(shown_raw)
    output['pixels']=dict(artifact_id=str(uuid4()),sha256='0'*64 if failure=='changed_pixels_sha' else params.pixels.sha256)
    if failure=='changed_color':output['color']['icc_sha256']='0'*64
    raw=canonical_bytes(output)
    params_raw=canonical_bytes(params)
    params_ref=ArtifactRef(uuid4(),sha256(params_raw).hexdigest())
    shown_ref=ArtifactRef(params.working_image.artifact_id,params.working_image.sha256)
    root_ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    original_ref=ArtifactRef(params.pixels.artifact_id,params.pixels.sha256)
    from uuid import UUID
    copied_ref=ArtifactRef(UUID(output['pixels']['artifact_id']),output['pixels']['sha256'])
    bodies={params_ref:params_raw,shown_ref:shown_raw,root_ref:raw}
    evidence={ref:ArtifactEvidence(ref,'application/json',len(body),'working_image' if ref==root_ref else None,
        'output' if ref==root_ref else 'input') for ref,body in bodies.items()}
    evidence[original_ref]=ArtifactEvidence(original_ref,'application/x-npy',path.stat().st_size,None,'input')
    evidence[copied_ref]=ArtifactEvidence(copied_ref,'application/x-npy',path.stat().st_size+(1 if failure=='wrong_pixel_size' else 0),
        'mask' if failure=='wrong_pixel_role' else 'pixels','output')
    from dataclasses import replace
    if failure=='params_source':evidence[params_ref]=replace(evidence[params_ref],source='output')
    if failure=='root_source':evidence[root_ref]=replace(evidence[root_ref],source='input')
    if failure=='root_role':evidence[root_ref]=replace(evidence[root_ref],role='document')
    if failure=='original_media':evidence[original_ref]=replace(evidence[original_ref],media_type='application/octet-stream')
    if failure=='copied_media':evidence[copied_ref]=replace(evidence[copied_ref],media_type='application/octet-stream')
    if failure=='json_size_float':evidence[params_ref]=replace(evidence[params_ref],bytes=float(len(params_raw)))
    if failure=='json_size_large':evidence[params_ref]=replace(evidence[params_ref],bytes=1048577)
    if failure=='json_size_small':evidence[params_ref]=replace(evidence[params_ref],bytes=1)
    if failure=='raw_changed':bodies[params_ref]=params_raw+b' '
    active=True;authorized=True;seen=[];admissions=0;json_reads=[];streams=[];closed_streams=[]
    class Reader:
        def evidence(self,ref):
            nonlocal active, authorized
            assert active,'read after revoked authority'
            seen.append(ref)
            lost=dict(lost_shown_evidence=shown_ref,lost_output_evidence=root_ref,
                lost_original_evidence=original_ref,lost_copied_evidence=copied_ref)
            if lost.get(failure)==ref:authorized=False
            result=evidence[ref]
            if ref==original_ref and failure=='revoke_between_pixels':active=False
            return result
        def read_json(self,ref,max_bytes):
            nonlocal active, authorized
            assert active and max_bytes in (1048576,8388608)
            json_reads.append(ref)
            if dict(lost_shown_read=shown_ref,lost_output_read=root_ref).get(failure)==ref:authorized=False
            if failure=='revoke_json' and ref==params_ref:active=False
            return bodies[ref]
        def iter_bytes(self,ref,max_bytes):
            assert active and ref in (original_ref,copied_ref) and max_bytes==path.stat().st_size
            streams.append(ref)
            try:
                data=path.read_bytes()
                if failure=='generic_corrupt' and ref==copied_ref:
                    data=data[:-1]+bytes([data[-1]^1])
                for start in range(0,len(data),65536):yield data[start:start+65536]
            finally:closed_streams.append(ref)
        def staged_outputs(self):
            refs={root_ref,copied_ref}
            if failure=='generic_extra_output':refs.add(ArtifactRef(uuid4(),'a'*64))
            return refs
    def admitted(raw):
        nonlocal admissions
        assert raw==params_raw
        admissions+=1
        if not authorized:return False
        if failure=='generic_late_admission':return not streams
        return failure!='late_admission' or admissions==1
    def run():
        if isinstance(failure,str) and failure.startswith('generic'):
            return prepare_v2_adoption(task_id=uuid4(),operation='semantic.adopt_shown_background_cleanup',
                version='1',root=root_ref,reader=Reader(),params_ref=params_ref,
                shown_background_cleanup_admission=admitted,layer_scratch_root=tmp_path,
                layer_scratch_bytes=2*path.stat().st_size,check_active=lambda:active)
        return verify_shown_background_cleanup_output(Reader(),params_ref,root_ref,
            verify_admission=admitted,check_active=lambda:active)
    if failure=='generic':
        prepared=run()
        assert streams==closed_streams==[original_ref,copied_ref] and admissions>4
        assert {entry['artifact_id'] for entry in prepared.inventory}=={str(root_ref.artifact_id),str(copied_ref.artifact_id)}
        assert prepared.digest==sha256(canonical_bytes(list(prepared.inventory))).hexdigest()
    elif failure is None:
        verified,shown,result=run()
        assert verified==params and result.pixels.sha256==shown.pixels.sha256 and admissions>2
    else:
        with pytest.raises(AdoptionInvalid):run()
        if failure=='revoke_between_pixels':assert copied_ref not in seen
        stops=dict(lost_shown_evidence=([params_ref,shown_ref],[params_ref]),
            lost_shown_read=([params_ref,shown_ref],[params_ref,shown_ref]),
            lost_output_evidence=([params_ref,shown_ref,root_ref],[params_ref,shown_ref]),
            lost_output_read=([params_ref,shown_ref,root_ref],[params_ref,shown_ref,root_ref]),
            lost_original_evidence=([params_ref,shown_ref,root_ref,original_ref],[params_ref,shown_ref,root_ref]),
            lost_copied_evidence=([params_ref,shown_ref,root_ref,original_ref,copied_ref],[params_ref,shown_ref,root_ref]))
        if failure in stops:
            assert (seen,json_reads)==stops[failure]
            assert streams==[]
        if failure in ('json_size_float','json_size_large','json_size_small','params_source'):
            assert json_reads==[] and admissions==0
        if failure=='revoke_json':
            assert json_reads==[params_ref] and seen==[params_ref] and admissions==0
        if isinstance(failure,str) and failure.startswith('generic'):
            assert streams==closed_streams==[original_ref,copied_ref]
            if failure=='generic_late_admission':assert admissions>2
    assert not list(tmp_path.glob('adoption-*'))
