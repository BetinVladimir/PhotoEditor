from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_batch_physical import RestoreBatchPhysicalEvidence
from photo_worker.runtime.restore_render_review_pending import RestorePendingRenderReview
from photo_worker.runtime import restore_render_review_previews as m


@pytest.mark.parametrize('mode',['valid','fallback','missing','changed_catalogue',
                                 'changed_pending','foreign_source','deadline','fixture_false','activity_false'])
def test_review_previews_pin_both_sides_before_decision(monkeypatch,mode):
    tenant,batch,child,workflow,review=(uuid4() for _ in range(5))
    source_image=WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        canvas=Canvas(width=100,height=80,orientation=1,space_id='source',source_sha256='a'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
    source_raw=canonical_bytes(source_image)
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source_raw).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=source_ref,
        canvas_hash=canonical_hash(source_image.canvas))
    after_image=source_image.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256='b'*64)})
    after_ref=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    physical=RestoreBatchPhysicalEvidence('restore-batch-physical/v1',NS(),
        NS(working_image=after_ref),after_image,(),None,None,(),None)
    pending=RestorePendingRenderReview('isolated',tenant,child,review,
        uuid5(workflow,'render.compose:0'),1,after_ref)
    events=[];clock=[0.0];pending_reads=[0];catalogue_reads={'before':0,'after':0};cached={}
    monkeypatch.setattr(m,'monotonic',lambda:clock[0])
    def find(*args,**kwargs):
        pending_reads[0]+=1;events.append('pending')
        if mode=='changed_pending' and pending_reads[0]>1:
            return pending.__class__(pending.database,pending.tenant_id,pending.child_run_id,
                pending.review_id,pending.task_id,2,pending.candidate)
        return pending
    def objects(side,ref):
        catalogue_reads[side]+=1;events.append(side+' catalogue')
        if mode=='missing':raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_NOT_READY')
        if side in cached and not (mode=='changed_catalogue' and catalogue_reads[side]>1):
            return cached[side]
        value=NS(source=ref,variant='preview1600',profile_hash=m.media_profile_hash(
            'preview1600',sha256(b'P'*128).hexdigest()),manifest=NS(),image=NS(),
            manifest_object=NS(),image_object=NS())
        cached[side]=value
        return value
    def before(*args,**kwargs):
        if mode=='fallback':raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_NOT_READY')
        return objects('before',source_ref)
    def fallback(*args,**kwargs):return objects('before',source_ref)
    def after(*args,**kwargs):return objects('after',after_ref)
    def download(*args,**kwargs):
        events.append(args[3]+' download')
        if mode=='deadline':clock[0]=2.0
        side=args[3];ref=source_ref if side=='before' else after_ref
        if mode=='foreign_source' and side=='after':ref=source_ref
        return NS(verified=NS(source=ref,manifest=kwargs['manifest_ref'],image=kwargs['image_ref']))
    monkeypatch.setattr(m,'find_fixture_pending_render_review',find)
    monkeypatch.setattr(m,'observe_fixture_before_render_preview',before)
    monkeypatch.setattr(m,'observe_fixture_before_asset_preview',fallback)
    monkeypatch.setattr(m,'observe_fixture_after_preview_objects',after)
    monkeypatch.setattr(m,'download_fixture_render_preview',download)
    def run():return m.run_fixture_render_review_previews(object(),tenant,adoption,physical,pending,
        binding,source_raw,('photos',),object(),srgb_profile=b'P'*128,tls_context=object(),
        maximum_seconds=1,
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('valid','fallback'):
        result=run();assert result.pending==pending
        assert result.before.verified.source==source_ref and result.after.verified.source==after_ref
        assert catalogue_reads=={'before':2,'after':2}
        assert pending_reads[0]==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'after download' not in events if mode in ('missing','changed_pending') else True
        if mode in ('fixture_false','activity_false'):assert events==[]
