"""Worker process entrypoint; queue and registrations are provisioned externally."""
from __future__ import annotations
from dataclasses import dataclass,field
import os
from pathlib import Path
import signal
import shutil
from threading import Event,Thread,current_thread,main_thread
from typing import Mapping,TYPE_CHECKING
if TYPE_CHECKING:
    from photo_worker.operations.optical.registry import LensRegistry
from uuid import UUID
from urllib.parse import urlsplit
from psycopg.conninfo import conninfo_to_dict
from photo_worker.operations.asset.adapters import asset_operations
from photo_worker.operations.pair.register import pair_operations
from photo_worker.operations.pair.fit.body import body_fit_operations
from photo_worker.operations.pair.fit import beauty_fit_operations,defect_fit_operations
from photo_worker.operations.color.stats import stats_operations
from photo_worker.operations.style.resolve import resolve_operations
from photo_worker.operations.style.aggregate import aggregate_operations
from photo_worker.operations.style import validate_operations,package_operations
from photo_worker.operations.style.import_style import import_operations
from photo_worker.operations.defects.propose import defect_propose_operations
from photo_worker.operations.defects.repair import defect_repair_operations
from photo_worker.operations.portrait.beauty import beauty_operations
from photo_worker.operations.portrait import detail_operations
from photo_worker.operations.alpha import refine_operations
from photo_worker.operations.geometry import body_operations,crop_operations
from photo_worker.operations.composition import composition_operations,structure_operations
from photo_worker.operations.derivative import resize_operations,sharpen_operations,watermark_operations
from photo_worker.operations.metadata import metadata_operations
from photo_worker.operations.report import contact_sheet_operations
from photo_worker.operations.quality import quality_operations
from photo_worker.operations.collection import group_operations,cull_operations
from photo_worker.operations.session.adapter import session_plan_operations
from photo_worker.operations.learning.fit import learning_fit_operations
from photo_worker.operations.series import measure_operations,balance_operations
from photo_worker.operations.mask.transform import mask_transform_operations
from photo_worker.operations.mask.rasterize import mask_operations
from photo_worker.operations.mask.face_skin import face_skin_operations
from photo_worker.operations.image.local import image_local_operations
from photo_worker.operations.image.color import image_color_operations
from photo_worker.operations.background import background_operations,blur_operations,sky_operations
from photo_worker.operations.hdr import hdr_operations,register_operations,merge_operations
from photo_worker.operations.sensor import dust_repair_operations, sensor_operations
from photo_worker.operations.archive import archive_operations
from photo_worker.operations.export import export_operations
from photo_worker.operations.media import media_operations
from photo_worker.operations.editor import preview_operations
from .artifacts import ArtifactConfig
from .database import TaskDatabase
from .capacity import CapacityReporter,measure_cpu,measure_accelerator
from .capacity_policy import CapacityPolicy,load_capacity_policy
from .executor import Executor,OperationFailure
from .gateway import client_tls
from .transport import Transport

class WorkerConfigurationError(ValueError):pass

