"""Measured installed identity runtime; the policy is approved elsewhere."""
from __future__ import annotations

from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform

from photo_worker.contracts.base import canonical_hash
from photo_worker.models.install import ModelError, verify_model
from photo_worker.runtime.executor import OperationFailure


REQUIRED_MODELS = ('sfd', 'fan2d', 'identity512')


def component_paths() -> dict[str, Path]:
    import onnxruntime
    import scipy
    from photo_worker.operations.mask.person_measurement import component_paths as shared

    paths = {key: path for key, path in shared().items()
             if not key.startswith('mask_') and key != 'contracts_person'}
    root = Path(__file__).resolve().parents[2]
    for name in ('adapter', 'contracts', 'embedding', 'metrics', 'measurement',
                 'authority'):
        paths['identity_' + name] = root / 'operations/identity' / (name + '.py')
    for name in ('contracts', 'describe'):
        paths['session_' + name] = root / 'operations/session' / (name + '.py')
    for name in ('model_delta',):
        paths['algorithm_' + name] = root / 'algorithms' / (name + '.py')
    for name in ('onnx_session', 'backend_support'):
        paths['models_' + name] = root / 'models' / (name + '.py')
    paths['identity_config'] = root / 'runtime/identity_config.py'
    for label, module in (('onnxruntime', onnxruntime), ('scipy', scipy)):
        folder = Path(module.__file__).parent
        for path in folder.rglob('*'):
            if path.is_file() and (path.suffix == '.py' or
                                   path.suffix.lower() in ('.so', '.dylib', '.dll', '.pyd')):
                paths[label + '_' + path.relative_to(folder).as_posix()] = path
    if not any(key.startswith('onnxruntime_') and
               Path(key).suffix.lower() in ('.so', '.dylib', '.dll', '.pyd')
               for key in paths):
        raise OperationFailure('IDENTITY_RUNTIME_COMPONENTS_UNAVAILABLE')
    return paths


def runtime_packages(backend: str) -> dict[str, str]:
    if backend not in ('cpu', 'cuda'):
        raise OperationFailure('IDENTITY_BACKEND_UNAVAILABLE')
    selected = 'onnxruntime-gpu' if backend == 'cuda' else 'onnxruntime'
    other = 'onnxruntime' if backend == 'cuda' else 'onnxruntime-gpu'
    # Both wheels install the same Python namespace; mixed installations cannot
    # be identified by a package version receipt and must not be qualified.
    try:
        metadata.version(other)
    except metadata.PackageNotFoundError:
        pass
    else:
        raise OperationFailure('IDENTITY_RUNTIME_DISTRIBUTION_AMBIGUOUS')
    return {name: metadata.version(name) for name in
            ('numpy', 'pydantic', 'rfc8785', 'torch', 'torchvision', selected, 'scipy')}


def measure_runtime(backend: str = 'cpu') -> str:
    import torch

    components = {}
    for name, path in component_paths().items():
        digest = sha256()
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(1048576), b''):
                digest.update(block)
        components[name] = digest.hexdigest()
    return canonical_hash(dict(schema_version=1, operation='identity.compare',
        python_version=platform.python_version(),
        implementation=platform.python_implementation(), machine=platform.machine(),
        packages=runtime_packages(backend),
        torch_build=sha256(torch.__config__.show().encode()).hexdigest(),
        components=components))


def installed_identity_measurements(pool, backend: str) -> dict[str, str]:
    from photo_worker.operations.mask.person_measurement import measure_device

    if backend not in ('cpu', 'cuda'):
        raise OperationFailure('IDENTITY_BACKEND_UNAVAILABLE')
    try:
        entries = {entry.id: entry for entry in pool.lock.models}
        if not set(REQUIRED_MODELS) <= set(entries):
            raise OperationFailure('IDENTITY_MODEL_UNAVAILABLE')
        for model_id in REQUIRED_MODELS:
            entry = entries[model_id]
            if (not entry.approved_for_install or backend not in entry.backends or
                    entry.backends[backend].status != 'passed'):
                raise OperationFailure('IDENTITY_MODEL_UNQUALIFIED')
            verify_model(pool.root / pool.lock.lock_id / model_id, entry)
        return dict(model_lock_sha=pool.lock.lock_id,
                    runtime_lock_sha=measure_runtime(backend),
                    device_profile_sha=measure_device(backend), backend=backend)
    except ModelError as error:
        raise OperationFailure(str(error)) from error
    except (OSError, ImportError, ValueError, metadata.PackageNotFoundError) as error:
        raise OperationFailure('IDENTITY_MEASUREMENT_UNAVAILABLE') from error
