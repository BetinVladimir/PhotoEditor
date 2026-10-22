"""Before/after physical previews for one still-pending render review."""
from dataclasses import dataclass
from hashlib import sha256
from time import monotonic
from photo_worker.contracts.base import canonical_bytes,parse_json,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.media.derivative import media_profile_hash
from photo_worker.operations.session.contracts import VersionBinding
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_batch_physical import RestoreBatchPhysicalEvidence
from .restore_render_review_pending import RestorePendingRenderReview,find_fixture_pending_render_review
from .restore_render_preview_catalogue import (
    observe_fixture_before_asset_preview,observe_fixture_before_render_preview,
    observe_fixture_after_preview_objects)
from .restore_render_preview_download import download_fixture_render_preview
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderReviewPreviews:
    pending: RestorePendingRenderReview
    before: object
    after: object
    before_catalogue: object
    after_catalogue: object


def run_fixture_render_review_previews(connection,tenant_id,adoption,physical,pending,
                                       source_binding,source_raw,buckets,download_target,*,
                                       srgb_profile,tls_context,maximum_seconds,
                                       assert_installed_fixture,check_active):
    """Preview both sides before deciding; no decision or writer unlock."""
    if (not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not isinstance(physical,RestoreBatchPhysicalEvidence)
            or not isinstance(pending,RestorePendingRenderReview)
            or not isinstance(source_binding,VersionBinding)
            or type(source_raw) is not bytes or not 2<=len(source_raw)<=1048576
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4*1048576
            or not isinstance(buckets,(tuple,list)) or not buckets
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_INSTALLATION')
    if (pending.database!=adoption.database or pending.tenant_id!=tenant_id
            or pending.child_run_id!=adoption.child_run_id
            or pending.candidate!=physical.image_result.working_image
            or sha256(source_raw).hexdigest()!=source_binding.source.sha256):
        raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_SOURCE')
    source=WorkingImage.model_validate_json(source_raw)
    if (canonical_bytes(parse_json(source_raw))!=source_raw
            or canonical_bytes(source)!=source_raw
            or canonical_hash(source.canvas)!=source_binding.canvas_hash):
        raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_SOURCE')
    after=physical.working_image
    if not isinstance(after,WorkingImage):
        raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_AFTER')
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_TIMEOUT')
    def current():return find_fixture_pending_render_review(connection,tenant_id,adoption,physical,
        assert_fixture_scope=guard,check_active=guard)
    guard()
    if current()!=pending:raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_PENDING_CHANGED')
    profile=media_profile_hash('preview1600',sha256(srgb_profile).hexdigest())
    observed=[];catalogues=[]
    for side,ref,image in (('before',source_binding.source,source),
                           ('after',pending.candidate,after)):
        guard()
        if side=='after' and current()!=pending:
            raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_PENDING_CHANGED')
        def catalogue():
            if side=='before':
                try:
                    return observe_fixture_before_render_preview(connection,tenant_id,pending.review_id,
                        ref,'preview1600',profile,buckets,expected_database=adoption.database,
                        assert_fixture_scope=guard,check_active=guard)
                except ValueError as exc:
                    if str(exc)!='RESTORE_RENDER_PREVIEW_CATALOGUE_NOT_READY':raise
                    return observe_fixture_before_asset_preview(connection,tenant_id,pending.review_id,
                        ref,'preview1600',profile,buckets,expected_database=adoption.database,
                        assert_fixture_scope=guard,check_active=guard)
            return observe_fixture_after_preview_objects(connection,tenant_id,pending.review_id,
                pending.task_id,ref,'preview1600',profile,buckets,expected_database=adoption.database,
                assert_fixture_scope=guard,check_active=guard)
        objects=catalogue()
        if (objects.source!=ref or objects.variant!='preview1600'
                or objects.profile_hash!=profile):
            raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_CATALOGUE')
        loaded=download_fixture_render_preview(download_target,tenant_id,pending.review_id,
            side,'preview1600',source=ref,source_width=image.canvas.width,
            source_height=image.canvas.height,source_alpha=image.alpha is not None,
            manifest_ref=objects.manifest,manifest_object=objects.manifest_object,
            image_ref=objects.image,image_object=objects.image_object,
            srgb_profile=srgb_profile,tls_context=tls_context,
            assert_fixture_scope=guard,check_active=guard)
        if (loaded.verified.source!=ref or loaded.verified.manifest!=objects.manifest
                or loaded.verified.image!=objects.image or catalogue()!=objects):
            raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_CHANGED')
        observed.append(loaded);catalogues.append(objects)
    if current()!=pending:raise ValueError('RESTORE_RENDER_REVIEW_PREVIEWS_PENDING_CHANGED')
    guard()
    return RestoreRenderReviewPreviews(pending,observed[0],observed[1],
                                       catalogues[0],catalogues[1])