@dataclass(frozen=True)
class WorkerConfig:
    database_url:str=field(repr=False)
    amqp_url:str=field(repr=False)
    worker_id:UUID
    resource_class:str
    scratch_root:Path
    gateway_origin:str
    ca:Path
    certificate:Path
    private_key:Path=field(repr=False)
    max_scratch_bytes:int
    max_download_bytes:int
    max_upload_bytes:int
    development:bool=False
    icc_root:Path|None=None
    backend:str='cpu'
    model_root:Path|None=None
    model_lock:Path|None=None
    capacity_policy:CapacityPolicy|None=None
    lens_registry:LensRegistry|None=None
    layer_runtime_lock:object|None=None
    layer_runtime_sha:str|None=None
    person_runtime_locks:tuple[str,str,str]|None=None
    depth_runtime_locks:tuple[str,str,str]|None=None
    series_runtime_locks:tuple[str,str,str]|None=None
    delivery_enabled:bool=False
    interchange_enabled:bool=False
    identity_runtime_locks:tuple[str,str,str]|None=None

    @classmethod
    def from_env(cls,env:Mapping[str,str]|None=None):
        env=os.environ if env is None else env
        def required(name):
            value=env.get(name,'')
            if not value:raise WorkerConfigurationError(name+' required')
            return value
        def limit(name):
            raw=required(name)
            if len(raw)>16 or not raw.isascii() or not raw.isdigit():raise WorkerConfigurationError(name+' must be byte count')
            value=int(raw)
            if not 1<=value<=2**53-1:raise WorkerConfigurationError(name+' out of bounds')
            return value
        environment=env.get('APP_ENV','development')
        if environment not in ('development','production'):raise WorkerConfigurationError('Invalid APP_ENV')
        try:worker=UUID(required('PHOTO_WORKER_ID'))
        except ValueError:raise WorkerConfigurationError('Invalid PHOTO_WORKER_ID') from None
        if worker.int==0:raise WorkerConfigurationError('Invalid PHOTO_WORKER_ID')
        resource_class=required('PHOTO_WORKER_CLASS')
        if resource_class not in ('media','cpu','gpu','archive'):raise WorkerConfigurationError('No executable operations installed for worker class')
        backend=required('PHOTO_WORKER_BACKEND') if resource_class=='gpu' else env.get('PHOTO_WORKER_BACKEND','cpu')
        if backend not in ('cpu','mps','cuda') or resource_class!='gpu' and backend!='cpu':raise WorkerConfigurationError('Invalid PHOTO_WORKER_BACKEND')
        dsn=required('DATABASE_URL')
        try:conninfo_to_dict(dsn)
        except Exception:raise WorkerConfigurationError('Invalid DATABASE_URL') from None
        root=Path(required('PHOTO_SCRATCH_ROOT')).absolute()
        if not root.is_dir() or root.resolve()!=root:raise WorkerConfigurationError('Scratch root must be an existing directory without symlinks')
        origin=required('GATEWAY_ORIGIN')
        try:
            url=urlsplit(origin)
            if url.scheme!='https' or not url.hostname or url.username is not None or url.password is not None or url.path or url.query or url.fragment or (url.port is not None and url.port<1):
                raise ValueError()
        except ValueError:raise WorkerConfigurationError('Invalid GATEWAY_ORIGIN') from None
        icc_root=Path(env['PHOTO_ICC_ROOT']).absolute() if env.get('PHOTO_ICC_ROOT') else None
        if icc_root is not None:
            from photo_worker.color.icc import ICCRegistry
            try:
                if icc_root.resolve(strict=True)!=icc_root:raise ValueError()
                registry=ICCRegistry(icc_root)
                for profile in ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'):registry.resolve(profile)
            except (OSError,ValueError,KeyError,TypeError):raise WorkerConfigurationError('Invalid PHOTO_ICC_ROOT') from None
        model_root=Path(env['PHOTO_MODEL_ROOT']).absolute() if env.get('PHOTO_MODEL_ROOT') else None
        model_lock=Path(env['PHOTO_MODEL_LOCK']).absolute() if env.get('PHOTO_MODEL_LOCK') else None
        if (model_root is None)!=(model_lock is None):raise WorkerConfigurationError('PHOTO_MODEL_ROOT and PHOTO_MODEL_LOCK must be set together')
        if model_root is not None:
            try:
                if model_root.resolve(strict=True)!=model_root or not model_root.is_dir():raise ValueError()
                if model_lock.resolve(strict=True)!=model_lock or not model_lock.is_file() or model_lock.stat().st_size>8388608:raise ValueError()
                from photo_worker.models.lock import ModelLock
                ModelLock.model_validate_json(model_lock.read_bytes())
            except (OSError,ValueError):raise WorkerConfigurationError('Invalid PHOTO_MODEL configuration') from None
        policy=None
        if env.get('PHOTO_CAPACITY_POLICY'):
            try:policy=load_capacity_policy(Path(env['PHOTO_CAPACITY_POLICY']).absolute())
            except (OSError,ValueError):raise WorkerConfigurationError('Invalid PHOTO_CAPACITY_POLICY') from None
        if backend=='mps' and policy is None:
            raise WorkerConfigurationError('PHOTO_CAPACITY_POLICY required for MPS')
        lens_registry=None
        registry_path=env.get('PHOTO_LENS_REGISTRY','')
        registry_sha=env.get('PHOTO_LENS_REGISTRY_SHA256','')
        if bool(registry_path)!=bool(registry_sha):
            raise WorkerConfigurationError('PHOTO_LENS_REGISTRY and PHOTO_LENS_REGISTRY_SHA256 must be set together')
        if registry_path:
            if resource_class!='cpu':raise WorkerConfigurationError('Lens registry requires CPU worker')
            try:
                path=Path(registry_path).absolute()
                if path.resolve(strict=True)!=path:raise ValueError()
                from photo_worker.operations.optical.registry import load_lens_registry
                lens_registry=load_lens_registry(path,registry_sha)
            except (OSError,ValueError,OperationFailure):
                raise WorkerConfigurationError('Invalid PHOTO_LENS_REGISTRY') from None
        layer_lock=None
        layer_sha=env.get('PHOTO_LAYER_RUNTIME_SHA256') or None
        layer_path=env.get('PHOTO_LAYER_RUNTIME_LOCK') or None
        if bool(layer_sha)!=bool(layer_path):
            raise WorkerConfigurationError('Layer runtime lock and SHA256 must be set together')
        if layer_path:
            if resource_class!='cpu':raise WorkerConfigurationError('Layer runtime requires CPU worker')
            try:
                from photo_worker.operations.layers.runtime_lock import load_layer_runtime_lock
                path=Path(layer_path).absolute()
                if path.resolve(strict=True)!=path or path.stat().st_size>65536:raise ValueError()
                layer_lock=load_layer_runtime_lock(path,layer_sha)
            except (OSError,ValueError,OperationFailure):
                raise WorkerConfigurationError('Invalid PHOTO_LAYER_RUNTIME_LOCK') from None
        from .person_config import person_runtime_locks_from_env
        person_locks=person_runtime_locks_from_env(env,resource_class,backend,model_root is not None)
        from .depth_config import depth_runtime_locks_from_env
        depth_locks=depth_runtime_locks_from_env(env,resource_class,backend,model_root is not None)
        from .series_config import series_runtime_locks_from_env
        series_locks=series_runtime_locks_from_env(env,resource_class,backend)
        from .identity_config import identity_runtime_locks_from_env
        identity_locks=identity_runtime_locks_from_env(env,resource_class,backend,model_root is not None)
        from photo_worker.operations.delivery.installation import delivery_enabled_from_env
        from photo_worker.operations.interchange.installation import interchange_enabled_from_env
        try:
            delivery_enabled=delivery_enabled_from_env(env,resource_class)
            interchange_enabled=interchange_enabled_from_env(env,resource_class)
        except ValueError as error:
            raise WorkerConfigurationError(str(error)) from None
        return cls(dsn,required('AMQP_URL'),worker,resource_class,root,origin,
            Path(required('GATEWAY_CA_FILE')),Path(required('WORKER_CERT_FILE')),Path(required('WORKER_KEY_FILE')),
            limit('WORKER_MAX_SCRATCH_BYTES'),limit('WORKER_MAX_DOWNLOAD_BYTES'),limit('WORKER_MAX_UPLOAD_BYTES'),environment=='development',icc_root,backend,model_root,model_lock,policy,lens_registry,layer_lock,layer_sha,person_locks,depth_locks,series_locks,delivery_enabled,interchange_enabled,identity_locks)


