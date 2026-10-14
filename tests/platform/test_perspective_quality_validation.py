from dataclasses import replace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.perspective_quality_validation import verify_perspective_quality_result

class Reader:
    def __init__(self, status='pass', reasons=None, black=0):
        self.target=ArtifactRef(uuid4(),'a'*64)
        target={'artifact_id':str(self.target.artifact_id),'sha256':self.target.sha256}
        self.params=dict(target=target,target_kind='working',stage='master',threshold_profile_id='master_v1',baseline=None,protected=[],seams=[])
        self.report=dict(target=target,target_kind='working',stage='master',threshold_profile_id='master_v1',status=status,reference=None,reasons=reasons or [],technical=dict(below_zero_fraction=0,above_one_fraction=0,clipped_black_fraction=black,clipped_white_fraction=0,sharpness_laplacian=.01,seam_p99_delta=0,finite=True))
        self.refresh()
    def refresh(self):
        self.docs={};self.meta={}
        for name,value,source,role in [('params',self.params,'input','task_params'),('report',self.report,'output','quality_report')]:
            raw=canonical_bytes(value);ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
            setattr(self,name+'_ref',ref);self.docs[ref]=raw
            self.meta[ref]=ArtifactEvidence(ref,'application/json',len(raw),role,source)
    def evidence(self,ref):return self.meta[ref]
    def read_json(self,ref,limit):
        assert len(self.docs[ref])<=limit
        return self.docs[ref]
    def staged_outputs(self):return {self.report_ref}
    def verify(self,**kwargs):return verify_perspective_quality_result(self,self.params_ref,self.report_ref,expected_target=self.target,**kwargs)

@pytest.mark.parametrize('status,reasons,black',[('pass',[],0),('review',['excessive_black_clipping'],.03),('fail',['excessive_black_clipping'],.11)])
def test_quality_preserves_decision(status,reasons,black):
    assert Reader(status,reasons,black).verify().status==status

@pytest.mark.parametrize('mutation',[
 lambda r:r.report.update(status='pass',reasons=[]),
 lambda r:r.report.update(target={'artifact_id':str(uuid4()),'sha256':'a'*64}),
 lambda r:r.params.update(baseline=r.params['target']),
 lambda r:r.report['technical'].update(seam_p99_delta=.01),
 lambda r:r.report.update(reference={'mean_absolute_error':0,'psnr_db':100,'protected_max_delta':0}),
])
def test_quality_rejects_substitution(mutation):
    r=Reader('review',['excessive_black_clipping'],.03);mutation(r);r.refresh()
    with pytest.raises(AdoptionInvalid):r.verify()

def test_quality_rejects_bytes_and_authority():
    r=Reader();r.docs[r.report_ref]=r.docs[r.report_ref][:-1]+b' '
    with pytest.raises(AdoptionInvalid):r.verify()
    r=Reader();r.meta[r.report_ref]=replace(r.meta[r.report_ref],source='input')
    with pytest.raises(AdoptionInvalid):r.verify()

def test_quality_cancellation():
    class Stop(Exception):pass
    def stop():raise Stop()
    with pytest.raises(Stop):Reader().verify(check_active=stop)
