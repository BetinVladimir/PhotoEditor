"""Quality-gated staging of a prepared same-canvas render result."""
from .render_quality import build_render_quality
from .render_files import prepare_render_output_files
from .render_stage import stage_render_image
from .render_result import stage_render_result


def publish_prepared_render(artifacts,inputs,prepared,measured,parents,*,tile_size=512,check_active=lambda:None):
    """Validate before writes; caller retains preparation and submits adoption.

    This stages attempt artifacts, not a database commit. Parent inventory must
    include source roots verified at preparation, plus every qualified base and
    layer provider result. Caller proves provider inventory completeness.
    """
    check_active();binding=inputs.recipe.recipe.source
    quality=build_render_quality(artifacts,measured,parents,binding,check_active=check_active)
    pins={(ref.artifact_id,ref.sha256) for ref in quality.parents.reports}
    if (any((ref.artifact_id,ref.sha256) not in pins for ref in prepared.source_reports.reports)
            or prepared.source_reports.review_required and not quality.parents.review_required):
        raise ValueError('RENDER_PUBLICATION_SOURCE_REPORTS_DROPPED')
    from .render_effects import effect_review_required
    if effect_review_required(getattr(prepared,'effect_evidence',None)):
        from .render_quality import RenderQuality
        from photo_worker.operations.optical.contracts import ImageReport
        body=quality.report.model_dump(mode='python')
        body['status']='review'
        if body['reason'] is None:body['reason']='RENDER_EFFECT_REVIEW'
        quality=RenderQuality(ImageReport.model_validate(body),quality.parents)
    with prepare_render_output_files(prepared,artifacts.scratch,tile_size=tile_size,check_active=check_active) as files:
        image=stage_render_image(artifacts,inputs.source,binding,files,
            alpha_converted=prepared.alpha_requires_new_reference,check_active=check_active)
        return stage_render_result(artifacts,image,quality.report,binding,quality.parents,check_active=check_active)