def build_worker(config:WorkerConfig, *, render_renderer=None, region_renderer=None, semantic_installation_factory=None, semantic_artifact_installation_factory=None, semantic_assembly_installation_factory=None, local_exposure_installation_factory=None, glare_admission_factory=None, glare_installation=None, reflection_admission_factory=None, reflection_installation=None, donor_installation=None, negative_installation=None, relight_installation=None, local_portrait_installation=None, perspective_installation=None, perspective_lines_installation=None, multi_installations=(), delivery_installation=None, interchange_installation=None, interchange_import_installation=None, interchange_export_installation=None, learning_evaluation_installation=None)->tuple[TaskDatabase,Transport]:
    if learning_evaluation_installation is not None:
        from photo_worker.operations.learning.installation import LearningEvaluationInstallation
        from .learning_config import SignedLearningRequest
        if (not isinstance(learning_evaluation_installation,
                           (LearningEvaluationInstallation, SignedLearningRequest))
                or config.resource_class != 'gpu' or config.backend != 'cuda'):
            raise WorkerConfigurationError('Learning evaluation requires explicit CUDA GPU installation')
        if (isinstance(learning_evaluation_installation,
                       LearningEvaluationInstallation)
                and not config.development):
            raise WorkerConfigurationError(
                'Production learning evaluation requires signed qualification')

    if (config.interchange_enabled or interchange_installation is not None or interchange_import_installation is not None or interchange_export_installation is not None) and (config.resource_class!='cpu' or config.backend!='cpu'):
        raise WorkerConfigurationError('Interchange runtime requires CPU worker')
    from photo_worker.operations.interchange.installation import resolve_interchange_export_installation
    try:
        interchange_export_installation=resolve_interchange_export_installation(interchange_export_installation)
    except ValueError as error:
        raise WorkerConfigurationError(str(error)) from None
    if (config.delivery_enabled or delivery_installation is not None) and config.resource_class!='cpu':
        raise WorkerConfigurationError('Delivery runtime requires CPU worker')
    if render_renderer is not None and (config.resource_class!='gpu'
            or getattr(render_renderer,'backend',None)!=config.backend):
        raise WorkerConfigurationError('Render runtime must match GPU worker backend')
    if region_renderer is not None and (config.resource_class!='gpu' or config.backend!='cuda'
            or getattr(region_renderer,'backend',None)!='cuda'):
        raise WorkerConfigurationError('Region runtime requires matching CUDA GPU worker')
    if semantic_installation_factory is not None and semantic_artifact_installation_factory is not None:
        raise WorkerConfigurationError('Semantic installation owners are ambiguous')
    semantic_owner=semantic_installation_factory if semantic_installation_factory is not None else semantic_artifact_installation_factory
    if semantic_owner is not None and (not callable(semantic_owner)
            or config.resource_class!='gpu' or config.backend!='cuda'):
        raise WorkerConfigurationError('Semantic installation requires matching CUDA GPU worker')
    if semantic_assembly_installation_factory is not None and (not callable(semantic_assembly_installation_factory) or config.resource_class!='cpu' or config.backend!='cpu'):
        raise WorkerConfigurationError('Semantic assembly installation requires CPU worker')
    if local_exposure_installation_factory is not None and (not callable(local_exposure_installation_factory)
            or config.resource_class!='cpu' or config.backend!='cpu'):
        raise WorkerConfigurationError('Local exposure installation requires CPU worker')
    if reflection_installation is not None and reflection_admission_factory is not None:
        raise WorkerConfigurationError('Ambiguous reflection installation')
    if glare_installation is not None and glare_admission_factory is not None:
        raise WorkerConfigurationError('Ambiguous glare installation')
    if glare_admission_factory is not None or glare_installation is not None or reflection_admission_factory is not None or reflection_installation is not None:
        if (config.resource_class!='gpu' or config.backend not in ('cpu','cuda')
                or glare_admission_factory is not None and not callable(glare_admission_factory)
                or reflection_admission_factory is not None and not callable(reflection_admission_factory)):
            raise WorkerConfigurationError('V28 installation requires CPU or CUDA GPU-class worker and callable factories')
    if config.depth_runtime_locks is not None and (config.resource_class!='gpu' or config.model_root is None or config.model_lock is None):raise WorkerConfigurationError('Depth runtime requires GPU-class model worker')
    if config.person_runtime_locks is not None and (config.resource_class!='gpu' or config.model_root is None or config.model_lock is None):raise WorkerConfigurationError('Person runtime requires GPU-class model worker')
    if config.identity_runtime_locks is not None and (config.resource_class!='gpu' or config.model_root is None or config.model_lock is None):raise WorkerConfigurationError('Identity runtime requires GPU-class model worker')
    if donor_installation is not None and (config.resource_class!='cpu' or config.backend!='cpu'):
        raise WorkerConfigurationError('Donor installation requires CPU worker')
    if perspective_lines_installation is not None:
        from photo_worker.operations.optical.perspective_lines_installation import PerspectiveLinesInstallation
        if (not isinstance(perspective_lines_installation, PerspectiveLinesInstallation)
                or config.resource_class!='cpu' or config.backend!='cpu' or config.icc_root is None):
            raise WorkerConfigurationError('Perspective lines installation requires CPU worker and ICC registry')
        from photo_worker.color.icc import ICCRegistry
        if ICCRegistry(config.icc_root).resolve('linear_srgb_v1').sha256 != perspective_lines_installation.working_icc_sha:
            raise WorkerConfigurationError('Perspective lines working ICC mismatch')
    from photo_worker.operations.optical.multi_installation import MultiImageInstallation,MultiImageRuntimeOptions
    if not isinstance(multi_installations,(list,tuple)):
        raise WorkerConfigurationError('Invalid multi-image installations')
    installed_multi=set()
    for installation in multi_installations:
        if (not isinstance(installation,MultiImageInstallation) or not isinstance(installation.options,MultiImageRuntimeOptions)
                or installation.options.operation not in ('optical.panorama','optical.focus_stack')
                or installation.options.operation in installed_multi or config.resource_class!='cpu'
                or config.backend!='cpu' or config.icc_root is None):
            raise WorkerConfigurationError('Multi-image installation requires unique operations, CPU and ICC registry')
        installed_multi.add(installation.options.operation)
    if perspective_installation is not None:
        from photo_worker.operations.optical.perspective_installation import PerspectiveInstallation
        if (not isinstance(perspective_installation,PerspectiveInstallation)
                or config.resource_class!='cpu' or config.backend!='cpu' or config.icc_root is None):
            raise WorkerConfigurationError('Perspective installation requires CPU worker and ICC registry')
    if negative_installation is not None:
        from photo_worker.operations.optical.negative_adapter import NegativeInstallation
        if (not isinstance(negative_installation,NegativeInstallation)
                or config.resource_class!='cpu' or config.backend!='cpu' or config.icc_root is None):
            raise WorkerConfigurationError('Negative installation requires CPU worker and ICC registry')
    if relight_installation is not None:
        from .relight_config import RelightInstallation
        from photo_worker.operations.portrait.relight_runtime_lock import verify_installed_runtime
        if (not isinstance(relight_installation,RelightInstallation)
                or config.resource_class!='cpu' or config.backend!='cpu'
                or not isinstance(relight_installation.bucket,str) or not relight_installation.bucket
                or not callable(getattr(relight_installation.objects,'get_object',None))):
            raise WorkerConfigurationError('Invalid relight installation or CPU worker')
        verify_installed_runtime(relight_installation.runtime_lock,relight_installation.runtime_sha)
    if local_portrait_installation is not None:
        from .local_portrait_config import LocalPortraitInstallation
        from photo_worker.operations.portrait.local_runtime_lock import verify_installed_runtime
        if (not isinstance(local_portrait_installation,LocalPortraitInstallation)
                or config.resource_class!='cpu' or config.backend!='cpu'):
            raise WorkerConfigurationError('Local portrait requires explicit CPU installation')
        verify_installed_runtime(local_portrait_installation.runtime_lock,
            local_portrait_installation.runtime_sha)
    if reflection_installation is not None:
        from .reflection_config import ReflectionInstallation
        from photo_worker.operations.image.reflection_runtime_lock import verify_installed_runtime
        if not isinstance(reflection_installation, ReflectionInstallation):
            raise WorkerConfigurationError('Invalid reflection installation')
        try:
            verify_installed_runtime(reflection_installation.runtime_lock,
                                     reflection_installation.runtime_sha, config.backend)
        except (OSError, ValueError):
            raise WorkerConfigurationError('Unqualified reflection runtime') from None
    if glare_installation is not None:
        from .glare_config import GlareDonorInstallation
        from photo_worker.operations.portrait.glare_donor_runtime_lock import verify_installed_runtime
        if (not isinstance(glare_installation, GlareDonorInstallation)
                or config.backend != 'cpu'):
            raise WorkerConfigurationError('Invalid donor glare installation')
        try:
            verify_installed_runtime(glare_installation.runtime_lock,
                                     glare_installation.runtime_sha)
        except (OSError, ValueError):
            raise WorkerConfigurationError('Unqualified donor glare runtime') from None
    model_pools=[]
    gpu_model_pool=None
    tls=client_tls(config.ca,config.certificate,config.private_key)
    artifacts=ArtifactConfig(config.scratch_root,config.gateway_origin,tls,config.max_scratch_bytes,config.max_download_bytes,config.max_upload_bytes)
    if config.resource_class=='gpu':
        from photo_worker.operations.pair.fit.color import color_fit_operations
        operations=color_fit_operations(artifacts)
        if config.model_root is not None and config.model_lock is not None:
            from photo_worker.models.adapters import factories
            from photo_worker.models.lock import ModelLock
            from photo_worker.models.pool import ModelPool
            from photo_worker.operations.image.denoise import denoise_operations
            from photo_worker.operations.image.upscale import upscale_operations
            from photo_worker.operations.image.inpaint import inpaint_operations
            from photo_worker.operations.image.generate import generation_operations
            from photo_worker.operations.image.outpaint import outpaint_operations
            from photo_worker.operations.mask.person import person_operations
            from photo_worker.operations.pose.detect import pose_operations
            from photo_worker.operations.face.detect import face_detect_operations
            from photo_worker.operations.face.landmarks import landmark_operations
            from photo_worker.operations.face.parse import face_parse_operations
            from photo_worker.operations.features import scene_operations
            from photo_worker.operations.depth import depth_operations
            from photo_worker.operations.scene import scene_parse_operations
            from photo_worker.operations.session.describe import REQUIRED_MODELS,session_describe_operations
            from photo_worker.operations.identity.adapter import REQUIRED_MODELS as IDENTITY_MODELS,identity_compare_operations
            from photo_worker.operations.capture.adapter import capture_assess_operations
            from photo_worker.operations.capture.prepare_adapter import capture_prepare_operations
            from photo_worker.operations.mask.prompt import prompt_operations
            lock=ModelLock.model_validate_json(config.model_lock.read_bytes());pool=ModelPool(config.model_root,lock,factories())
            model_pools.append(pool)
            gpu_model_pool=pool
            ids={entry.id for entry in lock.models}
            operations+=capture_assess_operations(artifacts,pool,config.backend)
            operations+=capture_prepare_operations(artifacts,pool,config.backend)
            if config.person_runtime_locks is not None and 'lraspp_mobilenet_v3_large' not in ids:raise WorkerConfigurationError('Person runtime requires installed person model')
            if 'lraspp_mobilenet_v3_large' in ids:
                from .person_config import configured_person_runtime
                operations+=person_operations(artifacts,pool,config.backend,configured_person_runtime(config,pool))
            if 'keypointrcnn_resnet50_fpn' in ids:operations+=pose_operations(artifacts,pool,config.backend)
            if 'sfd' in ids:operations+=face_detect_operations(artifacts,pool,config.backend)
            if 'fan2d' in ids:operations+=landmark_operations(artifacts,pool,config.backend)
            if 'bisenet_face19' in ids:operations+=face_parse_operations(artifacts,pool,config.backend)
            if 'dinov2_vits14' in ids:operations+=scene_operations(artifacts,pool,config.backend)
            if REQUIRED_MODELS<=ids:operations+=session_describe_operations(artifacts,pool,config.backend)
            if config.identity_runtime_locks is not None:
                if not IDENTITY_MODELS<=ids:raise WorkerConfigurationError('Identity runtime requires installed identity models')
                from .identity_config import configured_identity_runtime
                operations+=identity_compare_operations(artifacts,pool,config.backend,configured_identity_runtime(config,pool))
            if config.depth_runtime_locks is not None and 'depth_anything_v2_small' not in ids:raise WorkerConfigurationError('Depth runtime requires installed depth model')
            if 'depth_anything_v2_small' in ids:
                from .depth_config import configured_depth_runtime
                operations+=depth_operations(artifacts,pool,config.backend,configured_depth_runtime(config,pool))
            if 'segformer_b0_ade' in ids:operations+=scene_parse_operations(artifacts,pool,config.backend)
            if 'mobilesam' in ids:operations+=prompt_operations(artifacts,pool,config.backend)
            if 'scunet_color_real_psnr' in ids:operations+=denoise_operations(artifacts,pool,config.backend)
            if ids&{'realesrgan_x2','realesrgan_x4'}:operations+=upscale_operations(artifacts,pool,config.backend)
            if 'big_lama' in ids:operations+=inpaint_operations(artifacts,pool,config.backend)
            if 'sd15_inpaint' in ids:operations+=generation_operations(artifacts,pool,config.backend)+outpaint_operations(artifacts,pool,config.backend)
        # Registration permits only one backend; reject misrouted profiles even
        # before loading task artifacts. No runtime backend substitution.
        def bound(operation):
            def execute(ctx):
                from .executor import OperationFailure
                if ctx.lease.execution_profile.get('backend')!=config.backend:raise OperationFailure('CONFIG_INVALID')
                return operation.execute(ctx)
            from .executor import Operation
            return Operation(operation.name,operation.resource_class,execute,operation.version)
        operations=[bound(op) for op in operations]
    elif config.resource_class=='archive':
        operations=archive_operations(artifacts)
    else:
        operations=[op for op in [*preview_operations(artifacts,config.icc_root),*asset_operations(artifacts,config.icc_root),*pair_operations(artifacts),*body_fit_operations(artifacts),*beauty_fit_operations(artifacts),*defect_fit_operations(artifacts),*body_operations(artifacts),*structure_operations(artifacts),*composition_operations(artifacts),*crop_operations(artifacts),*stats_operations(artifacts),*resolve_operations(artifacts),*aggregate_operations(artifacts),*validate_operations(artifacts),*package_operations(artifacts),*import_operations(artifacts),*group_operations(artifacts),*session_plan_operations(artifacts),*learning_fit_operations(artifacts),*cull_operations(artifacts),*measure_operations(artifacts),*balance_operations(artifacts,config.series_runtime_locks),*image_color_operations(artifacts),*image_local_operations(artifacts),*resize_operations(artifacts),*sharpen_operations(artifacts),*watermark_operations(artifacts),*metadata_operations(artifacts),*contact_sheet_operations(artifacts),*quality_operations(artifacts),*background_operations(artifacts),*blur_operations(artifacts),*sky_operations(artifacts),*hdr_operations(artifacts),*register_operations(artifacts),*merge_operations(artifacts),*sensor_operations(artifacts),*dust_repair_operations(artifacts),*mask_operations(artifacts),*face_skin_operations(artifacts),*mask_transform_operations(artifacts),*beauty_operations(artifacts),*detail_operations(artifacts),*refine_operations(artifacts),*defect_repair_operations(artifacts),*defect_propose_operations(artifacts),*export_operations(artifacts,config.icc_root),*media_operations(artifacts,config.icc_root)] if op.resource_class==config.resource_class]
        if config.resource_class=='cpu' and config.model_root is not None and config.model_lock is not None:
            from photo_worker.models.adapters import factories
            from photo_worker.models.lock import ModelLock
            from photo_worker.models.pool import ModelPool
            from photo_worker.operations.alpha import alpha_operations
            lock=ModelLock.model_validate_json(config.model_lock.read_bytes());pool=ModelPool(config.model_root,lock,factories())
            model_pools.append(pool)
            if 'u2net' in {entry.id for entry in lock.models}:operations+=alpha_operations(artifacts,pool)
    if config.resource_class=='cpu' and config.lens_registry is not None:
        from photo_worker.operations.optical.adapter import lens_operations
        operations+=lens_operations(artifacts,config.lens_registry)
    if not operations:raise WorkerConfigurationError('No executable operations for resource class')
    database=TaskDatabase(config.database_url)
    if (config.series_runtime_locks is not None and
            database.series_runtime_ready(config.worker_id,*config.series_runtime_locks[1:]) is not True):
        raise WorkerConfigurationError('Series runtime does not match worker registration')
    if glare_installation is not None:
        from photo_worker.models.install import ModelError, verify_model
        from photo_worker.operations.portrait.glare_donor_admission import GlareDonorAdmission
        if (gpu_model_pool is None or
                gpu_model_pool.lock.lock_id != glare_installation.model_lock_sha):
            raise WorkerConfigurationError('Glare model lock mismatch')
        entries=[item for item in gpu_model_pool.lock.models if item.id=='glare_mask']
        if len(entries)!=1:
            raise WorkerConfigurationError('Glare mask model is absent')
        try:
            verify_model(config.model_root/gpu_model_pool.lock.lock_id/'glare_mask',entries[0])
            with gpu_model_pool.acquire('glare_mask',config.backend):
                pass
        except (OSError,ModelError):
            raise WorkerConfigurationError('Glare mask model unavailable') from None
        glare_admission_factory=lambda db,worker_config: GlareDonorAdmission(
            db,glare_installation.objects,glare_installation.bucket,
            runtime_sha=glare_installation.runtime_sha,
            model_lock_sha=glare_installation.model_lock_sha,
            material_scratch_root=glare_installation.material_scratch_root,
            material_max_bytes=glare_installation.material_max_bytes,
            alpha_mode=glare_installation.alpha_mode)
    if reflection_installation is not None:
        from photo_worker.models.install import ModelError, verify_model
        from photo_worker.operations.image.reflection_admission import ReflectionModelAdmission
        if (gpu_model_pool is None or
                gpu_model_pool.lock.lock_id != reflection_installation.model_lock_sha):
            raise WorkerConfigurationError('Reflection model lock mismatch')
        entries=[item for item in gpu_model_pool.lock.models if item.id=='reflection_rgb']
        if len(entries)!=1:
            raise WorkerConfigurationError('Reflection model is absent')
        try:
            verify_model(config.model_root/gpu_model_pool.lock.lock_id/'reflection_rgb',entries[0])
            with gpu_model_pool.acquire('reflection_rgb', config.backend) as engine:
                if (not callable(getattr(engine, 'infer', None))
                        or type(getattr(engine, 'size', None)) is not int):
                    raise ModelError('MODEL_CONTRACT_ERROR')
        except (OSError,ModelError):
            raise WorkerConfigurationError('Reflection model bytes or tensor contract unavailable') from None
        reflection_admission_factory=lambda db,worker_config: ReflectionModelAdmission(
            db,gpu_model_pool,worker_config.backend,reflection_installation.runtime_sha)
    if local_portrait_installation is not None and database.local_portrait_runtime_ready() is not True:
        raise WorkerConfigurationError('Local portrait SQL authority is not installed')
    if ((glare_admission_factory is not None or reflection_admission_factory is not None)
            and database.restoration_runtime_ready() is not True):
        raise WorkerConfigurationError('Restoration SQL authority is not installed')
    if glare_admission_factory is not None and database.glare_donor_runtime_ready() is not True:
        raise WorkerConfigurationError('Glare donor SQL authority is not installed')
    if glare_admission_factory is not None or reflection_admission_factory is not None:
        from .v28_installation import installed_v28_operations
        operations += installed_v28_operations(artifacts,database,config,
            glare_factory=glare_admission_factory,
            reflection_factory=reflection_admission_factory)
    from photo_worker.operations.delivery.installation import resolve_delivery_installation
    from photo_worker.operations.interchange.installation import resolve_interchange_installation, resolve_interchange_import_installation
    try:
        delivery_installation=resolve_delivery_installation(database,config.delivery_enabled,delivery_installation)
        interchange_installation=resolve_interchange_installation(database,config.interchange_enabled,interchange_installation)
        interchange_import_installation=resolve_interchange_import_installation(interchange_import_installation)
    except ValueError as error:
        raise WorkerConfigurationError(str(error)) from None
    if learning_evaluation_installation is not None:
        from photo_worker.operations.learning.installation import installed_learning_evaluate_operations
        from .learning_config import SignedLearningRequest, install_signed_learning_evaluation
        if isinstance(learning_evaluation_installation, SignedLearningRequest):
            learning_evaluation_installation = install_signed_learning_evaluation(
                learning_evaluation_installation, config, gpu_model_pool)
        try:
            operations += installed_learning_evaluate_operations(artifacts, learning_evaluation_installation)
        except ValueError as error:
            raise WorkerConfigurationError(str(error)) from None
    if render_renderer is not None:
        from photo_worker.v2.render_task import SQLRenderTaskAdmission,render_task_operations
        operations+=render_task_operations(artifacts,SQLRenderTaskAdmission(database),render_renderer)
    if region_renderer is not None:
        from photo_worker.operations.region.adapter import region_operations
        from photo_worker.operations.region.admission import SQLRegionAdmission
        operations+=region_operations(artifacts,SQLRegionAdmission(database),region_renderer)
    if config.resource_class=='cpu':
        from photo_worker.operations.layers.adapter import layer_operations
        operations+=layer_operations(artifacts,database,config.layer_runtime_lock,config.layer_runtime_sha)
    if semantic_owner is not None:
        from photo_worker.operations.semantic.adapter import semantic_transfer_operations
        operations+=semantic_transfer_operations(artifacts,installation_factory=semantic_installation_factory,artifact_installation_factory=semantic_artifact_installation_factory)
    if semantic_assembly_installation_factory is not None:
        from photo_worker.operations.semantic.assemble_adapter import semantic_assembly_operations
        operations+=semantic_assembly_operations(artifacts,installation_factory=semantic_assembly_installation_factory)
    if local_exposure_installation_factory is not None:
        from photo_worker.operations.semantic.exposure_adapter import local_exposure_operations
        operations+=local_exposure_operations(artifacts,installation_factory=local_exposure_installation_factory)
    if donor_installation is not None:
        from photo_worker.operations.portrait.donor_adapter import DonorInstallation,donor_portrait_operations
        if not isinstance(donor_installation,DonorInstallation):
            raise WorkerConfigurationError('Invalid donor installation')
        operations+=donor_portrait_operations(artifacts,database,donor_installation.objects,
            bucket=donor_installation.bucket,options=donor_installation.options)
    if local_portrait_installation is not None:
        from photo_worker.operations.portrait.local_sql_origin import sql_manual_local_portrait_operations
        operations+=sql_manual_local_portrait_operations(artifacts,database,
            local_portrait_installation.runtime_lock,local_portrait_installation.runtime_sha)
    if perspective_lines_installation is not None:
        from photo_worker.operations.optical.perspective_lines_installation import installed_perspective_line_operations
        operations+=installed_perspective_line_operations(artifacts,perspective_lines_installation)
    if multi_installations:
        from photo_worker.operations.optical.multi_installation import multi_operations
        from photo_worker.operations.optical.perspective_color import perspective_icc_verifier
        from photo_worker.color.icc import ICCRegistry
        verifier=perspective_icc_verifier(ICCRegistry(config.icc_root))
        for installation in multi_installations:
            operations+=multi_operations(artifacts,database,installation,verify_color=verifier)
    if perspective_installation is not None:
        from photo_worker.operations.optical.perspective_installation import perspective_operations
        from photo_worker.operations.optical.perspective_color import perspective_icc_verifier
        from photo_worker.color.icc import ICCRegistry
        operations+=perspective_operations(artifacts,database,perspective_installation,
            verify_color=perspective_icc_verifier(ICCRegistry(config.icc_root)))
    if negative_installation is not None:
        from photo_worker.color.icc import ICCRegistry
        from photo_worker.operations.optical.negative_color import negative_icc_verifier
        from photo_worker.operations.optical.negative_adapter import negative_operations
        operations+=negative_operations(artifacts,database,negative_installation.objects,
            bucket=negative_installation.bucket,options=negative_installation.options,
            verify_color=negative_icc_verifier(ICCRegistry(config.icc_root)))
    if relight_installation is not None:
        from photo_worker.operations.portrait.relight_resolver import qualified_relight_operations
        operations+=qualified_relight_operations(artifacts,database,relight_installation.objects,
            relight_installation.bucket,relight_installation.runtime_lock,relight_installation.runtime_sha)
    if interchange_installation is not None:
        from photo_worker.operations.interchange.parse import interchange_parse_operation
        operations.append(interchange_parse_operation(artifacts,interchange_installation))
    if interchange_import_installation is not None:
        if database.interchange_import_person_mask_ready() is not True:
            raise WorkerConfigurationError('Import person-mask SQL authority is not installed')
        from photo_worker.operations.interchange.import_adapter import interchange_import_operation
        operations.append(interchange_import_operation(artifacts,interchange_import_installation))
    if interchange_export_installation is not None:
        from photo_worker.operations.interchange.export_adapter import interchange_export_operation
        operations.append(interchange_export_operation(artifacts,interchange_export_installation))
    if delivery_installation is not None:
        from photo_worker.operations.delivery.adapter import delivery_operations
        operations+=delivery_operations(artifacts,delivery_installation)
    executor=Executor(database,config.worker_id,operations)
    owned_model_pools=tuple(model_pools)
    executor.model_residency_snapshot=lambda:tuple(item for pool in owned_model_pools for item in pool.residency_snapshot())
    return database,Transport(config.amqp_url,config.resource_class,executor,development=config.development)


