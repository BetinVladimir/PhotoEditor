"""Stage the complete V29 multi-image result closure; adoption stays separate."""
from dataclasses import dataclass
from math import isfinite
import numpy as np
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef, DenseWarpTransform
from .contracts import ImageReport, ImageResult
from .multi_lineage import compile_multi_lineage
from .multi_image_prepare import PreparedMultiImage


@dataclass(frozen=True)
class StagedMultiResult:
    document: ArtifactRef
    outputs: tuple[ArtifactRef, ...]
    required_inputs: tuple[ArtifactRef, ...]
    bytes_written: int
    review_required: bool


def multi_required_inputs(images, bindings):
    """Ordered direct roots and typed children of every source WorkingImage."""
    if len(images) != len(bindings) or not 2 <= len(images) <= 30:
        raise ValueError('MULTI_SOURCE_MISMATCH')
    result = []
    seen = {}
    versions = set()
    for image, binding in zip(images, bindings):
        if (canonical_hash(image) != binding.source.sha256
                or canonical_hash(image.canvas) != binding.canvas_hash
                or binding.asset_version_id in versions):
            raise ValueError('MULTI_SOURCE_MISMATCH')
        versions.add(binding.asset_version_id)
        children = [(binding.source, 'document'), (image.pixels, 'pixels')]
        if image.alpha is not None:
            children.append((image.alpha, 'alpha'))
        children.extend((step.inverse_field, 'warp_field') for step in image.transform_chain.steps
                        if isinstance(step, DenseWarpTransform))
        if (image.alpha is not None and image.alpha.artifact_id == image.pixels.artifact_id
                or any(ref.artifact_id == binding.source.artifact_id for ref, _ in children[1:])):
            raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
        for ref, kind in children:
            previous = seen.get(ref.artifact_id)
            if previous is not None and previous[1] != kind:
                raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
            if previous is not None and previous[0] != ref.sha256:
                raise ValueError('MULTI_SOURCE_MISMATCH')
            if previous is None:
                seen[ref.artifact_id] = (ref.sha256, kind)
                result.append(ref)
    return tuple(result)


def multi_clipping_fraction(rgb, *, tile=256, check_active=lambda: None):
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3
            or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1 or rgb.shape[0]*rgb.shape[1] > 200000000
            or type(tile) is not int or not 1 <= tile <= 512):
        raise ValueError('MULTI_RESULT_INVALID')
    clipped = 0
    for y in range(0, rgb.shape[0], tile):
        for x in range(0, rgb.shape[1], tile):
            check_active(); block = rgb[y:y+tile, x:x+tile]
            if not np.isfinite(block).all():
                raise ValueError('MULTI_RESULT_INVALID')
            clipped += int(np.count_nonzero(np.any((block < 0) | (block >= 1), axis=2)))
    check_active()
    return clipped / (rgb.shape[0]*rgb.shape[1])


def multi_image_report(operation,bindings,registrations,*,motion_fraction,registration_inconsistent,
                       clipping_fraction=0.):
    if (operation not in ('panorama','focus_stack') or type(motion_fraction) not in (int,float)
            or not isfinite(motion_fraction) or not 0<=motion_fraction<=1
            or type(clipping_fraction) not in (int,float) or not isfinite(clipping_fraction)
            or not 0<=clipping_fraction<=1
            or operation=='panorama' and len(bindings)>20
            or type(registration_inconsistent) is not bool or len(bindings)!=len(registrations)
            or not 2<=len(bindings)<=30):raise ValueError('MULTI_REPORT_INVALID')
    reason=f'{operation.upper()}_REVIEW;motion_fraction={motion_fraction:.17g}'
    if registration_inconsistent:reason+=';REGISTRATION_INCONSISTENT'
    return ImageReport(schema_version=1,status='review',operation='optical.'+operation,
        skipped_count=0,reason=reason,outside_mask_max_error=0.,clipping_fraction=clipping_fraction,
        identity=None,transform=None,sources=list(bindings),registrations=list(registrations))


