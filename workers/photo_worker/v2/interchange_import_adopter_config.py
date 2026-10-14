"""Pinned local resources for a process-qualified V30 import adopter.

This loader never treats a file or environment variable as runtime approval.
The caller must install an independent live qualification callback.
"""

from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
import os
import re

from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
from photo_worker.runtime.learning_config import _read_file
from .interchange_import_adopter_installation import SchedulerImportRecipeAdoptionInstallation
from .interchange_operator_schemas import build_interchange_operator_schemas


@dataclass(frozen=True)
class PinnedImportAdopterResources:
    schemas_jcs: bytes
    schemas_sha: str
    work_icc_sha: str
    scratch_root: Path
    scratch_bytes: int
    max_file_bytes: int

    def builder(self, *, qualify_runtime=None, runtime_builder=None, extra_validators=None,
                qualify_confirmation=None, qualify_occurrence=None,
                operation_schemas=None, background_mappings=None):
        if (qualify_runtime is None) == (runtime_builder is None):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_REQUIRED')
        if qualify_runtime is not None and not callable(qualify_runtime):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_REQUIRED')
        if runtime_builder is not None and not callable(runtime_builder):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_REQUIRED')
        resources = self
        def install(read_connection, stop):
            if not callable(getattr(stop, 'is_set', None)):
                raise ValueError('INTERCHANGE_IMPORT_ADOPTER_STOP_REQUIRED')
            def active():
                if stop.is_set():
                    raise InterruptedError('Adopter stopped')
            runtime = qualify_runtime
            if runtime_builder is not None:
                from .interchange_import_adoption_runtime import SchedulerImportAdoptionRuntimeAuthority
                runtime = runtime_builder(read_connection, stop)
                if type(runtime) is not SchedulerImportAdoptionRuntimeAuthority:
                    raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_REQUIRED')
            return SchedulerImportRecipeAdoptionInstallation(
                read_connection, schemas_jcs=resources.schemas_jcs,
                schemas_sha=resources.schemas_sha, work_icc_sha=resources.work_icc_sha,
                scratch_root=resources.scratch_root, scratch_bytes=resources.scratch_bytes,
                max_file_bytes=resources.max_file_bytes, qualify_runtime=runtime,
                check_active=active, extra_validators=extra_validators,
                qualify_confirmation=qualify_confirmation,
                qualify_occurrence=qualify_occurrence,
                operation_schemas=operation_schemas,
                background_mappings=background_mappings)
        return install


def import_adopter_resources_from_environment(getenv=os.environ.get):
    prefix = 'PHOTO_ADOPTER_INTERCHANGE_IMPORT_'
    enabled = getenv(prefix + 'ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_ENABLEMENT')
    keys = ('SCHEMAS_FILE', 'SCHEMAS_SHA', 'SCRATCH_ROOT',
            'SCRATCH_BYTES', 'MAX_FILE_BYTES')
    values = {key: getenv(prefix + key, '') for key in keys}
    if enabled == 'false':
        if any(values.values()):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_PARTIAL_CONFIG')
        return None
    if any(not isinstance(value, str) or not value for value in values.values()):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_PARTIAL_CONFIG')
    digest = values['SCHEMAS_SHA']
    if re.fullmatch('[0-9a-f]{64}', digest) is None:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_SCHEMA_PIN')
    raw = _read_file(values['SCHEMAS_FILE'], 16 << 20)
    if not 2 <= len(raw) or sha256(raw).hexdigest() != digest:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_SCHEMA_PIN')
    schema = parse_json(raw, max_bytes=16 << 20)
    if not isinstance(schema, dict) or canonical_bytes(schema) != raw:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_SCHEMAS')
    build_interchange_operator_schemas(schema.get('$defs'))
    scratch = Path(values['SCRATCH_ROOT'])
    if (not scratch.is_absolute() or not scratch.is_dir()
            or scratch.resolve(strict=True) != scratch):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_SCRATCH')
    def number(key, upper):
        value = values[key]
        if re.fullmatch('[1-9][0-9]{0,15}', value) is None:
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUDGET')
        parsed = int(value)
        if parsed > upper:
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUDGET')
        return parsed
    total = number('SCRATCH_BYTES', 2**53-1)
    maximum = number('MAX_FILE_BYTES', 128 << 30)
    if maximum > total:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUDGET')
    icc = Path(getenv('PHOTO_ICC_ROOT', ''))
    if (not icc.is_absolute() or not icc.is_dir()
            or icc.resolve(strict=True) != icc):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_ICC')
    work = ICCRegistry(icc).resolve('work_rec2020_gamma24_v1')
    if work.sha256 != WORKING_PROFILE_SHA:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_ICC')
    return PinnedImportAdopterResources(bytes(raw), digest, work.sha256,
                                        scratch, total, maximum)
