"""Bounded local StyleV2/schema probe with an exact model-policy file pin."""
from hashlib import sha256
from importlib.metadata import version
from pathlib import Path
import os
import platform
import re
import stat
import sys
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.v2.interchange_export_style import validate_export_style, validate_export_override, validate_export_composition, validate_export_resolved, validate_export_resolved_origin, validate_export_operation_params, validate_export_series_corrections, validate_export_series_origin, validate_export_series_measure_origin, validate_export_mask_origin, validate_export_body_origin, validate_export_body_dependency_origin, validate_export_body_fit_origin, validate_export_pair_alignment_origin, validate_export_face_detector_origin


def read_pinned(path, size, digest, maximum):
    if (not isinstance(path, str) or len(path) > 4096 or not Path(path).is_absolute()
            or type(size) is not int or not 2 <= size <= maximum
            or not isinstance(digest, str) or re.fullmatch('[0-9a-f]{64}', digest) is None):
        raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_PIN')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size != size:
            raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_FILE')
        raw = stream.read(size + 1)
        after = os.fstat(stream.fileno())
        pin = lambda value: (value.st_dev, value.st_ino, value.st_size,
                             value.st_mtime_ns, value.st_ctime_ns)
        if (pin(before) != pin(after) or len(raw) != size
                or sha256(raw).hexdigest() != digest):
            raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_BYTES')
    return raw


def probe_export_style(request):
    fields = {'schema_version', 'file_path', 'bytes', 'sha256',
              'model_policy_path', 'model_policy_bytes', 'model_policy_sha256'}
    if not isinstance(request, dict) or type(request.get('schema_version')) is not int:
        raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_REQUEST')
    if request['schema_version'] == 1 and set(request) == fields:
        contract = 'StyleV2'
    elif (request['schema_version'] == 2 and set(request) == fields | {'contract'}
          and request['contract'] == 'Override'):
        contract = 'Override'
    elif (request['schema_version'] == 3 and set(request) == fields | {'contract'}
          and request['contract'] == 'RenderComposition'):
        contract = 'RenderComposition'
    elif (request['schema_version'] == 4 and set(request) == fields | {'contract'}
          and request['contract'] == 'ResolvedStyle'):
        contract = 'ResolvedStyle'
    elif (request['schema_version'] == 5 and set(request) == fields | {'contract'}
          and request['contract'] == 'ResolvedOrigin'):
        contract = 'ResolvedOrigin'
    elif (request['schema_version'] == 6 and set(request) == fields | {'contract'}
          and request['contract'] == 'OperationParams'):
        contract = 'OperationParams'
    elif (request['schema_version'] == 7 and set(request) == fields | {'contract'}
          and request['contract'] == 'SeriesCorrections'):
        contract = 'SeriesCorrections'
    elif (request['schema_version'] == 8 and set(request) == fields | {'contract'}
          and request['contract'] == 'SeriesOrigin'):
        contract = 'SeriesOrigin'
    elif (request['schema_version'] == 9 and set(request) == fields | {'contract'}
          and request['contract'] == 'SeriesMeasureOrigin'):
        contract = 'SeriesMeasureOrigin'
    elif (request['schema_version'] == 10 and set(request) == fields | {'contract'}
          and request['contract'] == 'MaskOrigin'):
        contract = 'MaskOrigin'
    elif (request['schema_version'] == 11 and set(request) == fields | {'contract'}
          and request['contract'] == 'BodyOrigin'):
        contract = 'BodyOrigin'
    elif (request['schema_version'] == 12 and set(request) == fields | {'contract'}
          and request['contract'] == 'BodyDependencyOrigin'):
        contract = 'BodyDependencyOrigin'
    elif (request['schema_version'] == 13 and set(request) == fields | {'contract'}
          and request['contract'] == 'BodyFitOrigin'):
        contract = 'BodyFitOrigin'
    elif (request['schema_version'] == 14 and set(request) == fields | {'contract'}
          and request['contract'] == 'PairAlignmentOrigin'):
        contract = 'PairAlignmentOrigin'
    elif (request['schema_version'] == 15 and set(request) == fields | {'contract'}
          and request['contract'] == 'FaceDetectorOrigin'):
        contract = 'FaceDetectorOrigin'
    else:
        raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_REQUEST')
    raw = read_pinned(request['file_path'], request['bytes'], request['sha256'], (16 if contract == 'BodyFitOrigin' else 4 if contract in ('BodyDependencyOrigin', 'PairAlignmentOrigin') else 12 if contract == 'MaskOrigin' else 32 if contract == 'SeriesOrigin' else 128 if contract == 'RenderComposition' else 10 if contract in ('ResolvedOrigin', 'SeriesMeasureOrigin', 'BodyOrigin') else 2 if contract == 'OperationParams' else 8) << 20)
    policy_raw = read_pinned(request['model_policy_path'], request['model_policy_bytes'],
                             request['model_policy_sha256'], 1 << 20)
    policy = parse_json(policy_raw, max_bytes=1 << 20)
    if canonical_bytes(policy) != policy_raw:
        raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_POLICY')
    validator = {'StyleV2': validate_export_style, 'Override': validate_export_override,
                 'RenderComposition': validate_export_composition, 'ResolvedStyle': validate_export_resolved,
                 'ResolvedOrigin': validate_export_resolved_origin,
                 'OperationParams': validate_export_operation_params,
                 'SeriesCorrections': validate_export_series_corrections,
                 'SeriesOrigin': validate_export_series_origin,
                 'SeriesMeasureOrigin': validate_export_series_measure_origin,
                 'MaskOrigin': validate_export_mask_origin,
                 'BodyOrigin': validate_export_body_origin,
                 'BodyDependencyOrigin': validate_export_body_dependency_origin,
                 'BodyFitOrigin': validate_export_body_fit_origin,
                 'PairAlignmentOrigin': validate_export_pair_alignment_origin,
                 'FaceDetectorOrigin': validate_export_face_detector_origin}[contract]
    if contract == 'RenderComposition':
        from photo_worker.color.icc import ICCRegistry
        root = os.environ.get('PHOTO_ICC_ROOT')
        registry = None
        if root is not None:
            if not root or not Path(root).is_absolute():
                raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_ICC_ROOT')
            registry = ICCRegistry(Path(root))
        validator(raw, request['sha256'], policy, icc_registry=registry)
    else:
        validator(raw, request['sha256'], policy)
    runtime_versions = {'python': platform.python_version(), 'pydantic': version('pydantic')}
    if contract in ('BodyFitOrigin', 'PairAlignmentOrigin'):
        import numpy as np
        import cv2
        runtime_versions.update(numpy=np.__version__, opencv=cv2.__version__)
    runtime = sha256(canonical_bytes(runtime_versions)).hexdigest()
    return {'schema_version': 1, 'contract': contract, 'sha256': request['sha256'],
            'bytes': request['bytes'], 'model_policy_sha256': request['model_policy_sha256'],
            'runtime_sha256': runtime}


def main():
    try:
        request = parse_json(sys.stdin.buffer.read(8193), max_bytes=8192)
        result = canonical_bytes(probe_export_style(request))
        if len(result) > 4096:
            raise ValueError('INTERCHANGE_EXPORT_STYLE_PROBE_RECEIPT')
        sys.stdout.buffer.write(result)
    except Exception:
        sys.stderr.write('INTERCHANGE_EXPORT_STYLE_PROBE_FAILED\n')
        return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
