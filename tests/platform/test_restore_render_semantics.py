from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,Canvas,ColorSpace,TransformChain
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.runtime.restore_render_semantics import validate_restore_render_documents


@pytest.mark.parametrize('mode',['valid','review','failed','foreign_operation','foreign_source','wrong_canvas','noncanonical','wrong_selector','protected','extra_source','activity_false','late_activity_false'])
def test_render_semantics_uses_real_codecs_and_exact_source_binding(mode):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image=WorkingImage(pixels=ref(),alpha=None,dtype='float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash=canonical_hash(image.canvas))
    mask=MaskRef(artifact=ref(),canvas=image.canvas,transform_chain=image.transform_chain,
        selector='changed',confidence=1,model_lock_id=None,protected=False)
    report=ImageReport(schema_version=1,status='review' if mode=='review' else 'fail' if mode=='failed' else 'pass',
        operation='other' if mode=='foreign_operation' else 'render.compose',skipped_count=0,reason=None,
        outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[binding],registrations=[])
    if mode=='foreign_source':report.sources=[binding.model_copy(update={'canvas_hash':'d'*64})]
    if mode=='wrong_canvas':mask=mask.model_copy(update={'canvas':image.canvas.model_copy(update={'width':3})})
    if mode=='wrong_selector':mask=mask.model_copy(update={'selector':'manual'})
    if mode=='protected':mask=mask.model_copy(update={'protected':True})
    if mode=='extra_source':report.sources=[binding,binding]
    raw=canonical_bytes(image)
    if mode=='noncanonical':raw=b' '+raw
    checks=[]
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='late_activity_false' and len(checks)>=7 else None
    def run():return validate_restore_render_documents(raw,canonical_bytes(report),canonical_bytes(mask),None,
        binding=binding,check_active=active)
    if mode in ('valid','review'):assert run().status==('review' if mode=='review' else 'pass')
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='late_activity_false':assert len(checks)==7
