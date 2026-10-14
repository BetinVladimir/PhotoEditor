from copy import deepcopy
from hashlib import sha256
from io import BytesIO
import json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.negative_qualification import NegativeQualificationPin
from photo_worker.v2.negative_leased_prepare import prepare_leased_negative_files
from test_negative_qualification import fixture
from test_negative_result import source_case
from test_negative_runtime_lock import lock


@pytest.mark.parametrize('mode',['ok','revoked','budget','ram','profile','stage','stage_revoked'])
def test_leased_negative_real_source_and_preparation_share_budget(tmp_path,mode):
    p,c,r,pin,policy=fixture();_,image,_=source_case();bodies={};reads=[]
    scan=np.broadcast_to(np.asarray(p['film_base_rgb'],np.float32),(image.canvas.height,image.canvas.width,3)).copy()
    for ref,value in [(image.pixels,scan),(image.alpha,np.ones(scan.shape[:2],np.float32))]:
        stream=BytesIO();np.save(stream,value);body=stream.getvalue()
        ref.sha256=sha256(body).hexdigest();bodies[ref.artifact_id]=body
    source=canonical_bytes(image);p['base']['source']['sha256']=sha256(source).hexdigest()
    p['base']['canvas_hash']=canonical_hash(image.canvas)
    runtime=lock();digest=canonical_hash(runtime)
    report=json.loads(r);report['runtime_lock_sha']=digest;r=canonical_bytes(report)
    pin['report']['sha256']=sha256(r).hexdigest()
    calibration=json.loads(c);calibration['qualification']=deepcopy(pin['report']);c=canonical_bytes(calibration)
    pin['calibration']['sha256']=sha256(c).hexdigest();p['calibration']=deepcopy(pin['calibration'])
    pin['runtime_lock_sha']=digest;policy['runtime_lock_sha']=digest
    policy['calibration_shas']=[pin['calibration']['sha256']];pin['policy_sha']=canonical_hash(policy)
    raw=canonical_bytes(p);ready=[True]
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(raw).hexdigest())),
        execution_profile={'negative_qualification':deepcopy(pin),'negative_runtime_sha':digest})
    from photo_worker.operations.optical.negative_resources import negative_resources
    estimate=negative_resources(image.canvas.width,image.canvas.height,has_alpha=True)
    estimate['material_scratch_bytes']=200000
    estimate['scratch_bytes']=200000+estimate['output_scratch_bytes']
    if mode=='profile':estimate['ram_bytes']=1
    lease.execution_profile.update(renderer_version='v2.2',backend='cpu',resource_estimate=estimate)
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',len(bodies[ref.artifact_id]),None,'input')
        def iter_bytes(self,ref,maximum):
            reads.append(ref.artifact_id);body=bodies[ref.artifact_id]
            for i in range(0,len(body),65536):yield body[i:i+65536]
    database=SimpleNamespace(negative_lease_ready=lambda *args:ready[0],negative_source_ready=lambda *args:ready[0])
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        def run():
            if mode in ('stage','stage_revoked'):
                from photo_worker.contracts.base import ArtifactRef
                from photo_worker.v2.negative_leased_stage import prepare_and_stage_leased_negative
                outputs=[]
                class Publisher:
                    def store(self,body):
                        outputs.append(body)
                        if mode=='stage_revoked' and len(outputs)==2:ready[0]=False
                        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
                    def stage(self,path,reservation,role,mime):
                        reservation.validate_file(path);return self.store(path.read_bytes())
                    def stage_contract(self,value,role):return self.store(canonical_bytes(value))
                result=prepare_and_stage_leased_negative(Reader(),Publisher(),database,lease,raw,source,c,r,
                    policy,scratch,runtime_lock=runtime,runtime_sha=digest,source_max_bytes=200000,
                    ram_max_bytes=1<<30,qualification_authority=lambda:NegativeQualificationPin.from_value(pin),
                    verify_color=lambda value:True,clipping_fraction=0,check_active=lambda:None)
                assert len(result.outputs)==6 and result.review_required
                assert result.bytes_written==sum(map(len,outputs))
                return
            with prepare_leased_negative_files(Reader(),database,lease,raw,source,c,r,policy,scratch,
                    runtime_lock=runtime,runtime_sha=digest,source_max_bytes=(1<<20) if mode=='budget' else 200000,
                    ram_max_bytes=1 if mode=='ram' else 1<<30,qualification_authority=lambda:NegativeQualificationPin.from_value(pin),
                    verify_color=lambda value:True,check_active=lambda:None) as files:
                np.testing.assert_allclose(np.load(files.rgb),0,atol=1e-7)
                np.testing.assert_array_equal(np.load(files.changed_mask),1)
                if mode=='revoked':ready[0]=False
        if mode in ('ok','stage'):run()
        else:
            with pytest.raises(OperationFailure if mode=='profile' else ValueError,match='RAM_ADMISSION_EXCEEDED' if mode=='profile' else 'NEGATIVE_RAM_EXCEEDED' if mode=='ram' else 'NEGATIVE_SCRATCH_REQUIRED' if mode=='budget' else 'NEGATIVE_ATTEMPT_STALE'):run()
        assert not list(scratch.directory.iterdir())
    if mode in ('budget','ram','profile'):assert not reads
    else:assert reads==[image.pixels.artifact_id,image.alpha.artifact_id]
