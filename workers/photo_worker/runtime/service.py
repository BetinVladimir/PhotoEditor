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
        return cls(dsn,required('AMQP_URL'),worker,resource_class,root,origin,
            Path(required('GATEWAY_CA_FILE')),Path(required('WORKER_CERT_FILE')),Path(required('WORKER_KEY_FILE')),
            limit('WORKER_MAX_SCRATCH_BYTES'),limit('WORKER_MAX_DOWNLOAD_BYTES'),limit('WORKER_MAX_UPLOAD_BYTES'),environment=='development',icc_root,backend,model_root,model_lock,policy,lens_registry,layer_lock,layer_sha)


def build_worker(config:WorkerConfig)->tuple[TaskDatabase,Transport]:
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
            ids={entry.id for entry in lock.models}
            operations+=capture_assess_operations(artifacts,pool,config.backend)
            if config.backend in ("cpu", "cuda"): operations+=capture_prepare_operations(artifacts,pool,config.backend)
            if 'lraspp_mobilenet_v3_large' in ids:operations+=person_operations(artifacts,pool,config.backend)
            if 'keypointrcnn_resnet50_fpn' in ids:operations+=pose_operations(artifacts,pool,config.backend)
            if 'sfd' in ids:operations+=face_detect_operations(artifacts,pool,config.backend)
            if 'fan2d' in ids:operations+=landmark_operations(artifacts,pool,config.backend)
            if 'bisenet_face19' in ids:operations+=face_parse_operations(artifacts,pool,config.backend)
            if 'dinov2_vits14' in ids:operations+=scene_operations(artifacts,pool,config.backend)
            if REQUIRED_MODELS<=ids:operations+=session_describe_operations(artifacts,pool,config.backend)
            if IDENTITY_MODELS<=ids:operations+=identity_compare_operations(artifacts,pool,config.backend)
            if 'depth_anything_v2_small' in ids:operations+=depth_operations(artifacts,pool,config.backend)
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
        operations=[op for op in [*preview_operations(artifacts,config.icc_root),*asset_operations(artifacts,config.icc_root),*pair_operations(artifacts),*body_fit_operations(artifacts),*beauty_fit_operations(artifacts),*defect_fit_operations(artifacts),*body_operations(artifacts),*structure_operations(artifacts),*composition_operations(artifacts),*crop_operations(artifacts),*stats_operations(artifacts),*resolve_operations(artifacts),*aggregate_operations(artifacts),*validate_operations(artifacts),*package_operations(artifacts),*import_operations(artifacts),*group_operations(artifacts),*session_plan_operations(artifacts),*learning_fit_operations(artifacts),*cull_operations(artifacts),*measure_operations(artifacts),*balance_operations(artifacts),*image_color_operations(artifacts),*image_local_operations(artifacts),*resize_operations(artifacts),*sharpen_operations(artifacts),*watermark_operations(artifacts),*metadata_operations(artifacts),*contact_sheet_operations(artifacts),*quality_operations(artifacts),*background_operations(artifacts),*blur_operations(artifacts),*sky_operations(artifacts),*hdr_operations(artifacts),*register_operations(artifacts),*merge_operations(artifacts),*sensor_operations(artifacts),*dust_repair_operations(artifacts),*mask_operations(artifacts),*face_skin_operations(artifacts),*mask_transform_operations(artifacts),*beauty_operations(artifacts),*detail_operations(artifacts),*refine_operations(artifacts),*defect_repair_operations(artifacts),*defect_propose_operations(artifacts),*export_operations(artifacts,config.icc_root),*media_operations(artifacts,config.icc_root)] if op.resource_class==config.resource_class]
        if config.resource_class=='cpu' and config.model_root is not None and config.model_lock is not None:
            from photo_worker.models.adapters import factories
            from photo_worker.models.lock import ModelLock
            from photo_worker.models.pool import ModelPool
            from photo_worker.operations.alpha import alpha_operations
            lock=ModelLock.model_validate_json(config.model_lock.read_bytes());pool=ModelPool(config.model_root,lock,factories())
            if 'u2net' in {entry.id for entry in lock.models}:operations+=alpha_operations(artifacts,pool)
    if config.resource_class=='cpu' and config.lens_registry is not None:
        from photo_worker.operations.optical.adapter import lens_operations
        operations+=lens_operations(artifacts,config.lens_registry)
    if not operations:raise WorkerConfigurationError('No executable operations for resource class')
    database=TaskDatabase(config.database_url)
    if config.resource_class=='cpu':
        from photo_worker.operations.layers.adapter import layer_operations
        operations+=layer_operations(artifacts,database,config.layer_runtime_lock,config.layer_runtime_sha)
    executor=Executor(database,config.worker_id,operations)
    return database,Transport(config.amqp_url,config.resource_class,executor,development=config.development)


def run_worker(config:WorkerConfig):
    if current_thread() is not main_thread():raise WorkerConfigurationError('Worker must run on process main thread')
    database,transport=build_worker(config)
    database.ping()
    registered=(database.accelerator_operations(config.worker_id,config.backend) if config.resource_class=='gpu'
        else database.runtime_operations(config.worker_id,config.resource_class))
    installed=frozenset(transport.executor.operations)
    if not registered or not registered.issubset(installed):
        raise WorkerConfigurationError('Worker registration does not match executable operations')
    if config.resource_class=='gpu':
        reporter=CapacityReporter(lambda sample:database.heartbeat_accelerator_worker(config.worker_id,config.backend,installed,sample.ram_bytes,sample.vram_bytes,sample.scratch_bytes),
            lambda:_capacity_sample(config,measure_accelerator(config.scratch_root,config.backend)),transport.executor.abort_active,
            interval_seconds=config.capacity_policy.heartbeat_seconds if config.capacity_policy else 10)
    else:
        reporter=CapacityReporter(lambda sample:database.heartbeat_cpu_worker(config.worker_id,config.resource_class,installed,sample.ram_bytes,sample.scratch_bytes),
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
        transport.executor.abort_active()
        if reports.ident is not None:reports.join(timeout=15)
        for sig,handler in previous.items():signal.signal(sig,handler)
    if reports.is_alive() or reporter.failed.is_set():raise RuntimeError('Worker capacity reporting stopped')


def _capacity_sample(config:WorkerConfig,measured):
    if config.capacity_policy is None:return measured
    return config.capacity_policy.cap_measurement(measured,resource_class=config.resource_class,
        scratch_total_bytes=shutil.disk_usage(config.scratch_root).total)