def _with_policy_thread_budget(function):
    from functools import wraps
    @wraps(function)
    def bounded(config, *args, **kwargs):
        if current_thread() is not main_thread():
            raise WorkerConfigurationError('Worker must run on process main thread')
        from .thread_budget import worker_thread_budget
        from contextlib import nullcontext
        policy=config.capacity_policy
        scope=worker_thread_budget(policy.cpu_threads_per_task) if isinstance(policy,CapacityPolicy) else nullcontext()
        with scope:
            return function(config,*args,**kwargs)
    return bounded


@_with_policy_thread_budget
def run_worker(config:WorkerConfig, *, render_renderer=None, region_renderer=None, semantic_installation_factory=None, semantic_artifact_installation_factory=None, semantic_assembly_installation_factory=None, local_exposure_installation_factory=None, glare_admission_factory=None, glare_installation=None, reflection_admission_factory=None, reflection_installation=None, donor_installation=None, negative_installation=None, relight_installation=None, local_portrait_installation=None, perspective_installation=None, perspective_lines_installation=None, multi_installations=(), delivery_installation=None, interchange_installation=None, interchange_import_installation=None, interchange_export_installation=None, learning_evaluation_installation=None):
    if current_thread() is not main_thread():raise WorkerConfigurationError('Worker must run on process main thread')
    metal_budget = _install_metal_budget(config)
    installed_renderers={}
    if learning_evaluation_installation is not None:installed_renderers['learning_evaluation_installation']=learning_evaluation_installation
    if render_renderer is not None:installed_renderers['render_renderer']=render_renderer
    if region_renderer is not None:installed_renderers['region_renderer']=region_renderer
    if semantic_installation_factory is not None:installed_renderers['semantic_installation_factory']=semantic_installation_factory
    if semantic_artifact_installation_factory is not None:installed_renderers['semantic_artifact_installation_factory']=semantic_artifact_installation_factory
    if semantic_assembly_installation_factory is not None:installed_renderers['semantic_assembly_installation_factory']=semantic_assembly_installation_factory
    if local_exposure_installation_factory is not None:installed_renderers['local_exposure_installation_factory']=local_exposure_installation_factory
    if glare_admission_factory is not None:installed_renderers['glare_admission_factory']=glare_admission_factory
    if glare_installation is not None:installed_renderers['glare_installation']=glare_installation
    if reflection_admission_factory is not None:installed_renderers['reflection_admission_factory']=reflection_admission_factory
    if reflection_installation is not None:installed_renderers['reflection_installation']=reflection_installation
    if donor_installation is not None:installed_renderers['donor_installation']=donor_installation
    if perspective_installation is not None:installed_renderers['perspective_installation']=perspective_installation
    if perspective_lines_installation is not None:installed_renderers['perspective_lines_installation']=perspective_lines_installation
    if negative_installation is not None:installed_renderers['negative_installation']=negative_installation
    if relight_installation is not None:installed_renderers['relight_installation']=relight_installation
    if local_portrait_installation is not None:installed_renderers['local_portrait_installation']=local_portrait_installation
    if multi_installations:installed_renderers['multi_installations']=multi_installations
    if delivery_installation is not None:installed_renderers['delivery_installation']=delivery_installation
    if interchange_installation is not None:installed_renderers['interchange_installation']=interchange_installation
    if interchange_import_installation is not None:installed_renderers['interchange_import_installation']=interchange_import_installation
    if interchange_export_installation is not None:installed_renderers['interchange_export_installation']=interchange_export_installation
    database,transport=build_worker(config,**installed_renderers)
    run_built_worker(config,database,transport,metal_budget=metal_budget)


