"""Immutable composition build identity; qualification remains an external gate."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import sys
from typing import Literal
from pydantic import model_validator
from photo_worker.contracts.base import Contract, SHA256, canonical_hash, canonical_bytes
from photo_worker.operations.layers.runtime_lock import component_paths as layer_paths
from photo_worker.runtime.executor import OperationFailure

RENDER_COMPONENTS = ('recipe','inputs','layers','layer_params','providers','learning','scalars','overrides',
    'style','color','native_base','background','background_binding','background_decode','beauty','defects','detail','denoise','dust','upscale','geometry_masks','effects','portrait','portrait_background','protection','masks','adjustments','changes','composition','source','reports','prepare','files',
    'stage','quality','result','publish','resources','modules','authored','execute','task','adoption','adoption_inputs','prepare_adoption','profiled_replay','qualification_pin','cache_identity','cache_lookup','cache_store','cache_bytes','cache_republish','cache_worker','replay_artifacts','replay_reader','recomposition_adoption','runtime')


def component_paths():
    root = Path(__file__).resolve().parents[1]
    result = dict(layer_paths())
    for name in RENDER_COMPONENTS:
        result['render_'+name] = root/'v2'/('render_'+name+'.py')
    for name in ('style','override'):
        result['contract_'+name] = root/'contracts'/(name+'.py')
    for name, relative in {'learning_contracts':'operations/learning/contracts.py',
            'learning_model':'operations/learning/model.py',
            'learning_recipe':'operations/learning/recipe.py',
            'portrait_prepare':'operations/portrait/prepare.py',
            'portrait_detail_prepare':'operations/portrait/prepare_detail.py',
            'portrait_detail_algorithm':'algorithms/portrait_detail.py',
            'portrait_detail_contract':'contracts/portrait_detail.py',
            'denoise_prepare':'operations/image/prepare_denoise.py',
            'upscale_prepare':'operations/image/prepare_upscale.py',
            'mask_transform_prepare':'operations/mask/transform_prepare.py',
            'mask_transform_publish':'operations/mask/transform_publish.py',
            'mask_transform_algorithm':'algorithms/mask_transform.py',
            'mask_refine_algorithm':'algorithms/mask_refine.py',
            'mask_png_algorithm':'algorithms/mask_png.py',
            'denoise_delta':'algorithms/model_delta.py',
            'restoration_contract':'contracts/restoration.py',
            'background_prepare':'operations/background/prepare.py',
            'background_prepare_blur':'operations/background/prepare_blur.py',
            'algorithm_background':'algorithms/background.py','contract_background':'contracts/background.py',
            'geometry_prepare':'operations/geometry/prepare.py',
            'asset_decode':'operations/asset/decode.py','asset_verify':'operations/asset/verify.py',
            'asset_inspect':'operations/asset/inspect.py','contract_assets':'contracts/assets.py',
            'defects_prepare':'operations/defects/prepare.py','contract_defects':'contracts/defects.py',
            'algorithm_defect_labels':'algorithms/defect_labels.py','algorithm_defect_repair':'algorithms/defect_repair.py',
            'algorithm_effective':'algorithms/effective.py',
            'algorithm_beauty':'algorithms/beauty.py','algorithm_beauty_roi':'algorithms/beauty_roi.py',
            'algorithm_skin_mask':'algorithms/skin_mask.py','algorithm_masks':'algorithms/masks.py',
            'algorithm_face_composite':'algorithms/face_composite.py',
            'contract_face':'contracts/face.py',
            'learning_color_render':'operations/learning/color_render.py',
            'algorithm_color':'algorithms/color.py','algorithm_style':'algorithms/style.py',
            'algorithm_regions':'algorithms/regions.py','contract_resolved':'contracts/resolved.py',
            'runtime_database':'runtime/database.py',
            'runtime_service':'runtime/service.py','adopter_service':'v2/service.py',
            'array_evidence':'v2/array_evidence.py','adoption':'v2/adoption.py',
            'adoption_service':'v2/adoption_service.py',
            'lens_adoption':'v2/lens_adoption.py','lens_bypass':'v2/lens_bypass.py',
            'render_derivative':'v2/render_derivative.py',
            'render_derivative_adoption':'v2/render_derivative_adoption.py',
            'render_derivative_store':'v2/render_derivative_store.py',
            'render_derivative_service':'v2/render_derivative_service.py',
            'render_derivative_schema':'v2/render_derivative_schema.py',
            'object_evidence_reader':'v2/adoption_store.py',
            'media_derivative_encoder':'operations/media/derivative.py',
            'media_contract':'contracts/media.py',
            'dust_prepare':'operations/sensor/prepare_repair.py','dust_contract':'contracts/dust.py'}.items():
        result[name] = root/relative
    return result


def installed_component_hashes():
    result = {}
    for name,path in component_paths().items():
        digest = sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1048576),b''):
                digest.update(block)
        result[name] = digest.hexdigest()
    return result


class RenderRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: str
    implementation: Literal['CPython']
    machine: str
    packages: dict[str,str]
    components: dict[str,SHA256]

    @model_validator(mode='after')
    def inventory(self):
        import re
        if (re.fullmatch(r'\d+\.\d+\.\d+',self.python_version) is None
                or not 1<=len(self.machine)<=80
                or set(self.packages)!={'numpy','pydantic','rfc8785'}
                or any(not 1<=len(version)<=120 for version in self.packages.values())
                or set(self.components)!=set(component_paths())):
            raise ValueError('RENDER_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock: RenderRuntimeLock, expected_sha: str) -> str:
    lock = RenderRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest = canonical_hash(lock)
    if digest != expected_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation()!=lock.implementation
            or '.'.join(map(str,sys.version_info[:3]))!=lock.python_version
            or platform.machine()!=lock.machine):
        raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name)!=version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    if installed_component_hashes()!=lock.components:
        raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest


def load_render_runtime_lock(path, expected_sha: str) -> RenderRuntimeLock:
    """Read one bounded regular file and verify externally pinned build identity."""
    import os
    import stat
    from photo_worker.contracts.base import parse_json
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as source:
            info = os.fstat(source.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                    or not 1 <= info.st_size <= 65536):
                raise OperationFailure('RUNTIME_LOCK_MISMATCH')
            raw = source.read(65537)
        if len(raw) != info.st_size:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        parsed = parse_json(raw, max_bytes=65536)
        if canonical_bytes(parsed) != raw:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        lock = RenderRuntimeLock.model_validate_json(raw)
    except (OSError, ValueError) as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock, expected_sha)
    return lock
