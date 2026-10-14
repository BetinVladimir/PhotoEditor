"""Installed bytes/device measurement for person qualification; no self-approval."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
from photo_worker.contracts.base import canonical_hash
from photo_worker.models.install import ModelError,verify_model
from photo_worker.runtime.executor import OperationFailure


def component_paths():
    import torch
    import torchvision
    import cv2
    from photo_worker.operations.optical.runtime_lock import component_paths as optical
    base=optical()
    paths={key:base[key] for key in ('python','numpy_core','pydantic_core','canonical_impl')}
    root=Path(__file__).resolve().parents[2]
    for name in ('person','person_authority','person_measurement','person_nvml'):
        paths['mask_'+name]=root/'operations/mask'/(name+'.py')
    for name in ('mask_refine','model_delta','mask_png','tiles','color'):
        paths['algorithm_'+name]=root/'algorithms'/(name+'.py')
    for group,names in {'contracts':('base','images','person'),
                        'models':('adapters','install','lock','pool'),
                        'runtime':('arrays','artifacts','executor','scratch')}.items():
        for name in names:paths[group+'_'+name]=root/group/(name+'.py')
    paths['torch_core']=Path(torch._C.__file__)
    paths['torchvision_init']=Path(torchvision.__file__)
    paths['opencv_init']=Path(cv2.__file__)
    for label,folder,pattern in (('torch_lib',Path(torch.__file__).parent/'lib','*'),
                                 ('torchvision_native',Path(torchvision.__file__).parent,'_C*'),
                                 ('opencv_native',Path(cv2.__file__).parent,'cv2*')):
        natives=[path for path in folder.glob(pattern) if path.is_file() and any(suffix in ('.so','.dylib','.dll','.pyd') for suffix in path.suffixes)]
        if not natives:raise OperationFailure('PERSON_RUNTIME_COMPONENTS_UNAVAILABLE')
        for path in natives:paths[label+'_'+path.name]=path
    for label,module in (('torch_python',torch),('torchvision_python',torchvision)):
        folder=Path(module.__file__).parent
        for path in folder.rglob('*.py'):
            if path.is_file():paths[label+'_'+path.relative_to(folder).as_posix()]=path
    folder=Path(torchvision.__file__).parent
    for path in folder.rglob('*'):
        if path.is_file() and any(suffix in ('.so','.dylib','.dll','.pyd') for suffix in path.suffixes):
            paths['torchvision_library_'+path.relative_to(folder).as_posix()]=path
    return paths


def measure_runtime():
    import torch
    components={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as stream:
            for block in iter(lambda:stream.read(1048576),b''):digest.update(block)
        components[name]=digest.hexdigest()
    return canonical_hash(dict(schema_version=1,operation='mask.person',python_version=platform.python_version(),
        implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','torch','torchvision')},
        torch_build=sha256(torch.__config__.show().encode()).hexdigest(),components=components))


def measure_device(backend):
    import torch
    value=dict(schema_version=1,backend=backend,machine=platform.machine(),system=platform.system())
    if backend=='cuda':
        if not torch.cuda.is_available():raise OperationFailure('PERSON_DEVICE_UNAVAILABLE')
        from .person_nvml import measure_nvidia_driver
        value.update(measure_nvidia_driver())
        index=torch.cuda.current_device();props=torch.cuda.get_device_properties(index)
        value.update(index=index,name=props.name,total_memory=props.total_memory,
            capability=list(torch.cuda.get_device_capability(index)),multiprocessors=props.multi_processor_count,
            uuid=str(getattr(props,'uuid','')),cuda_build=torch.version.cuda,cudnn_version=torch.backends.cudnn.version(),
            cudnn_deterministic=torch.backends.cudnn.deterministic,cudnn_benchmark=torch.backends.cudnn.benchmark,
            matmul_tf32=torch.backends.cuda.matmul.allow_tf32,cudnn_tf32=torch.backends.cudnn.allow_tf32)
    elif backend=='cpu':
        value.update(processor=platform.processor(),threads=torch.get_num_threads(),interop_threads=torch.get_num_interop_threads())
    elif backend=='mps':
        if not torch.backends.mps.is_available():raise OperationFailure('PERSON_DEVICE_UNAVAILABLE')
        value.update(processor=platform.processor(),os_release=platform.release())
    else:raise OperationFailure('PERSON_DEVICE_UNAVAILABLE')
    return canonical_hash(value)


def installed_person_measurements(pool,backend):
    """Verify person weights on each call; return measured deployment identities.

    No network/download or qualification import occurs. These hashes must be
    approved independently against actual acceptance corpus/benchmarks.
    """
    entries=[entry for entry in pool.lock.models if entry.id=='lraspp_mobilenet_v3_large']
    if len(entries)!=1:raise OperationFailure('PERSON_MODEL_UNAVAILABLE')
    try:
        verify_model(pool.root/pool.lock.lock_id/entries[0].id,entries[0])
        return dict(model_lock_sha=pool.lock.lock_id,runtime_lock_sha=measure_runtime(),
                    device_profile_sha=measure_device(backend),backend=backend)
    except ModelError as error:
        raise OperationFailure(str(error)) from error
    except (OSError,ImportError,ValueError,metadata.PackageNotFoundError) as error:
        raise OperationFailure('PERSON_MEASUREMENT_UNAVAILABLE') from error
