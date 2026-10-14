from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.depth import DepthParams
from photo_worker.contracts.images import RelativeDepth,WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _required_depth_ram(height:int,width:int,has_alpha:bool)->int:
    """Reserve color conversion, model result and resized depth/invalid maps."""
    if (type(height) is not int or type(width) is not int or type(has_alpha) is not bool
            or height<1 or width<1):raise ValueError('DEPTH_SIZE_INVALID')
    return 512*1024**2+height*width*(128+(24 if has_alpha else 0))


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str,verify_runtime=None)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    pinned=any(key in profile for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha'))
    if pinned and not callable(verify_runtime):raise OperationFailure('DEPTH_RUNTIME_INSTALLATION_REQUIRED')
    if verify_runtime is not None:
        if not callable(verify_runtime) or verify_runtime(ctx) is not True:raise OperationFailure('DEPTH_RUNTIME_UNQUALIFIED')
    try:
        p=artifacts.params(DepthParams);image=artifacts.contract(p.image,WorkingImage)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding!='gamma24' or image.canvas.width*image.canvas.height>200000000):raise ValueError('DEPTH_IMAGE_INVALID')
        required=[image.pixels,*([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('DEPTH_INPUT_UNDECLARED')
        if ram<_required_depth_ram(image.canvas.height,image.canvas.width,image.alpha is not None):raise OperationFailure('RESOURCE_OOM')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));srgb,_=work_to_model(rgb)
            with pool.acquire(p.model_id,backend) as engine:raw=engine.infer(srgb)
        from .prepare import normalize_depth
        depth,flat=normalize_depth(raw,w,h)
        invalid=None
        if image.alpha is not None:
            with artifacts.input(image.alpha,suffix='.npy') as local,read_array(local.path,(h,w),'uint16',ctx.check_active) as alpha:
                missing=alpha==0
                if np.any(missing):invalid=missing.astype(np.float32);depth[missing]=0
        with artifacts.scratch.reserve(h*w*(8 if invalid is not None else 4)+16384) as reservation:
            path=reservation.path('relative-depth','.npy');np.save(path,depth,allow_pickle=False);ipath=None
            if invalid is not None:ipath=reservation.path('depth-invalid','.npy');np.save(ipath,invalid,allow_pickle=False)
            reservation.validate();ctx.check_active();dref=artifacts.stage(path,reservation,'relative_depth','application/octet-stream');ctx.check_active();iref=artifacts.stage(ipath,reservation,'depth_invalid','application/octet-stream') if ipath else None;written=path.stat().st_size+(ipath.stat().st_size if ipath else 0)
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    result=RelativeDepth(artifact=dref,float32_shape=[h,w],invalid_mask=iref,canvas=image.canvas,transform_chain=image.transform_chain,normalization='minmax_valid');ctx.check_active();ref=artifacts.stage_contract(result,'relative_depth_ref');return OperationResult(outputs=[dref,*([iref] if iref else []),ref],review_required=flat,bytes_written=written+len(canonical_bytes(result)))


def depth_operations(config:ArtifactConfig,pool:ModelPool,backend:str,verify_runtime=None):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'relative_depth','depth_invalid','relative_depth_ref'})) as a:return execute(ctx,a,pool,backend,verify_runtime)
    return [Operation('depth.predict','gpu',run)]
