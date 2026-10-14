"""Bounded local process protocol for physical MaskArray qualification."""
from hashlib import sha256
from importlib.metadata import version
from pathlib import Path
import platform
import re
import sys

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.interchange.export_mask_qualification import qualify_export_mask_file


def export_mask_runtime_sha():
    return sha256(canonical_bytes({'python': platform.python_version(),
                                  'numpy': version('numpy')})).hexdigest()


def probe_export_mask(request):
    if (not isinstance(request, dict) or set(request) != {
            'schema_version', 'file_path', 'sha256', 'bytes', 'media_type',
            'width', 'height', 'canvas_hash'}
            or type(request['schema_version']) is not int or request['schema_version'] != 1
            or not isinstance(request['file_path'], str)
            or len(request['file_path']) > 4096
            or not Path(request['file_path']).is_absolute()
            or request['media_type'] not in ('application/octet-stream', 'application/x-npy')
            or not isinstance(request['canvas_hash'], str)
            or re.fullmatch('[0-9a-f]{64}', request['canvas_hash']) is None):
        raise ValueError('INTERCHANGE_EXPORT_MASK_PROBE_REQUEST')
    receipt = qualify_export_mask_file(Path(request['file_path']), request['width'],
        request['height'], request['bytes'], request['sha256'])
    return {**receipt, 'schema_version': 1, 'media_type': request['media_type'],
            'canvas_hash': request['canvas_hash'], 'runtime_sha256': export_mask_runtime_sha()}


def main():
    try:
        request = parse_json(sys.stdin.buffer.read(8193), max_bytes=8192)
        result = canonical_bytes(probe_export_mask(request))
        if len(result) > 4096:
            raise ValueError('INTERCHANGE_EXPORT_MASK_PROBE_RECEIPT')
        sys.stdout.buffer.write(result)
    except Exception:
        sys.stderr.write('INTERCHANGE_EXPORT_MASK_PROBE_FAILED\n')
        return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
