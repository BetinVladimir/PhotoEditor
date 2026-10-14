"""Source-bound perspective result closure with legacy center coordinates."""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import Canvas,MatrixTransform,Rect,TransformChain,WorkingImage,MaskRef,ResolvedTransform
from photo_worker.algorithms.perspective import checked_homography
from .contracts import ImageResult,ImageReport


def perspective_center_matrix(source,geometry):
    """Conjugate edge-coordinate H into existing integer-center MatrixTransform."""
    edge=checked_homography(geometry.source_to_output,[(0,0),(source.canvas.width,0),
        (source.canvas.width,source.canvas.height),(0,source.canvas.height)])
    to_edge=np.array([[1.,0.,.5],[0.,1.,.5],[0.,0.,1.]])
    to_center=np.array([[1.,0.,-.5],[0.,1.,-.5],[0.,0.,1.]])
    center=to_center@edge@to_edge
    if abs(center[2,2])>1e-10:center/=center[2,2]
    if not np.isfinite(center).all() or np.any(np.abs(center)>1e8):
        raise ValueError('PERSPECTIVE_GEOMETRY_INVALID')
    return center.reshape(-1).tolist()


def perspective_working_image(source,pixels:ArtifactRef,alpha:ArtifactRef,geometry):
    matrix=perspective_center_matrix(source,geometry)
    if any(type(v) is not int or v<1 for v in (geometry.width,geometry.height)) or geometry.width*geometry.height>200000000:
        raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    if source.dtype!='float32':raise ValueError('PERSPECTIVE_SOURCE_INVALID')
    space='perspective:'+canonical_hash({'canvas':source.canvas.model_dump(mode='json'),'chain':source.transform_chain.hash,
        'matrix':matrix,'width':geometry.width,'height':geometry.height})[:32]
    canvas=Canvas(width=geometry.width,height=geometry.height,orientation=1,space_id=space,
        source_sha256=source.canvas.source_sha256)
    step=MatrixTransform(kind='matrix',from_canvas=source.canvas,to_canvas=canvas,
        invertibility='partial',valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height),forward_matrix=matrix)
    chain=TransformChain.create([*source.transform_chain.steps,step])
    image=WorkingImage(pixels=pixels,alpha=alpha,canvas=canvas,color=source.color,dtype='float32',transform_chain=chain)
    transform=ResolvedTransform(schema_version=1,from_canvas_hash=canonical_hash(source.canvas),
        to_canvas_hash=canonical_hash(canvas),matrix=matrix,kind='homography')
    return image,transform


@dataclass(frozen=True)
class StagedPerspectiveResult:
    document:ArtifactRef
    outputs:tuple[ArtifactRef,...]
    review_required:bool
    bytes_written:int


def stage_perspective_result(artifacts,source_ref,source,binding,files,*,mode):
    """Stage declared closure after validating all source/geometry metadata.

    Every correction requires review; generated_mask is always null. This
    function does not grant adoption authority or enable an operation registry.
    """
    if mode not in ('manual','auto'):raise ValueError('PERSPECTIVE_MODE_INVALID')
    if binding.source!=source_ref or binding.canvas_hash!=canonical_hash(source.canvas):
        raise ValueError('PERSPECTIVE_SOURCE_MISMATCH')
    if not np.isfinite(files.clipping_fraction) or not 0<=files.clipping_fraction<=1:
        raise ValueError('PERSPECTIVE_REPORT_INVALID')
    # Validate transform before publishing any pixel files, using placeholders
    # that are replaced by the actual immutable staged references below.
    perspective_working_image(source,source.pixels,source.alpha or source.pixels,files.geometry)
    files.reservation.validate()
    outputs=[];written=0
    def stage(path,role):
        nonlocal written
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size
        return ref
    def contract(value,role):
        nonlocal written
        ref=artifacts.stage_contract(value,role);outputs.append(ref);written+=len(canonical_bytes(value));return ref
    pixels=stage(files.rgb,'pixels');alpha=stage(files.alpha,'alpha')
    image,transform=perspective_working_image(source,pixels,alpha,files.geometry)
    image_ref=contract(image,'working_image');transform_ref=contract(transform,'transform')
    changed_pixels=stage(files.changed_mask,'mask_pixels')
    changed=MaskRef(artifact=changed_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    changed_ref=contract(changed,'mask')
    report=ImageReport(schema_version=1,status='review',operation='optical.perspective',skipped_count=0,
        reason='perspective_'+mode,outside_mask_max_error=0.,clipping_fraction=files.clipping_fraction,
        identity=None,transform=transform_ref,sources=[binding],registrations=[])
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,report=report_ref,
        changed_mask=changed_ref,generated_mask=None),'document')
    return StagedPerspectiveResult(document,tuple(outputs),True,written)
