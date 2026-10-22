"""Narrow, reversible Compose origin migration; does not touch secrets or volumes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile

APP = 'https://app.framevela.com'
PORTAL = 'https://framevela.com'
SERVICES = {'migrate','bootstrap-storage','bootstrap-queue','api','scheduler','publisher',
            'dead-letter-worker','collection-worker','maintenance','object-gateway','mail-worker'}

def migrate(value):
    # Work on a detached tree so refusal never partly changes the caller's data.
    result = json.loads(json.dumps(value))
    services = result['services']
    actual = {name for name, service in services.items()
              if 'APP_ORIGIN' in service.get('environment', {})}
    if actual != SERVICES:
        raise ValueError('Unexpected frontend-origin service inventory')
    changed = []
    for name in sorted(SERVICES):
        env = services[name]['environment']
        if env.get('APP_ORIGIN') not in {PORTAL, APP} or env.get('PORTAL_ORIGIN') != PORTAL or env.get('CAPTURE_ORIGIN') not in {PORTAL, APP}:
            raise ValueError('Unexpected existing frontend origins: '+name)
        for key in ['APP_ORIGIN','CAPTURE_ORIGIN']:
            if env[key] != APP:
                env[key] = APP
                changed.append(name+'.'+key)
    env = services['minio']['environment']
    origins = env.get('MINIO_API_CORS_ALLOW_ORIGIN','').split(',')
    if set(origins) not in [{PORTAL}, {PORTAL, APP}]:
        raise ValueError('Unexpected existing storage origins')
    if set(origins) != {PORTAL, APP}:
        env['MINIO_API_CORS_ALLOW_ORIGIN'] = ','.join([PORTAL, APP])
        changed.append('minio.MINIO_API_CORS_ALLOW_ORIGIN')
    return result, changed

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--compose', type=Path, required=True)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--backup-dir', type=Path)
    args = parser.parse_args()
    path = args.compose.resolve(strict=True)
    raw = path.read_bytes()
    value, changed = migrate(json.loads(raw))
    digest = hashlib.sha256(raw).hexdigest()
    if args.apply and changed:
        if args.backup_dir is None:
            raise ValueError('Explicit backup directory required')
        args.backup_dir.mkdir(parents=True, exist_ok=True)
        backup = args.backup_dir / ('compose-before-app-origin-'+digest+'.json')
        if backup.exists():
            if backup.read_bytes() != raw:
                raise ValueError('Backup identity mismatch')
        else:
            with backup.open('xb') as f:
                os.chmod(backup, 0o600);f.write(raw);f.flush();os.fsync(f.fileno())
        if path.read_bytes() != raw:
            raise ValueError('Concurrent compose modification')
        fd, pending = tempfile.mkstemp(prefix='.compose-origins-', dir=path.parent)
        try:
            with os.fdopen(fd,'wb') as f:
                os.fchmod(f.fileno(), path.stat().st_mode & 0o777)
                f.write((json.dumps(value, indent=2)+'\n').encode());f.flush();os.fsync(f.fileno())
            os.replace(pending,path)
        finally:
            if os.path.exists(pending):os.unlink(pending)
    print(json.dumps({'applied':bool(args.apply and changed),'changed_fields':changed,
                      'before_sha256':digest,'after_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),
                      'public_app_origin':APP,'public_portal_origin':PORTAL}))

if __name__ == '__main__':main()