def stage_multi_result(artifacts,prepared:PreparedMultiImage,*,check_active=lambda:None,tile=256):
    """Stage encoded pixels, every nonlinear field and registration, mask/report.

    Independent adoption must replay pixels, registration, geometry and evidence
    against current authorized original inputs. A staged result is never Apply.
    """
    if not isinstance(prepared,PreparedMultiImage) or type(tile) is not int or not 1<=tile<=512:
        raise ValueError('MULTI_RESULT_INVALID')
    plan=prepared.automatic.prepared;files=prepared.encoded
    if len(plan.registrations)!=len(prepared.images) or len(prepared.bindings)!=len(prepared.images):
        raise ValueError('MULTI_RESULT_INVALID')
    if prepared.projections and len(prepared.projections)!=len(prepared.images):
        raise ValueError('MULTI_RESULT_INVALID')
    for image,binding in zip(prepared.images,prepared.bindings):
        if canonical_hash(image)!=binding.source.sha256 or canonical_hash(image.canvas)!=binding.canvas_hash:
            raise ValueError('MULTI_SOURCE_MISMATCH')
    required_inputs=multi_required_inputs(prepared.images,prepared.bindings)
    if (plan.operation not in ('panorama','focus_stack') or type(plan.reference) is not int
            or not 0<=plan.reference<len(prepared.images) or type(prepared.geometry_changed) is not bool
            or type(prepared.automatic.registration_inconsistent) is not bool
            or type(plan.result.motion_fraction) not in (int,float)
            or not isfinite(plan.result.motion_fraction) or not 0<=plan.result.motion_fraction<=1):
        raise ValueError('MULTI_RESULT_INVALID')
    reference=prepared.images[plan.reference]
    reference_shape=(reference.canvas.height,reference.canvas.width)
    from photo_worker.algorithms.multi_registration import _matrix
    transforms=[_matrix(r.source_to_output,(image.canvas.height,image.canvas.width))
                for r,image in zip(plan.registrations,prepared.images)]
    derived_geometry=bool(prepared.projections) or plan.shape!=reference_shape or not np.array_equal(transforms[plan.reference],np.eye(3))
    if prepared.geometry_changed!=derived_geometry:
        raise ValueError('MULTI_RESULT_INVALID')
    if plan.operation=='focus_stack' and (prepared.projections or plan.shape!=reference_shape or not np.array_equal(transforms[plan.reference],np.eye(3))):
        raise ValueError('MULTI_RESULT_INVALID')
    files.reservation.validate();check_active()
    # Validate all final arrays before the first staged write, including mask
    # semantics and NPY shapes. Exact object bytes are checked by the stager.
    for path,shape,kind in ((files.rgb,(*plan.shape,3),'rgb'),(files.alpha,plan.shape,'alpha'),
                            (files.changed_mask,plan.shape,'mask')):
        files.reservation.validate_file(path)
        value=np.load(path,mmap_mode='r',allow_pickle=False)
        try:
            if value.dtype!=np.float32 or value.shape!=shape:raise ValueError('MULTI_RESULT_INVALID')
            for y in range(0,plan.shape[0],tile):
                for x in range(0,plan.shape[1],tile):
                    check_active();block=value[y:y+tile,x:x+tile]
                    if not np.isfinite(block).all():raise ValueError('MULTI_RESULT_INVALID')
                    if kind=='alpha' and (np.any(block<0) or np.any(block>1)):raise ValueError('MULTI_RESULT_INVALID')
                    if kind=='mask' and np.any((block!=0)&(block!=1)):raise ValueError('MULTI_RESULT_INVALID')
                    if kind=='mask' and prepared.geometry_changed and not np.all(block==1):raise ValueError('MULTI_RESULT_INVALID')
        finally:value._mmap.close()
    if prepared.projections:
        from .cylindrical_validation import verify_cylindrical_files
        for projection,image in zip(prepared.projections,prepared.images):
            verify_cylindrical_files(projection,(image.canvas.height,image.canvas.width),
                tile=tile,check_active=check_active)
    outputs=[];written=0
    def stage(path,reservation,role):
        nonlocal written
        check_active();reservation.validate_file(path)
        ref=artifacts.stage(path,reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size;return ref
    def contract(value,role):
        nonlocal written
        check_active();ref=artifacts.stage_contract(value,role)
        outputs.append(ref);written+=len(canonical_bytes(value));return ref
    pixels=stage(files.rgb,files.reservation,'pixels');alpha=stage(files.alpha,files.reservation,'alpha')
    fields=[stage(p.inverse_field,p.reservation,'warp_field') for p in prepared.projections] if prepared.projections else None
    image,registrations=compile_multi_lineage(prepared.images,prepared.bindings,
        [r.source_to_output for r in plan.registrations],pixels,alpha,operation='optical.'+plan.operation,
        reference=plan.reference,output_shape=plan.shape,inverse_fields=fields)
    image_ref=contract(image,'working_image')
    registration_refs=[contract(registration,'registration') for registration in registrations]
    changed_pixels=stage(files.changed_mask,files.reservation,'mask_pixels')
    changed_ref=contract(MaskRef(artifact=changed_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False),'mask')
    encoded_rgb=np.load(files.rgb,mmap_mode='r',allow_pickle=False)
    try:
        clipping_fraction=multi_clipping_fraction(encoded_rgb,tile=tile,check_active=check_active)
    finally:
        encoded_rgb._mmap.close()
    report=multi_image_report(plan.operation,prepared.bindings,registration_refs,
        motion_fraction=plan.result.motion_fraction,
        registration_inconsistent=prepared.automatic.registration_inconsistent,
        clipping_fraction=clipping_fraction)
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,report=report_ref,
        changed_mask=changed_ref,generated_mask=None),'document')
    check_active()
    return StagedMultiResult(document,tuple(outputs),required_inputs,written,True)
