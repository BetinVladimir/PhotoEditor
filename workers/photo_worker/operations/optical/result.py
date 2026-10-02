"""Construct lens image geometry from already staged, immutable pixel artifacts."""
from photo_worker.contracts.base import ArtifactRef, canonical_hash, canonical_bytes
from photo_worker.contracts.images import Canvas, DenseWarpTransform, Rect, TransformChain, WorkingImage


def lens_working_image(source: WorkingImage, pixels: ArtifactRef,
                       alpha: ArtifactRef, field: ArtifactRef,
                       *, strength: float) -> WorkingImage:
    """Never claim a homography for the nonlinear green inverse field.

    Zero strength retains the original canvas and chain. Nonzero correction
    changes space identity and uses a partial dense transform with explicit
    half-pixel coordinates; alpha carries actual validity, without guessed crop.
    """
    if type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('LENS_PARAMS_INVALID')
    if source.dtype!='float32':
        raise ValueError('LENS_IMAGE_INVALID')
    canvas=source.canvas;chain=source.transform_chain
    if strength!=0:
        space='lens:'+canonical_hash({'canvas':source.canvas.model_dump(mode='json'),
                                     'chain':source.transform_chain.hash,
                                     'field':field.model_dump(mode='json')})[:32]
        canvas=Canvas(width=canvas.width,height=canvas.height,orientation=1,
                      space_id=space,source_sha256=canvas.source_sha256)
        step=DenseWarpTransform(kind='dense_warp',from_canvas=source.canvas,
            to_canvas=canvas,invertibility='partial',
            valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height),
            inverse_field=field,pixel_centers='half_pixel')
        chain=TransformChain.create([*source.transform_chain.steps,step])
    return WorkingImage(pixels=pixels,alpha=alpha,canvas=canvas,color=source.color,
                        dtype='float32',transform_chain=chain)


from dataclasses import dataclass
from .contracts import ImageReport, ImageResult
from .prepare import LensFiles
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.images import MaskRef


@dataclass(frozen=True)
class StagedLensResult:
    document: ArtifactRef
    outputs: tuple[ArtifactRef, ...]
    review_required: bool
    bytes_written: int


def stage_lens_result(artifacts, source_ref: ArtifactRef, source: WorkingImage,
                      binding: VersionBinding, files: LensFiles,
                      *, strength: float) -> StagedLensResult:
    """Stage the complete declared result closure; adoption remains caller-owned.

    Only verified preparation output may be supplied. This boundary validates
    source binding before any staged writes and reuses the source document for
    exact zero-strength bypass. It never publishes a guessed registration.
    """
    if binding.source!=source_ref or binding.canvas_hash!=canonical_hash(source.canvas):
        raise ValueError('LENS_SOURCE_MISMATCH')
    if type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('LENS_PARAMS_INVALID')
    if source.dtype!='float32':
        raise ValueError('LENS_IMAGE_INVALID')
    files.reservation.validate()
    outputs=[]
    written=0
    def stage(path,role):
        nonlocal written
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref)
        written+=path.stat().st_size
        return ref
    def contract(value,role):
        nonlocal written
        ref=artifacts.stage_contract(value,role);outputs.append(ref);written+=len(canonical_bytes(value));return ref
    if strength==0:
        image=source;image_ref=source_ref
    else:
        pixels=stage(files.rgb,'pixels');alpha=stage(files.alpha,'alpha')
        field=stage(files.inverse_field,'warp_field')
        image=lens_working_image(source,pixels,alpha,field,strength=strength)
        image_ref=contract(image,'working_image')
    changed_pixels=stage(files.changed_mask,'mask_pixels')
    changed=MaskRef(artifact=changed_pixels,canvas=image.canvas,
        transform_chain=image.transform_chain,object_id=None,selector='changed',
        confidence=1.,model_lock_id=None,protected=False)
    changed_ref=contract(changed,'mask')
    review=strength!=0 and files.uncovered
    report=ImageReport(schema_version=1,status='review' if review else 'pass',
        operation='optical.lens',skipped_count=0,
        reason='uncovered_area' if review else None,outside_mask_max_error=0.,
        clipping_fraction=0.,identity=None,transform=None,sources=[binding],registrations=[])
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,
        report=report_ref,changed_mask=changed_ref,generated_mask=None),'document')
    return StagedLensResult(document,tuple(outputs),review,written)