def run_built_worker(config, database, transport, *, metal_budget=None):
    if current_thread() is not main_thread():
        raise WorkerConfigurationError('Worker must run on process main thread')
    from .thread_budget import worker_thread_budget
    from contextlib import nullcontext
    policy = config.capacity_policy
    scope = worker_thread_budget(policy.cpu_threads_per_task) if isinstance(policy, CapacityPolicy) else nullcontext()
    with scope:
        return _run_built_worker(config, database, transport, metal_budget=metal_budget)


def _run_built_worker(config, database, transport, *, metal_budget=None):
    """Shared capacity, signal and fenced shutdown lifecycle for a built worker."""
    if current_thread() is not main_thread():
        raise WorkerConfigurationError('Worker must run on process main thread')
    if isinstance(config.capacity_policy, CapacityPolicy):
        # Executor uses this cadence for lease renewal; set before consuming.
        transport.executor.heartbeat_seconds = config.capacity_policy.renew_seconds
    metal_budget = _ready_metal_budget(config, metal_budget)
    database.ping()
    if isinstance(config.capacity_policy, CapacityPolicy):
        ready = getattr(database, 'capacity_policy_timing_ready', None)
        policy = config.capacity_policy
        if not callable(ready) or ready(config.worker_id, policy.sha256,
                                       policy.offline_seconds, policy.lease_seconds) is not True:
            raise WorkerConfigurationError('Capacity policy timing is not installed for this worker')
    registered=(database.accelerator_operations(config.worker_id,config.backend) if config.resource_class=='gpu'
        else database.runtime_operations(config.worker_id,config.resource_class))
    installed=frozenset(transport.executor.operations)
    if not registered or not registered.issubset(installed):
        raise WorkerConfigurationError('Worker registration does not match executable operations')
    if config.resource_class=='gpu':
        reporter=CapacityReporter(lambda sample:_report_capacity(config,database,installed,sample,getattr(transport.executor,"model_residency_snapshot",None)),
            lambda:_capacity_sample(config,measure_accelerator(config.scratch_root,config.backend),metal_budget=metal_budget),transport.executor.abort_active,
            interval_seconds=config.capacity_policy.heartbeat_seconds if config.capacity_policy else 10)
    else:
        reporter=CapacityReporter(lambda sample:_report_capacity(config,database,installed,sample,getattr(transport.executor,"model_residency_snapshot",None)),
            lambda:_capacity_sample(config,measure_cpu(config.scratch_root)),transport.executor.abort_active,
            interval_seconds=config.capacity_policy.heartbeat_seconds if config.capacity_policy else 10)
    reporter.report_once()
    stop=Event()
    reports=Thread(target=reporter.run,args=(stop,),name='worker-capacity',daemon=True)
    def terminate(signum,frame):
        stop.set()
        transport.executor.abort_active()
    previous={sig:signal.getsignal(sig) for sig in (signal.SIGINT,signal.SIGTERM)}
    try:
        for sig in previous:signal.signal(sig,terminate)
        reports.start()
        transport.run(stop)
    finally:
        stop.set()
        try:
            transport.executor.abort_active()
        finally:
            try:
                if reports.ident is not None:reports.join(timeout=15)
            finally:
                for sig,handler in previous.items():signal.signal(sig,handler)
    if reports.is_alive() or reporter.failed.is_set():raise RuntimeError('Worker capacity reporting stopped')


