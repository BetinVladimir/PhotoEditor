from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.quality import QualityReport,TechnicalMetrics,ReferenceMetrics
from photo_worker.runtime.restore_legacy_quality import validate_legacy_master_quality


@pytest.mark.parametrize('mode',['valid','foreign_target','review','noncanonical','nonfinite','clipping','seam','protected','activity_false','post_validation_false'])
def test_legacy_quality_recomputes_thresholds_instead_of_trusting_pass(mode):
    target=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    metrics=TechnicalMetrics(below_zero_fraction=0,above_one_fraction=0,clipped_black_fraction=.2 if mode=='clipping' else 0,
        clipped_white_fraction=0,sharpness_laplacian=1,seam_p99_delta=.1 if mode=='seam' else 0,finite=mode!='nonfinite')
    report=QualityReport(target=target if mode!='foreign_target' else ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        target_kind='working',stage='master',threshold_profile_id='master_v1',status='review' if mode=='review' else 'pass',
        technical=metrics,reference=ReferenceMetrics(mean_absolute_error=0,psnr_db=100,protected_max_delta=.1)
        if mode=='protected' else None,reasons=[])
    raw=canonical_bytes(report)
    if mode=='noncanonical':raw=b' '+raw
    checks=[]
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='post_validation_false' and len(checks)>=2 else None
    def run():return validate_legacy_master_quality(raw,target,check_active=active)
    if mode=='valid':assert run().status=='pass'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='post_validation_false':assert len(checks)==2
