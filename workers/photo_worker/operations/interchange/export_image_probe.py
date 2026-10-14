"""Local admission probe; only the shared native decoder can produce a receipt."""
from hashlib import sha256
from importlib.metadata import version
from pathlib import Path
import platform
import sys
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.interchange.export_images import qualify_export_image_file


def export_native_runtime_sha():
    import pyvips
    runtime = {'python': platform.python_version(), 'pyvips': version('pyvips'),
               'libvips': '.'.join(str(pyvips.version(i)) for i in range(3))}
    return sha256(canonical_bytes(runtime)).hexdigest()


def probe_export_image(request):
    if (not isinstance(request, dict)
            or set(request) != {'schema_version', 'file_path', 'sha256', 'bytes', 'media_type'}
            or type(request['schema_version']) is not int or request['schema_version'] != 1
            or not isinstance(request['file_path'], str)
            or len(request['file_path']) > 4096 or not Path(request['file_path']).is_absolute()):
        raise ValueError('INTERCHANGE_EXPORT_PROBE_REQUEST')
    path = Path(request['file_path'])
    metadata = qualify_export_image_file(path, request['media_type'],
        request['bytes'], request['sha256'])
    return {'schema_version': 1, 'sha256': request['sha256'], 'bytes': request['bytes'],
            'media_type': request['media_type'], 'width': metadata.width,
            'height': metadata.height, 'bit_depth': metadata.bit_depth,
            'has_alpha': metadata.has_alpha, 'orientation': metadata.orientation,
            'icc_sha256': metadata.icc_sha256, 'runtime_sha256': export_native_runtime_sha()}


def main():
    try:
        raw = sys.stdin.buffer.read(8193)
        request = parse_json(raw, max_bytes=8192)
        result = canonical_bytes(probe_export_image(request))
        if len(result) > 4096:
            raise ValueError('INTERCHANGE_EXPORT_PROBE_RECEIPT')
        sys.stdout.buffer.write(result)
    except Exception:
        # Do not expose filesystem paths, metadata or library diagnostics.
        sys.stderr.write('INTERCHANGE_EXPORT_NATIVE_PROBE_FAILED\n')
        return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
