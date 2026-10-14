from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from io import BytesIO
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.optical.negative_prepare import prepare_negative_files
from photo_worker.operations.optical.negative_stage import stage_negative_result
from test_negative_result import source_case
from test_optical_negative import calibration


@pytest.mark.parametrize('orphan',[False,True])
@pytest.mark.parametrize('bad_review',[False,True])
@pytest.mark.parametrize('wrong_pixels',[False,True])
@pytest.mark.parametrize('bad_statistics',[False,True])
def test_real_negative_preparation_stages_complete_review_closure(tmp_path,orphan,bad_review,wrong_pixels,bad_statistics):
    p,source,raw=source_case();cal=canonical_bytes(calibration());p['calibration']['sha256']=sha256(cal).hexdigest()
    bodies={};roles=[];metadata={}
    class Artifacts:
        def store(self,body,role):
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            bodies[ref.artifact_id]=body;roles.append(role);metadata[ref.artifact_id]=(role,'output','application/x-npy' if role in ('pixels','mask_pixels') else 'application/json');return ref
        def stage(self,path,reservation,role,mime):
            reservation.validate_file(path);assert mime=='application/x-npy'
            body=path.read_bytes()
            if wrong_pixels and role=='pixels':
                value=np.load(BytesIO(body));value[0,0]=0.25
                stream=BytesIO();np.save(stream,value);body=stream.getvalue()
            return self.store(body,role)
        def stage_contract(self,value,role):
            if bad_review and role=='report':
                value=value.model_copy(deep=True);value.status='pass'
            if bad_statistics and role=='report':
                value=value.model_copy(deep=True);value.clipping_fraction=0.5
            return self.store(canonical_bytes(value),role)
    scan=np.ones((source.canvas.height,source.canvas.width,3),np.float32)
    def input_array(ref,value):
        stream=BytesIO();np.save(stream,value);body=stream.getvalue()
        ref.sha256=sha256(body).hexdigest();bodies[ref.artifact_id]=body
        metadata[ref.artifact_id]=(None,'input','application/x-npy')
    input_array(source.pixels,scan)
    input_array(source.alpha,np.ones(scan.shape[:2],np.float32))
    raw=canonical_bytes(source);p['base']['source']['sha256']=sha256(raw).hexdigest()

    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        with prepare_negative_files(scan,p,cal,scratch,input_icc_sha='c'*64,epsilon=1e-6,
                ram_max_bytes=1<<30,check_active=lambda:None) as files:
            result=stage_negative_result(Artifacts(),p,raw,files,clipping_fraction=0,check_active=lambda:None)
            assert result.review_required and len(result.outputs)==6 and result.document==result.outputs[-1]
            if not bad_review and not bad_statistics:assert result.bytes_written==sum(len(bodies[r.artifact_id]) for r in result.outputs)
        assert not list(scratch.directory.iterdir())
    assert roles==['pixels','working_image','mask_pixels','mask','report','document']

    from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef as EvidenceRef,prepare_v2_adoption,AdoptionInvalid
    source_id=ArtifactRef.model_validate_json(canonical_bytes(p['base']['source'])).artifact_id
    bodies[source_id]=raw;metadata[source_id]=(None,'input','application/json')

    class Reader:
        def evidence(self,ref):
            role,origin,mime=metadata[ref.artifact_id]
            return ArtifactEvidence(ref,mime,len(bodies[ref.artifact_id]),role,origin)
        def read_json(self,ref,maximum):return bodies[ref.artifact_id]
        def iter_bytes(self,ref,maximum):
            body=bodies[ref.artifact_id]
            assert len(body)<=maximum
            for i in range(0,len(body),65536):yield body[i:i+65536]
        def staged_outputs(self):
            return {EvidenceRef(r.artifact_id,r.sha256) for r in result.outputs}
    reader=Reader()
    if orphan:
        extra=EvidenceRef(uuid4(),'f'*64);original=reader.staged_outputs
        reader.staged_outputs=lambda:original()|{extra}
        with pytest.raises(AdoptionInvalid):
            prepare_v2_adoption(task_id=uuid4(),operation='optical.negative',version='1',
                root=EvidenceRef(result.document.artifact_id,result.document.sha256),reader=reader)
    else:
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='optical.negative',version='1',
            root=EvidenceRef(result.document.artifact_id,result.document.sha256),reader=reader)
        assert {str(r.artifact_id) for r in result.outputs} <= {row['artifact_id'] for row in prepared.inventory}

    from photo_worker.v2.negative_adoption import verify_negative_result_metadata
    params_raw=canonical_bytes(p);params_ref=EvidenceRef(uuid4(),sha256(params_raw).hexdigest())
    bodies[params_ref.artifact_id]=params_raw;metadata[params_ref.artifact_id]=(None,'input','application/json')
    root=EvidenceRef(result.document.artifact_id,result.document.sha256)
    if bad_review:
        with pytest.raises(AdoptionInvalid,match='NEGATIVE_METADATA_MISMATCH'):
            verify_negative_result_metadata(reader,params_ref,root,check_active=lambda:None)
    else:
        assert verify_negative_result_metadata(reader,params_ref,root,check_active=lambda:None)[1].report is not None

    if not bad_review:
        from photo_worker.v2.negative_adoption import verify_pinned_negative_replay
        from test_negative_runtime_lock import lock
        from photo_worker.contracts.base import canonical_hash
        runtime=lock()
        calibration_ref=ArtifactRef.model_validate_json(canonical_bytes(p['calibration']))
        bodies[calibration_ref.artifact_id]=cal;metadata[calibration_ref.artifact_id]=(None,'input','application/json')
        def replay():return verify_pinned_negative_replay(reader,params_ref,root,scratch_root=tmp_path,max_bytes=1<<20,
            runtime_lock=runtime,runtime_sha=canonical_hash(runtime),ram_max_bytes=1<<30,check_active=lambda:None)
        if wrong_pixels:
            with pytest.raises(AdoptionInvalid,match='NEGATIVE_REPLAY_MISMATCH'):replay()
        elif bad_statistics:
            with pytest.raises(AdoptionInvalid,match='NEGATIVE_STATISTICS_MISMATCH'):replay()
        else:replay()
        assert not any(path.is_file() for path in tmp_path.rglob('*'))
