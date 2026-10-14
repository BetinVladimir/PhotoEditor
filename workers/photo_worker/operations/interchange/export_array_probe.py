"""Local physical array probe; semantic canvas/source authority stays in caller."""
from pathlib import Path
import re
import sys
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.interchange.export_mask_probe import export_mask_runtime_sha
from photo_worker.operations.interchange.export_mask_qualification import qualify_export_array_file


def probe_export_array(request):
    if (not isinstance(request, dict) or set(request) != {
            'schema_version', 'file_path', 'sha256', 'bytes', 'media_type',
            'width', 'height', 'canvas_hash', 'contract', 'dtype'}
            or type(request['schema_version']) is not int or request['schema_version'] != 1
            or not isinstance(request['file_path'], str)
            or len(request['file_path']) > 4096
            or not Path(request['file_path']).is_absolute()
            or request['media_type'] not in ('application/octet-stream', 'application/x-npy')
            or not isinstance(request['canvas_hash'], str)
            or re.fullmatch('[0-9a-f]{64}', request['canvas_hash']) is None):
        raise ValueError('INTERCHANGE_EXPORT_ARRAY_PROBE_REQUEST')
    receipt = qualify_export_array_file(Path(request['file_path']), request['contract'],
        request['width'], request['height'], request['bytes'], request['sha256'],
        dtype=request['dtype'])
    return {**receipt, 'schema_version': 1, 'contract': request['contract'],
            'canvas_hash': request['canvas_hash'], 'media_type': request['media_type'],
            'runtime_sha256': export_mask_runtime_sha()}


def main():
    try:
        request = parse_json(sys.stdin.buffer.read(8193), max_bytes=8192)
        result = canonical_bytes(probe_export_array(request))
        if len(result) > 4096:
            raise ValueError('INTERCHANGE_EXPORT_ARRAY_PROBE_RECEIPT')
        sys.stdout.buffer.write(result)
    except Exception:
        sys.stderr.write('INTERCHANGE_EXPORT_ARRAY_PROBE_FAILED\n')
        return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
