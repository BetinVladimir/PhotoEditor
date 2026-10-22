"""Production Region adopter uses the same installed qualified native factories."""
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from photo_worker.runtime.region_config import region_installation_from_env,configured_region_renderer


@dataclass(frozen=True)
class RegionAdopterConfig:
    installation: object
    model_root: Path
    model_lock: Path
    icc_root: Path
    backend: str

    @classmethod
    def from_env(cls,env):
        enabled=env.get('PHOTO_REGION_ADOPTER_ENABLED','false')
        if enabled not in ('true','false'):raise ValueError('Invalid Region adopter enablement')
        if enabled=='false':return None
        backend=env.get('PHOTO_REGION_ADOPTER_BACKEND')
        if backend not in ('cuda','mps'):raise ValueError('Region adopter requires exact CUDA/Metal backend')
        pins=dict(env);pins['PHOTO_REGION_RUNTIME_ENABLED']='true'
        installation=region_installation_from_env(pins,'gpu',backend)
        paths=[]
        for name,folder in (('PHOTO_MODEL_ROOT',True),('PHOTO_MODEL_LOCK',False),('PHOTO_ICC_ROOT',True)):
            if not env.get(name):raise ValueError('Region adopter missing '+name)
            path=Path(env[name])
            if (not path.is_absolute() or path.resolve(strict=True)!=path or path.is_symlink()
                    or (not path.is_dir() if folder else not path.is_file())):
                raise ValueError('Region adopter invalid '+name)
            paths.append(path)
        return cls(installation,*paths,backend)


def configured_region_adopter(config,scratch_root,max_bytes):
    from photo_worker.models.lock import ModelLock
    from photo_worker.models.pool import ModelPool
    from photo_worker.models.adapters import factories
    from .region_replay import NativeRegionReplay
    if type(config) is not RegionAdopterConfig:raise ValueError('Region adopter config required')
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:raise ValueError('Region adopter scratch required')
    budget=RegionAdopterBudget.install(config.backend)
    lock=ModelLock.model_validate_json(config.model_lock.read_bytes())
    pool=ModelPool(config.model_root,lock,factories())
    try:
        worker=SimpleNamespace(region_installation=config.installation,backend=config.backend,
            icc_root=config.icc_root,max_download_bytes=max_bytes)
        renderer=configured_region_renderer(worker,pool)
        return NativeRegionReplay(renderer,scratch_root,max_bytes,budget),pool
    except BaseException:
        pool.close();raise


@dataclass(frozen=True)
class RegionAdopterBudget:
    backend: str
    limit_bytes: int

    @classmethod
    def install(cls,backend,*,torch_module=None):
        # Set process allocator policy before any model factory is constructed.
        if backend=='cuda':
            from photo_worker.runtime.cuda_budget import install_cuda_budget,CUDA_MVP_BYTES
            install_cuda_budget(CUDA_MVP_BYTES,torch_module=torch_module)
            return cls(backend,CUDA_MVP_BYTES)
        if backend!='mps':raise ValueError('REGION_ADOPTER_BACKEND_INVALID')
        if torch_module is None:import torch as torch_module
        if not torch_module.backends.mps.is_available():raise ValueError('METAL_BUDGET_UNAVAILABLE')
        # Device recommendation only; no artificial CUDA-sized Metal ceiling.
        from photo_worker.runtime.metal_budget import install_metal_budget
        receipt=install_metal_budget(torch_module.mps.recommended_max_memory(),torch_module=torch_module)
        return cls(backend,receipt.allocator_bytes)

    def remaining(self,*,torch_module=None):
        if self.backend=='cuda':
            from photo_worker.runtime.cuda_budget import remaining_cuda_budget
            return remaining_cuda_budget(self.limit_bytes,torch_module=torch_module)
        if self.backend=='mps':
            from photo_worker.runtime.metal_budget import remaining_metal_budget
            return remaining_metal_budget(self.limit_bytes,torch_module=torch_module)
        raise ValueError('REGION_ADOPTER_BACKEND_INVALID')
