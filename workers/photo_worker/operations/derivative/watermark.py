from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.color import to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.derivative import WatermarkParams,DerivativeReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .common import open_image,stage_arrays,validate_image
from .memory import admit_derivative


def _resize_mark(rgb, alpha, width, height, opacity):
    """Return linear premultiplied mark and opacity after area resampling."""
    linear = to_linear(np.asarray(rgb))
    premult = cv2.resize(linear * alpha[..., None], (width, height), interpolation=cv2.INTER_AREA)
    coverage = np.clip(cv2.resize(alpha, (width, height), interpolation=cv2.INTER_AREA), 0, 1)
    return premult * opacity, coverage * opacity


def _over_mark(rgb, alpha, premult, coverage, x, y):
    """Porter-Duff source-over with straight RGB output, owning all buffers."""
    height, width = coverage.shape
    encoded = np.array(rgb, copy=True)
    opacity = None if alpha is None else np.array(alpha, copy=True)
    if not np.any(coverage):
        return encoded, opacity, 0.
    region = rgb[y:y+height, x:x+width]
    linear = to_linear(np.asarray(region))
    base_alpha = np.ones_like(coverage) if alpha is None else alpha[y:y+height, x:x+width]
    combined_alpha = coverage + base_alpha * (1 - coverage)
    combined = premult + linear * base_alpha[..., None] * (1 - coverage[..., None])
    straight = np.zeros_like(combined)
    # Small positive alpha still has a defined straight color. The previous
    # epsilon made faint marks on transparent pixels black after unpremultiply.
    np.divide(combined, combined_alpha[..., None], out=straight, where=combined_alpha[..., None] > 0)
    changed = to_encoded(straight)
    # Keep hidden source RGB and untouched pixels bit-identical where mark is absent.
    encoded[y:y+height, x:x+width] = np.where(coverage[..., None] > 0, changed, region)
    if opacity is not None:
        opacity[y:y+height, x:x+width] = combined_alpha
    delta = np.where(coverage[..., None] > 0, np.abs(straight - linear), 0)
    return encoded, opacity, float(np.max(delta))


def _placement(width, height, mark_width, mark_height, params):
    """Compute placement before array IO using the existing ties-even policy."""
    scale = min(1., width * params.max_width_fraction / mark_width)
    mw = max(1, round(mark_width * scale))
    mh = max(1, round(mark_height * scale))
    mx, my = round(params.margin_x * width), round(params.margin_y * height)
    positions = {
        'top_left': (mx, my),
        'top_right': (width - mw - mx, my),
        'bottom_left': (mx, height - mh - my),
        'bottom_right': (width - mw - mx, height - mh - my),
        'center': ((width - mw) // 2, (height - mh) // 2),
    }
    if params.anchor not in positions:
        raise ValueError('Watermark anchor invalid')
    x, y = positions[params.anchor]
    if x < 0 or y < 0 or x + mw > width or y + mh > height:
        raise ValueError('Watermark placement outside canvas')
    return mw, mh, x, y


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    try:
        p=artifacts.params(WatermarkParams);image,required=validate_image(ctx,artifacts,p.image);mark,mark_required=validate_image(ctx,artifacts,p.watermark)
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=required+mark_required or image.color!=mark.color:raise ValueError('Watermark binding mismatch')
        if mark.alpha is None:raise ValueError('Watermark requires alpha')
        w,h=image.canvas.width,image.canvas.height
        mw,mh,x,y=_placement(w,h,mark.canvas.width,mark.canvas.height,p)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    # Scheduler watermark policy budgets original source plus original mark.
    # Refuse undersized admission before opening either physical array.
    admit_derivative(ctx.lease.execution_profile, image.canvas.width, image.canvas.height,
                     mark.canvas.width, mark.canvas.height)
    with ExitStack() as stack:
        rgb,alpha=open_image(stack,ctx,artifacts,image);mrgb,ma=open_image(stack,ctx,artifacts,mark)
        mrgb,ma=_resize_mark(mrgb,ma,mw,mh,p.opacity)
        encoded,out_alpha,maximum=_over_mark(rgb,alpha,mrgb,ma,x,y)
        with artifacts.scratch.reserve(w*h*(16 if alpha is not None else 12)+8192) as reservation:pixel,alpha_ref,outputs,written=stage_arrays(artifacts,reservation,encoded,out_alpha)
    result_image=image.model_copy(update={'pixels':pixel,'alpha':alpha_ref});image_ref=artifacts.stage_contract(result_image,'working_image');report=DerivativeReport(image=p.image,operation='watermark',applied=p.opacity>0,width=w,height=h,max_channel_delta=maximum,warnings=['zero_opacity'] if p.opacity==0 else []);report_ref=artifacts.stage_contract(report,'derivative_report');return OperationResult(outputs=[*outputs,image_ref,report_ref],bytes_written=written+len(canonical_bytes(result_image))+len(canonical_bytes(report)))


def watermark_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','derivative_report'})) as a:return execute(ctx,a)
    return [Operation('derivative.watermark','cpu',run)]
