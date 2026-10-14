"""Real CPU adapter/pixel/report execution with controlled SQL/store authorities."""
from dataclasses import asdict
from hashlib import sha256
from io import BytesIO
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes, ArtifactRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
from photo_worker.operations.semantic.exposure_installation import sql_local_exposure_factory
from photo_worker.operations.semantic.exposure_adapter import execute_exposure
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.adoption import AdoptionInvalid
from test_exposure_qualification_report import exposure_fixture
from test_v27_exposure_pipeline import inputs


@pytest.mark.parametrize('revoked', [None, 'before', 'publish', 'root', 'working_image', 'report', 'document'])
def test_installed_cpu_adapter_computes_and_publishes_with_report_proof(tmp_path,monkeypatch,revoked):
    import test_v27_exposure_geometry as geometry
    original=geometry.source_fixture
    def source():
        edit,image,_=original();image['color']['icc_sha256']=WORKING_PROFILE_SHA
        body=canonical_bytes(image);edit['source']['source']['sha256']=sha256(body).hexdigest()
        return edit,image,body
    monkeypatch.setattr(geometry,'source_fixture',source)
    value,root,artifacts,events,_,_=inputs(tmp_path)
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(value));raw=canonical_bytes(edit)
    policy,_,report,pin=exposure_fixture()
    frozen=json.loads(json.dumps(asdict(pin),default=str))
    profile=dict(backend='cpu',renderer_version='v2.2',qualification=frozen,
        resource_estimate=dict(ram_bytes=128*1024**2,scratch_bytes=2*1024**2,
            vram_bytes=0,output_bytes=2*1024**2))
    command=SimpleNamespace(operation='semantic.local_exposure',operation_version='1',
        resource_class='cpu',params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile),
        check_active=lambda:None,progress=lambda *args:None)
    reads=[];streams=[];staged=[]
    def evidence(lease,body):
        assert lease==ctx.lease and body==raw
        reads.append(1)
        if ((revoked=='before' and len(reads)>2) or (revoked=='publish' and staged)
                or (revoked=='root' and 'document' in staged)):return None
        return dict(pin=frozen,policy=policy,report=dict(artifact_id=str(pin.report.artifact_id),
            sha256=pin.report.sha256,bucket='owned',object_key='report',object_version='v1',
            bytes=len(report),media_type='application/json'))
    def get(bucket,key,*,version_id):
        assert (bucket,key,version_id)==('owned','report','v1')
        stream=BytesIO(report);streams.append(stream);return stream
    db=SimpleNamespace(exposure_report_evidence=evidence,
        exposure_protection_pin=lambda lease,body:root,
        admit_semantic_local_exposure=lambda lease,body:body==raw,
        local_exposure_protection_origin=lambda *args:True)
    reader=artifacts.contract_bytes
    artifacts.contract_bytes=lambda ref,model:raw if ref==command.params else reader(ref,model)
    artifacts.params=lambda model:edit
    def stage(path,reservation,role,media):
        pixels=np.load(path,allow_pickle=False)
        np.testing.assert_array_equal(pixels[:,:4],np.full((8,4,3),.5,np.float32))
        np.testing.assert_allclose(pixels[:,4:],.5*2**(.25*.5*.5/2.4),rtol=1e-6)
        staged.append(role)
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    def stage_contract(value,role):
        if revoked==role:
            raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True)
        if role=='report':assert value.status=='pass' and value.outside_mask_max_error==0
        staged.append(role)
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(value)).hexdigest())
    artifacts.stage=stage;artifacts.stage_contract=stage_contract
    factory=sql_local_exposure_factory(database=db,objects=SimpleNamespace(get_object=get),
        bucket='owned',icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'),
        **{key:getattr(pin,key) for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;installation=factory(ctx,artifacts)
        if revoked:
            expected_error=OperationFailure if revoked in ('working_image','report','document') else AdoptionInvalid
            with pytest.raises(expected_error) as failure:
                execute_exposure(ctx,artifacts,installation)
            if expected_error is OperationFailure:
                assert failure.value.error.code=='DEPENDENCY_UNAVAILABLE' and failure.value.error.retryable
            if revoked=='before':
                assert not staged and not events
            else:
                expected_roles={
                    'publish':['pixels'], 'root':['pixels','working_image','report','document'],
                    'working_image':['pixels'], 'report':['pixels','working_image'],
                    'document':['pixels','working_image','report']}
                assert staged==expected_roles[revoked]
                assert events.count('open')==events.count('close')==8
        else:
            result=execute_exposure(ctx,artifacts,installation)
            assert len(result.outputs)==1 and not result.review_required and result.peak_device_bytes==0
            assert staged==['pixels','working_image','report','document']
            assert len(reads)>4 and events.count('open')==events.count('close')==8
    assert all(stream.closed for stream in streams)
