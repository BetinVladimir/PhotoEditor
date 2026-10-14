"""Perspective metadata replay; original pixel replay and live SQL are separate."""
from photo_worker.contracts.base import canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
from photo_worker.contracts.optical_perspective import OpticalPerspectiveParams,LineSegments
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.optical.perspective_geometry import resolve_perspective_geometry
from photo_worker.operations.optical.perspective_result import perspective_working_image
from .adoption import AdoptionInvalid
from .lens_bypass import _document as read_document,_ref,verify_zero_npy_mask


def verify_perspective_result_metadata(reader,params_ref,root_ref,*,max_pixels,check_active):
    if not callable(check_active) or type(max_pixels) is not int or not 1<=max_pixels<=200000000:
        raise ValueError('PERSPECTIVE_ADOPTION_AUTHORITY_INVALID')
    def document(ref,model,origin):
        check_active();value=read_document(reader,ref,model,origin)
        if canonical_hash(value)!=ref.sha256:raise AdoptionInvalid('PERSPECTIVE_DOCUMENT_NONCANONICAL')
        return value
    params=document(params_ref,OpticalPerspectiveParams,'input')
    source=document(_ref(params.base.source),WorkingImage,'input')
    if source.dtype!='float32' or params.base.canvas_hash!=canonical_hash(source.canvas):
        raise AdoptionInvalid('PERSPECTIVE_SOURCE_MISMATCH')
    lines=None if params.lines is None else document(_ref(params.lines),LineSegments,'input')
    geometry=resolve_perspective_geometry(params,source,lines,max_pixels=max_pixels,check_active=check_active)
    result=document(root_ref,ImageResult,'output')
    if result.generated_mask is not None:raise AdoptionInvalid('PERSPECTIVE_GENERATED_CONTENT')
    report=document(_ref(result.report),ImageReport,'output')
    changed=document(_ref(result.changed_mask),MaskRef,'output')
    refs=[root_ref,_ref(result.report),_ref(result.changed_mask),_ref(changed.artifact)]
    roles=['document','report','mask','mask_pixels']
    if params.strength==0:
        if result.working_image!=params.base.source:raise AdoptionInvalid('PERSPECTIVE_BYPASS_MISMATCH')
        image=source
        expected_report=ImageReport(schema_version=1,status='pass',operation='optical.perspective',skipped_count=0,
            reason=None,outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[params.base],registrations=[])
    else:
        image=document(_ref(result.working_image),WorkingImage,'output')
        if image.alpha is None or report.transform is None:raise AdoptionInvalid('PERSPECTIVE_METADATA_MISMATCH')
        transform=document(_ref(report.transform),ResolvedTransform,'output')
        expected_image,expected_transform=perspective_working_image(source,image.pixels,image.alpha,geometry)
        if canonical_bytes(image)!=canonical_bytes(expected_image) or canonical_bytes(transform)!=canonical_bytes(expected_transform):
            raise AdoptionInvalid('PERSPECTIVE_GEOMETRY_MISMATCH')
        refs.extend((_ref(result.working_image),_ref(image.pixels),_ref(image.alpha),_ref(report.transform)))
        roles.extend(('working_image','pixels','alpha','transform'))
        expected_report=ImageReport(schema_version=1,status='review',operation='optical.perspective',skipped_count=0,
            reason='perspective_'+params.mode,outside_mask_max_error=0.,clipping_fraction=report.clipping_fraction,
            identity=None,transform=report.transform,sources=[params.base],registrations=[])
    expected_mask=MaskRef(artifact=changed.artifact,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    if canonical_bytes(changed)!=canonical_bytes(expected_mask) or canonical_bytes(report)!=canonical_bytes(expected_report):
        raise AdoptionInvalid('PERSPECTIVE_METADATA_MISMATCH')
    if len({ref.artifact_id for ref in refs})!=len(refs):raise AdoptionInvalid('PERSPECTIVE_OUTPUT_ALIAS')
    for ref,role in zip(refs,roles):
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='output' or evidence.role!=role:
            raise AdoptionInvalid('PERSPECTIVE_OUTPUT_EVIDENCE_INVALID')
    check_active();return params,result


def verify_perspective_prepared_arrays(files,output,alpha,changed,*,clipping_fraction,check_active):
    """Compare independently prepared files with digest-verified staged arrays."""
    import numpy as np
    from contextlib import ExitStack
    height,width=files.geometry.height,files.geometry.width
    if (output.dtype!=np.float32 or output.shape!=(height,width,3) or
        any(value.dtype!=np.float32 or value.shape!=(height,width) for value in (alpha,changed))):
        raise AdoptionInvalid('PERSPECTIVE_ARRAY_INVALID')
    with ExitStack() as owners:
        def load(path):
            mapped=np.load(path,mmap_mode='r',allow_pickle=False)
            owners.callback(mapped._mmap.close);return mapped
        expected_rgb=load(files.rgb);expected_alpha=load(files.alpha);expected_changed=load(files.changed_mask)
        for y in range(0,height,512):
            for x in range(0,width,512):
                check_active();region=(slice(y,y+512),slice(x,x+512))
                if (not np.array_equal(output[region].view(np.uint32),expected_rgb[region].view(np.uint32)) or
                    not np.array_equal(alpha[region].view(np.uint32),expected_alpha[region].view(np.uint32)) or
                    not np.array_equal(changed[region].view(np.uint32),expected_changed[region].view(np.uint32))):
                    raise AdoptionInvalid('PERSPECTIVE_PIXEL_REPLAY_MISMATCH')
    if type(clipping_fraction) not in (int,float) or clipping_fraction!=files.clipping_fraction:
        raise AdoptionInvalid('PERSPECTIVE_CLIPPING_MISMATCH')
    check_active()


def verify_perspective_pixel_replay(reader,params_ref,root_ref,*,max_pixels,scratch_root,max_bytes,ram_max_bytes,check_active):
    """Replay original geometry/transfer/alpha under a combined scratch budget.

    Metadata and physical replay alone do not grant current SQL adoption authority.
    Runtime qualification must independently pin these implementation bytes.
    """
    import numpy as np
    from uuid import uuid4
    from .array_evidence import ArrayEvidenceSession
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.optical.perspective_prepare import prepare_source_perspective
    from photo_worker.operations.optical.perspective_resources import perspective_resources
    params,result=verify_perspective_result_metadata(reader,params_ref,root_ref,max_pixels=max_pixels,check_active=check_active)
    source=read_document(reader,_ref(params.base.source),WorkingImage,'input')
    lines=None if params.lines is None else read_document(reader,_ref(params.lines),LineSegments,'input')
    geometry=resolve_perspective_geometry(params,source,lines,max_pixels=max_pixels,check_active=check_active)
    required=perspective_resources(source.canvas.width,source.canvas.height,geometry.width,geometry.height)
    if any(type(value) is not int or not 1<=value<=2**53-1 for value in (max_bytes,ram_max_bytes)):
        raise AdoptionInvalid('PERSPECTIVE_REPLAY_RESOURCE_INVALID')
    # Both the staged output mappings and independent prepared output remain live.
    replay_output_bytes=20*geometry.width*geometry.height
    if ram_max_bytes<required['ram_bytes']+replay_output_bytes:raise AdoptionInvalid('PERSPECTIVE_REPLAY_RAM_EXCEEDED')
    if max_bytes<required['scratch_bytes']+replay_output_bytes:raise AdoptionInvalid('PERSPECTIVE_REPLAY_SCRATCH_EXCEEDED')
    mask=read_document(reader,_ref(result.changed_mask),MaskRef,'output')
    report=read_document(reader,_ref(result.report),ImageReport,'output')
    output_shape=(geometry.height,geometry.width)
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        changed=arrays.array(_ref(mask.artifact),output_shape,source='output',role='mask_pixels',unit=True)
        if params.strength==0:
            for y in range(0,geometry.height,512):
                for x in range(0,geometry.width,512):
                    check_active()
                    if np.any(changed[y:y+512,x:x+512].view(np.uint32)!=0):
                        raise AdoptionInvalid('PERSPECTIVE_BYPASS_MASK_INVALID')
            return params,result
        image=read_document(reader,_ref(result.working_image),WorkingImage,'output')
        shape=(source.canvas.height,source.canvas.width)
        before=arrays.array(_ref(source.pixels),(*shape,3),source='input',legacy_input_npy=True)
        source_alpha=None if source.alpha is None else arrays.array(_ref(source.alpha),shape,source='input',unit=True,legacy_alpha=True,legacy_input_npy=True)
        after=arrays.array(_ref(image.pixels),(*output_shape,3),source='output',role='pixels')
        output_alpha=arrays.array(_ref(image.alpha),output_shape,source='output',role='alpha',unit=True)
        # Reserve the remaining combined budget, not a second full max_bytes.
        with ScratchSpace(scratch_root,uuid4(),uuid4(),1,max_bytes-arrays.used) as scratch:
            with prepare_source_perspective(source,before,source_alpha,geometry,scratch,check_active=check_active) as files:
                verify_perspective_prepared_arrays(files,after,output_alpha,changed,
                    clipping_fraction=report.clipping_fraction,check_active=check_active)
        check_active()
    return params,result


def verify_pinned_perspective_replay(reader,params_ref,root_ref,*,runtime_lock,runtime_sha,max_pixels,scratch_root,max_bytes,ram_max_bytes,check_active):
    from photo_worker.operations.optical.perspective_runtime_lock import PerspectiveRuntimeLock,verify_installed_runtime
    owned=PerspectiveRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
    check_active();verify_installed_runtime(owned,runtime_sha)
    result=verify_perspective_pixel_replay(reader,params_ref,root_ref,max_pixels=max_pixels,scratch_root=scratch_root,
        max_bytes=max_bytes,ram_max_bytes=ram_max_bytes,check_active=check_active)
    check_active();verify_installed_runtime(owned,runtime_sha);check_active()
    return result


def verify_perspective_identity(reader,params_ref,root_ref,*,check_active):
    """Allow required image-role reuse only for a proven exact zero operation."""
    params,result=verify_perspective_result_metadata(reader,params_ref,root_ref,max_pixels=200000000,check_active=check_active)
    if params.strength!=0:raise AdoptionInvalid('PERSPECTIVE_BYPASS_NOT_IDENTITY')
    source=read_document(reader,_ref(params.base.source),WorkingImage,'input')
    mask=read_document(reader,_ref(result.changed_mask),MaskRef,'output')
    verify_zero_npy_mask(reader,_ref(mask.artifact),(source.canvas.height,source.canvas.width),
        check_active=check_active,error_code='PERSPECTIVE_BYPASS_MASK_NONZERO')
    check_active();return _ref(params.base.source)