def _report_capacity(config,database,operations,sample,residency_snapshot=None):
    if isinstance(config.capacity_policy,CapacityPolicy):
        reported=database.heartbeat_policy_worker(config.worker_id,config.resource_class,config.backend,
            operations,sample.ram_bytes,sample.vram_bytes if config.resource_class=='gpu' else 0,
            sample.scratch_bytes,config.capacity_policy.sha256)
        if reported is not True:return False
        if residency_snapshot is not None:
            if not callable(residency_snapshot):raise WorkerConfigurationError('Invalid model residency source')
            return database.report_model_residency(config.worker_id,config.capacity_policy.sha256,residency_snapshot()) is True
        return True
    if config.resource_class=='gpu':
        return database.heartbeat_accelerator_worker(config.worker_id,config.backend,operations,
            sample.ram_bytes,sample.vram_bytes,sample.scratch_bytes)
    return database.heartbeat_cpu_worker(config.worker_id,config.resource_class,operations,
        sample.ram_bytes,sample.scratch_bytes)


def _capacity_sample(config:WorkerConfig,measured,*,metal_budget=None):
    if config.capacity_policy is None:
        if config.resource_class == 'gpu' and config.backend == 'mps':
            raise WorkerConfigurationError('PHOTO_CAPACITY_POLICY required for MPS')
        return measured
    capped=config.capacity_policy.cap_measurement(measured,resource_class=config.resource_class,
        scratch_total_bytes=shutil.disk_usage(config.scratch_root).total)
    if config.resource_class == 'gpu' and config.backend == 'mps':
        from .metal_budget import remaining_metal_budget
        from .capacity import Capacity
        limit=config.capacity_policy.gpu_budget_bytes
        from .metal_budget import valid_metal_budget_receipt
        if not valid_metal_budget_receipt(metal_budget,limit):
            raise WorkerConfigurationError('Metal allocator budget receipt mismatch')
        limit=metal_budget.allocator_bytes
        remaining=remaining_metal_budget(limit)
        return Capacity(capped.ram_bytes,capped.scratch_bytes,min(capped.vram_bytes,remaining))
    return capped


def _install_metal_budget(config):
    if config.resource_class == 'gpu' and config.backend == 'mps':
        if config.capacity_policy is None:
            raise WorkerConfigurationError('PHOTO_CAPACITY_POLICY required for MPS')
        from .metal_budget import install_metal_budget
        return install_metal_budget(config.capacity_policy.gpu_budget_bytes)
    return None


def _ready_metal_budget(config, installed):
    """Reuse the pre-build MPS receipt without resetting the allocator fraction."""
    if config.resource_class != 'gpu' or config.backend != 'mps':
        if installed is not None:
            raise WorkerConfigurationError('Unexpected Metal allocator budget receipt')
        return None
    if installed is None:
        return _install_metal_budget(config)
    from .metal_budget import valid_metal_budget_receipt, remaining_metal_budget
    if config.capacity_policy is None:
        raise WorkerConfigurationError('PHOTO_CAPACITY_POLICY required for MPS')
    limit = config.capacity_policy.gpu_budget_bytes
    if not valid_metal_budget_receipt(installed, limit):
        raise WorkerConfigurationError('Metal allocator budget receipt mismatch')
    remaining_metal_budget(installed.allocator_bytes)
    return installed
