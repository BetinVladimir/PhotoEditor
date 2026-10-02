"""Composition output metadata proof; binary coverage proof is a separate gate."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.operations.optical.contracts import ImageReport
from .reports import parent_report_evidence


def validate_layer_output_metadata(artifacts, params, result, source, source_parent, proposal_parent):
    if (params.base.source != (source_parent.working_image if source_parent else params.base.source)
            or canonical_hash(source.canvas) != params.base.canvas_hash):
        raise ValueError('LAYER_OUTPUT_SOURCE_INVALID')
    output=artifacts.contract(result.working_image,WorkingImage)
    if (output.canvas!=source.canvas or output.transform_chain!=source.transform_chain
            or output.color!=source.color or output.dtype!=source.dtype):
        raise ValueError('LAYER_OUTPUT_GEOMETRY_INVALID')
    inherited=proposal_parent.generated_mask is not None or (source_parent is not None and source_parent.generated_mask is not None)
    if (result.generated_mask is not None)!=inherited:
        raise ValueError('LAYER_GENERATED_BINDING_INVALID')
    for reference,selector in ((result.changed_mask,'changed'),(result.generated_mask,'generated')):
        if reference is None:continue
        mask=artifacts.contract(reference,MaskRef)
        if (mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain
                or mask.selector!=selector or mask.confidence!=1 or mask.model_lock_id is not None or mask.protected):
            raise ValueError('LAYER_OUTPUT_MASK_INVALID')
    evidence=parent_report_evidence(artifacts,source_parent,proposal_parent,params.base)
    review=evidence.review_required
    report=artifacts.contract(result.report,ImageReport)
    expected='review' if review else 'pass'
    if (tuple(report.parent_reports or ())!=evidence.reports or report.operation!='layer.compose' or report.status!=expected or report.sources!=[params.base]
            or report.reason!=('LAYER_PARENT_REVIEW' if review else None) or report.skipped_count!=0
            or report.registrations or report.identity is not None or report.transform is not None
            or report.outside_mask_max_error!=0 or report.clipping_fraction!=0):
        raise ValueError('LAYER_OUTPUT_REPORT_INVALID')


def validate_generated_coverage(output, mask, *, opacity, blend, source=None,
                                proposal=None, protected=None, check_active=lambda:None):
    """Verify mapped float32 arrays in bounded 512-pixel tiles without publication.

    Callers must separately hash/prove each artifact and canvas binding. Arrays
    remain owned by the caller; no full-size allocation is made here.
    """
    import numpy as np
    from photo_worker.algorithms.layer_lineage import generated_coverage
    check_active()
    if (not isinstance(mask,np.ndarray) or mask.ndim!=2 or mask.dtype!=np.float32
            or min(mask.shape)<1 or mask.size>200000000):
        raise ValueError('LAYER_COVERAGE_SHAPE_INVALID')
    inherited=source is not None or proposal is not None
    if (output is not None)!=inherited:
        raise ValueError('LAYER_GENERATED_BINDING_INVALID')
    for value in (output,source,proposal,protected):
        if value is not None and (not isinstance(value,np.ndarray) or value.shape!=mask.shape or value.dtype!=np.float32):
            raise ValueError('LAYER_COVERAGE_SHAPE_INVALID')
    for y in range(0,mask.shape[0],512):
        for x in range(0,mask.shape[1],512):
            check_active()
            part=(slice(y,y+512),slice(x,x+512))
            tile=mask[part]
            guard=protected[part] if protected is not None else None
            for value in (tile,guard):
                if value is not None and (not np.isfinite(value).all() or np.any((value<0)|(value>1))):
                    raise ValueError('LAYER_COVERAGE_MASK_INVALID')
            expected=generated_coverage(tile,opacity=opacity,blend=blend,
                source=source[part] if source is not None else None,
                proposal=proposal[part] if proposal is not None else None,
                protected=guard,max_tile_bytes=tile.size*64,check_active=check_active)
            if expected is not None:
                actual=output[part]
                if not np.array_equal(actual,expected):
                    raise ValueError('LAYER_GENERATED_COVERAGE_INVALID')
    check_active()


def validate_composed_pixels(output, changed, source, proposal, mask, *, color,
                             opacity, blend, output_alpha=None, source_alpha=None,
                             proposal_alpha=None, protected=None, check_active=lambda:None):
    """Bit-exact mapped output proof against the pinned canonical tile algorithm."""
    import numpy as np
    from photo_worker.algorithms.layer_compose import compose_layer_tile
    check_active()
    if (not isinstance(source,np.ndarray) or source.ndim!=3 or source.shape[-1]!=3
            or source.dtype!=np.float32 or min(source.shape[:2])<1
            or source.shape[0]*source.shape[1]>200000000):
        raise ValueError('LAYER_OUTPUT_SHAPE_INVALID')
    shape=source.shape[:2]
    for value,expected in ((proposal,source.shape),(output,source.shape),(mask,shape),(changed,shape)):
        if not isinstance(value,np.ndarray) or value.shape!=expected or value.dtype!=np.float32:
            raise ValueError('LAYER_OUTPUT_SHAPE_INVALID')
    for value in (output_alpha,source_alpha,proposal_alpha,protected):
        if value is not None and (not isinstance(value,np.ndarray) or value.shape!=shape or value.dtype!=np.float32):
            raise ValueError('LAYER_OUTPUT_SHAPE_INVALID')
    alpha_changed=False
    for y in range(0,shape[0],512):
        for x in range(0,shape[1],512):
            check_active();part=(slice(y,y+512),slice(x,x+512));tile=mask[part]
            guard=protected[part] if protected is not None else None
            for value in (tile,guard):
                if value is not None and (not np.isfinite(value).all() or np.any((value<0)|(value>1))):
                    raise ValueError('LAYER_COVERAGE_MASK_INVALID')
            expected=compose_layer_tile(source[part],proposal[part],tile,color=color,opacity=opacity,blend=blend,
                source_alpha=source_alpha[part] if source_alpha is not None else None,
                proposal_alpha=proposal_alpha[part] if proposal_alpha is not None else None,
                protected=guard,max_tile_bytes=tile.size*256,check_active=check_active)
            if not np.array_equal(output[part].view(np.uint32),expected.rgb.view(np.uint32)):
                raise ValueError('LAYER_OUTPUT_PIXELS_INVALID')
            if not np.array_equal(changed[part],expected.changed):
                raise ValueError('LAYER_OUTPUT_CHANGED_INVALID')
            if expected.alpha is not None:
                original=source_alpha[part] if source_alpha is not None else np.ones(tile.shape,np.float32)
                alpha_changed=alpha_changed or not np.array_equal(expected.alpha.view(np.uint32),original.view(np.uint32))
            wanted=expected.alpha if expected.alpha is not None else np.ones(tile.shape,np.float32)
            actual=output_alpha[part] if output_alpha is not None else np.ones(tile.shape,np.float32)
            if not np.array_equal(actual.view(np.uint32),wanted.view(np.uint32)):
                raise ValueError('LAYER_OUTPUT_ALPHA_INVALID')
    if (output_alpha is not None)!=(source_alpha is not None or alpha_changed):
        raise ValueError('LAYER_OUTPUT_ALPHA_INVALID')
    check_active()
