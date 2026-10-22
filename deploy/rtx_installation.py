"""Operator RTX image/launch wiring. Dry-run unless --execute is explicit.

No digest is discovered by pulling an image and no qualification is fabricated.
All image references originate in the operator's reviewed immutable lock.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
IMAGE = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[0-9a-f]{64}$')
TAG = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9._:/-]*:[a-zA-Z0-9_.-]+$')
FIELDS = {'schema_version', 'platform', 'python_version', 'cuda_major', 'cudnn_major',
          'uv_image', 'cuda_runtime_image', 'cuda_python_base_image', 'worker_image'}


def read_lock(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('Duplicate installation lock key')
            result[key] = value
        return result
    with Path(path).open("rb") as source:
        raw = source.read(16385)
    if len(raw) > 16384:
        raise ValueError('Installation lock exceeds limit')
    lock = json.loads(raw, object_pairs_hook=unique)
    if not isinstance(lock, dict) or set(lock) != FIELDS:
        raise ValueError('Installation lock shape invalid')
    for name, expected in [('schema_version', 1), ('platform', 'linux/amd64'),
                           ('python_version', '3.11.14'), ('cuda_major', 12), ('cudnn_major', 9)]:
        if type(lock[name]) is not type(expected) or lock[name] != expected:
            raise ValueError('Installation runtime contract invalid: ' + name)
    for name in ('uv_image', 'cuda_runtime_image', 'cuda_python_base_image', 'worker_image'):
        if lock[name] is not None and (not isinstance(lock[name], str) or not IMAGE.fullmatch(lock[name])):
            raise ValueError('Immutable image digest required: ' + name)
    return lock


def require_images(lock, names):
    for name in names:
        if lock[name] is None:
            raise ValueError('Reviewed image digest is unknown: ' + name)


def command(lock, phase, *, output_tag=None, metadata_file=None, env_file=None):
    if phase in ('base', 'worker'):
        names = ('uv_image', 'cuda_runtime_image') if phase == 'base' else ('uv_image', 'cuda_python_base_image')
        require_images(lock, names)
        if not isinstance(output_tag, str) or not TAG.fullmatch(output_tag) or '@' in output_tag:
            raise ValueError('Explicit output repository:tag required')
        if metadata_file is None or not Path(metadata_file).is_absolute():
            raise ValueError('Absolute build metadata output path required')
        recipe = 'Dockerfile.cuda-base' if phase == 'base' else 'Dockerfile.cuda'
        args = ['docker', 'buildx', 'build', '--platform', lock['platform'], '--provenance=mode=max',
                '--push', '--metadata-file', str(metadata_file), '--tag', output_tag,
                '--file', str(ROOT / 'deploy/images/worker' / recipe)]
        for name in names:
            args += ['--build-arg', name.upper() + '=' + lock[name]]
        return args + [str(ROOT)], {}
    if phase != 'start':
        raise ValueError('Unknown installation phase')
    require_images(lock, ('worker_image',))
    if env_file is None or not Path(env_file).is_absolute() or not Path(env_file).is_file():
        raise ValueError('Existing absolute operator environment file required')
    # Compose --env-file supplies interpolation only. The mounted operation pins
    # remain in PHOTO_RTX_INSTALLATION_ENV_FILE, as in the dedicated host contract.
    return ['docker', 'compose', '--env-file', str(env_file), '--file',
            str(ROOT / 'deploy/compose.rtx-worker.yaml'), 'up', '--detach'], {
                'PHOTO_RTX_WORKER_IMAGE': lock['worker_image']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase', choices=('base', 'worker', 'start'))
    parser.add_argument('--lock', type=Path, required=True)
    parser.add_argument('--output-tag')
    parser.add_argument('--metadata-file', type=Path)
    parser.add_argument('--env-file', type=Path)
    parser.add_argument('--execute', action='store_true')
    options = parser.parse_args()
    try:
        args, additions = command(read_lock(options.lock), options.phase,
            output_tag=options.output_tag, metadata_file=options.metadata_file, env_file=options.env_file)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    if options.execute:
        subprocess.run(args, cwd=ROOT, env={**os.environ, **additions}, check=True)
    else:
        print(json.dumps({'argv': args, 'environment': additions, 'executed': False}, indent=2))


if __name__ == '__main__':
    main()
