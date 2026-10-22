"""Actual offline-checkpoint creation; encrypted publication precedes resume.

pgBackRest owns base/WAL encryption and retention in its independently installed
repository. This operator packages the stopped MinIO volume (including version
metadata), deployment secrets and independent deletion history. A failed command
leaves maintenance in place. It does not infer a 15-minute RPO from a daily job.
"""
from dataclasses import asdict
from datetime import datetime, timezone
from hashlib import sha256
import json
import os
from pathlib import Path
import stat
import tempfile
from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes, parse_json
from .backup_host import HostCommands, PinnedTool, ComposeMaintenance, private_file
from .deletion_history_store import DeletionHistoryStore
from .deletion_history_backup import export_deletion_history_backup
from .restore_catalogue import verify_restore_database_objects
from .restore_objects import MinioRestoreObjects


def file_digest(path, active, maximum):
    digest = sha256(); size = 0
    with open(path, 'rb') as stream:
        while chunk := stream.read(1 << 20):
            active(); size += len(chunk)
            if size > maximum:
                raise ValueError('BACKUP_ARCHIVE_LIMIT')
            digest.update(chunk)
    active()
    return size, digest.hexdigest()


def validate_tree(root, active, maximum_files=10_000_000):
    root = Path(root)
    if not root.is_absolute() or root.resolve(strict=True) != root or not root.is_dir():
        raise ValueError('BACKUP_VOLUME_PATH')
    count = 0; size = 0
    for directory, directories, files in os.walk(root, followlinks=False):
        active()
        for name in [*directories, *files]:
            info = (Path(directory) / name).lstat(); count += 1
            if (count > maximum_files or not (stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode))
                    or stat.S_ISREG(info.st_mode) and info.st_nlink != 1):
                raise ValueError('BACKUP_VOLUME_ENTRY')
            if stat.S_ISREG(info.st_mode): size += info.st_size
    return root, size, count


def load_operator_config(path):
    value = parse_json(private_file(path, maximum=1 << 20), max_bytes=1 << 20)
    keys = {'schema_version', 'tools', 'compose_file', 'compose_sha256', 'compose_rendered_sha256', 'project',
        'database', 'stanza', 'pgbackrest_config', 'pgbackrest_config_sha256',
        'minio_volume', 'minio_volume_name', 'source_endpoint', 'source_buckets',
        'offhost_endpoint', 'offhost_bucket', 'age_recipients_file',
        'age_recipients_sha256', 'signing_key_file', 'signing_public_key_sha256',
        'config_root', 'deletion_history_root', 'scratch_root', 'catalogue_root', 'maximum_archive_bytes'}
    if (not isinstance(value, dict) or set(value) != keys
            or value['schema_version'] != 'photo-backup-operator/v1'
            or not isinstance(value['tools'], dict)
            or set(value['tools']) != {'docker', 'compose', 'pgbackrest', 'age', 'tar'}
            or any(not isinstance(v, dict) or set(v) != {'path', 'sha256'} for v in value['tools'].values())
            or any(not isinstance(value[k], str) or not value[k] for k in keys - {
                'tools', 'source_buckets', 'maximum_archive_bytes'})
            or type(value['maximum_archive_bytes']) is not int
            or not 1 <= value['maximum_archive_bytes'] <= 2**53 - 1
            or not isinstance(value['source_buckets'], list)
            or not 1 <= len(value['source_buckets']) <= 256
            or any(not isinstance(x, str) for x in value['source_buckets'])
            or len(set(value['source_buckets'])) != len(value['source_buckets'])
            or value['source_endpoint'] == value['offhost_endpoint']
            or any(not 1 <= len(value[k]) <= 63 or any(c not in 'abcdefghijklmnopqrstuvwxyz0123456789_-' for c in value[k])
                for k in ('project', 'stanza', 'database', 'minio_volume_name'))):
        raise ValueError('BACKUP_OPERATOR_CONFIG')
    for name in ('pgbackrest_config', 'age_recipients_file'):
        pin = value['pgbackrest_config_sha256' if name == 'pgbackrest_config' else 'age_recipients_sha256']
        if sha256(private_file(value[name])).hexdigest() != pin:
            raise ValueError('BACKUP_CONFIG_PIN')
    import configparser
    pgconfig = configparser.ConfigParser(interpolation=None, strict=True)
    pgconfig.read_string(private_file(value['pgbackrest_config']).decode('utf-8'))
    if (not pgconfig.has_section('global')
            or pgconfig['global'].get('repo1-cipher-type') != 'aes-256-cbc'
            or pgconfig['global'].get('repo1-type') != 's3'
            or pgconfig['global'].get('repo1-s3-endpoint') != value['offhost_endpoint']
            or 'include' in pgconfig['global'] or 'include-path' in pgconfig['global']):
        raise ValueError('BACKUP_ENCRYPTED_OFFHOST_POSTGRES_REQUIRED')
    return value


def minio_client(endpoint, access, secret):
    from minio import Minio
    import urllib3
    if (not isinstance(endpoint, str) or '://' in endpoint or '/' in endpoint
            or not access or not secret):
        raise ValueError('BACKUP_TLS_ENDPOINT')
    return Minio(endpoint, access_key=access, secret_key=secret, secure=True,
        http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5, read=30),
            retries=urllib3.Retry(total=2), cert_reqs='CERT_REQUIRED'))


def database_guard(connection, database, active):
    active()
    with connection.transaction():
        connection.execute("SET LOCAL statement_timeout='5s'")
        row = connection.execute("""SELECT current_database(),
          (SELECT rolsuper FROM pg_roles WHERE rolname=current_user),
          NOT EXISTS(SELECT FROM pg_stat_activity WHERE datname=current_database()
            AND pid<>pg_backend_pid() AND backend_type='client backend')""").fetchone()
        if row != (database, True, True):
            raise ValueError('BACKUP_DATABASE_NOT_DRAINED')
    active()


def create_checkpoint(config, connection, source, offhost, *, check_active):
    from .backup_catalogue import operator_catalogue
    with operator_catalogue(config['catalogue_root'], check_active=check_active) as catalogue:
        return _create_checkpoint(config, connection, source, offhost,
            catalogue=catalogue, check_active=check_active)


def _create_checkpoint(config, connection, source, offhost, *, catalogue, check_active):
    """Concrete production transport, not a deferred callback installation."""
    commands = HostCommands({k: PinnedTool(**v) for k, v in config['tools'].items()}, check_active=check_active)
    active = commands.active
    def pinned_configuration():
        active()
        for path_key, hash_key in (('pgbackrest_config', 'pgbackrest_config_sha256'),
            ('age_recipients_file', 'age_recipients_sha256')):
            if sha256(private_file(config[path_key])).hexdigest() != config[hash_key]:
                raise ValueError('BACKUP_CONFIG_CHANGED')
    pinned_configuration()
    maintenance = ComposeMaintenance(commands, config['compose_file'], config['compose_sha256'], config['project'],config['compose_rendered_sha256'])
    checkpoint_id = uuid4()
    root = Path(config['scratch_root'])
    if not root.is_absolute() or root.resolve(strict=True) != root or root.stat().st_mode & 0o077:
        raise ValueError('BACKUP_SCRATCH_AUTHORITY')
    from cryptography.hazmat.primitives.serialization import load_pem_private_key, Encoding, PublicFormat
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    key = load_pem_private_key(private_file(config['signing_key_file']), password=None)
    if (not isinstance(key, Ed25519PrivateKey)
            or sha256(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)).hexdigest() != config['signing_public_key_sha256']):
        raise ValueError('BACKUP_SIGNING_KEY_PIN')
    # Confirm the configured host volume is the actual Compose MinIO mount.
    volume = parse_json(commands.run('docker', ['volume', 'inspect', config['minio_volume_name']]), max_bytes=1 << 20)
    if (not isinstance(volume, list) or len(volume) != 1
            or volume[0].get('Mountpoint') != config['minio_volume']):
        raise ValueError('BACKUP_VOLUME_BINDING')
    containers = maintenance.command('ps', '--quiet', 'minio').decode().splitlines()
    if len(containers) != 1:
        raise ValueError('BACKUP_MINIO_CONTAINER')
    inspected = parse_json(commands.run('docker', ['inspect', containers[0]]), max_bytes=1 << 20)
    mounts = inspected[0].get('Mounts', []) if isinstance(inspected, list) and len(inspected) == 1 else []
    if not any(x.get('Type') == 'volume' and x.get('Name') == config['minio_volume_name']
        and x.get('Source') == config['minio_volume'] and x.get('Destination') == '/data' for x in mounts):
        raise ValueError('BACKUP_MINIO_MOUNT')
    maintenance.pause()
    rendered = parse_json(maintenance.command('config','--format','json'),max_bytes=16<<20)
    storage_images = {name:rendered['services'][name]['image'] for name in ('postgres','minio')}
    database_guard(connection, config['database'], active)
    with connection.transaction():
        system_id = connection.execute('SELECT system_identifier::text FROM pg_control_system()').fetchone()[0]
        wal = dict(connection.execute("SELECT name,setting FROM pg_settings WHERE name IN ('archive_mode','archive_timeout','archive_command')").fetchall())
        import shlex
        expected_archive = [config['tools']['pgbackrest']['path'],
            '--config='+config['pgbackrest_config'], '--stanza='+config['stanza'], 'archive-push', '%p']
        if (wal.get('archive_mode') not in ('on','always')
                or not str(wal.get('archive_timeout','')).isdigit()
                or not 1 <= int(wal['archive_timeout']) <= 900
                or shlex.split(wal.get('archive_command','')) != expected_archive):
            raise ValueError('BACKUP_CONTINUOUS_WAL_INSTALLATION')
        from .restore_keys import _SQL as key_versions_sql
        key_versions = [list(x) for x in connection.execute(key_versions_sql+' LIMIT 4097').fetchall()]
        if len(key_versions)>4096:raise ValueError('BACKUP_KEY_VERSION_LIMIT')
    adapter = MinioRestoreObjects(source, config['source_buckets'], check_active=active)
    inventory = verify_restore_database_objects(connection, adapter.open_version, check_active=active)
    maintenance.assert_paused()
    database_guard(connection, config['database'], active)
    started = datetime.now(timezone.utc)
    pgargs = ['--config=' + config['pgbackrest_config'], '--stanza=' + config['stanza']]
    commands.run('pgbackrest', [*pgargs, '--type=full', '--no-expire-auto', 'backup'])
    pinned_configuration()
    info = parse_json(commands.run('pgbackrest', [*pgargs, '--output=json', 'info']), max_bytes=16 << 20)
    if not isinstance(info, list) or len(info) != 1 or info[0].get('name') != config['stanza']:
        raise ValueError('BACKUP_POSTGRES_RECEIPT')
    backups = info[0].get('backup', [])
    base = backups[-1] if backups else None
    if (not isinstance(base, dict) or base.get('type') != 'full'
            or base.get('timestamp', {}).get('start', 0) < int(started.timestamp())
            or not base.get('lsn', {}).get('stop') or not base.get('archive', {}).get('stop')):
        raise ValueError('BACKUP_POSTGRES_CHECKPOINT')
    # An explicit restore point after the base backup avoids assuming the
    # backup-end byte boundary is itself a replayable target record.
    point_name='photo_checkpoint_'+checkpoint_id.hex
    with connection.transaction():
        point_lsn=connection.execute('SELECT pg_create_restore_point(%s)::text',(point_name,)).fetchone()[0]
        required_wal=connection.execute('SELECT pg_walfile_name(%s::pg_lsn)',(point_lsn,)).fetchone()[0]
        wal_bytes=connection.execute("SELECT pg_size_bytes(current_setting('wal_segment_size'))").fetchone()[0]
        connection.execute('SELECT pg_switch_wal()')
    import time
    deadline=time.monotonic()+300
    while True:
        active(); maintenance.assert_paused(); database_guard(connection,config['database'],active)
        with connection.transaction():
            archived=connection.execute('SELECT last_archived_wal FROM pg_stat_archiver').fetchone()[0]
        if isinstance(archived,str) and archived[:8]==required_wal[:8] and archived>=required_wal:break
        if time.monotonic()>=deadline:raise TimeoutError('BACKUP_RECOVERY_POINT_WAL_TIMEOUT')
        time.sleep(.25)
    with tempfile.TemporaryDirectory(prefix='checkpoint-wal-',dir=root) as wal_root:
        wal_path=Path(wal_root)/required_wal
        commands.run('pgbackrest',[*pgargs,'--repo=1','archive-get',required_wal,str(wal_path)],
            output_path=wal_path,maximum_path_bytes=wal_bytes)
        verified_wal_bytes,wal_sha=file_digest(wal_path,active,wal_bytes)
        if verified_wal_bytes!=wal_bytes:raise ValueError('BACKUP_RECOVERY_POINT_WAL_SIZE')
    recovery_target={'name':point_name,'lsn':point_lsn,'wal':required_wal,
        'timeline':int(required_wal[:8],16),'wal_bytes':wal_bytes,'wal_sha256':wal_sha}
    maintenance.assert_paused()
    database_guard(connection, config['database'], active)
    maintenance.stop_objects()
    # Version IDs are part of MinIO's internal volume metadata. Never re-PUT
    # objects during restore, which would silently mint different version IDs.
    validate_tree(config['minio_volume'], active)
    validate_tree(config['config_root'], active)
    with tempfile.TemporaryDirectory(prefix='checkpoint-', dir=root) as temporary:
        temporary = Path(temporary)
        history = DeletionHistoryStore(config['deletion_history_root'], check_active=active)
        deletion = export_deletion_history_backup(history, temporary, checkpoint_id, check_active=active)
        checkpoint = history.latest_verified_checkpoint()
        if checkpoint != deletion.checkpoint:
            raise ValueError('BACKUP_DELETION_CHECKPOINT_CHANGED')
        objects = []
        for name, directory in (('minio', config['minio_volume']), ('config', config['config_root']), ('deletions', deletion.directory)):
            import shutil
            _, source_bytes, entries = validate_tree(directory, active)
            estimate = source_bytes + entries*2048 + (16<<20)
            if source_bytes > config['maximum_archive_bytes'] or shutil.disk_usage(temporary).free < estimate*2:
                raise ValueError('BACKUP_SCRATCH_CAPACITY')
            archive = temporary / (name + '.tar')
            with open(archive, 'xb') as output:
                commands.run('tar', ['--create', '--file=-', '--directory=' + str(directory), '.'],
                    output=output, maximum_file_bytes=config['maximum_archive_bytes'])
                output.flush(); os.fsync(output.fileno())
            file_digest(archive, active, config['maximum_archive_bytes'])
            encrypted = temporary / (name + '.tar.age')
            pinned_configuration()
            commands.run('age', ['--encrypt', '--recipients-file', config['age_recipients_file'],
                '--output', str(encrypted), str(archive)], output_path=encrypted,
                maximum_path_bytes=config['maximum_archive_bytes'])
            size, digest = file_digest(encrypted, active, config['maximum_archive_bytes'])
            object_key = 'checkpoints/' + str(checkpoint_id) + '/' + encrypted.name
            with open(encrypted, 'rb') as stream:
                uploaded = offhost.put_object(config['offhost_bucket'], object_key, stream, size,
                    content_type='application/octet-stream')
            active()
            if not uploaded.version_id or uploaded.version_id=='null':
                raise ValueError('BACKUP_OFFHOST_VERSIONING_REQUIRED')
            # Independently read the exact remote version before publication.
            response = offhost.get_object(config['offhost_bucket'], object_key, version_id=uploaded.version_id)
            try:
                if response.headers.get('x-amz-version-id')!=uploaded.version_id:
                    raise ValueError('BACKUP_OFFHOST_VERSION')
                digest_remote = sha256(); count = 0
                while chunk := response.read(1 << 20):
                    active(); count += len(chunk)
                    if count > size: raise ValueError('BACKUP_OFFHOST_SIZE')
                    digest_remote.update(chunk)
                if count != size or digest_remote.hexdigest() != digest:
                    raise ValueError('BACKUP_OFFHOST_INTEGRITY')
            finally:
                response.close(); response.release_conn()
            objects.append({'name': name, 'key': object_key, 'version_id': uploaded.version_id, 'bytes': size, 'sha256': digest})
            archive.unlink(); encrypted.unlink()
        active(); maintenance.assert_paused(); database_guard(connection, config['database'], active)
        if history.latest_verified_checkpoint() != checkpoint:
            raise ValueError('BACKUP_DELETION_CHECKPOINT_CHANGED')
        manifest = {'schema_version': 'photo-checkpoint/v1', 'backup_id': str(checkpoint_id),
            'recovered_at': started.isoformat(), 'database': config['database'],
            'source_project': config['project'],
            'system_identifier': system_id, 'key_versions': key_versions,
            'storage_images':storage_images,
            'postgres': base, 'inventory': asdict(inventory.inventory),
            'recovery_target':recovery_target,
            'deletion_checkpoint': asdict(checkpoint), 'objects': objects,
            'signing_public_key_sha256': config['signing_public_key_sha256'],
            'compose_sha256': config['compose_sha256'], 'tools': config['tools'],
            'compose_rendered_sha256':config['compose_rendered_sha256'],
            'pgbackrest_config_sha256': config['pgbackrest_config_sha256'],
            'age_recipients_sha256': config['age_recipients_sha256']}
        raw = canonical_bytes(manifest)
        envelope = canonical_bytes({'manifest': manifest, 'signature': key.sign(raw).hex()})
        import io
        receipt = offhost.put_object(config['offhost_bucket'], 'checkpoints/' + str(checkpoint_id) + '/COMMITTED.json',
            io.BytesIO(envelope), len(envelope), content_type='application/json')
        active()
        if not receipt.version_id or receipt.version_id=='null':
            raise ValueError('BACKUP_PUBLICATION_VERSIONING_REQUIRED')
        response = offhost.get_object(config['offhost_bucket'],
            'checkpoints/' + str(checkpoint_id) + '/COMMITTED.json', version_id=receipt.version_id)
        try:
            remote = response.read(len(envelope) + 1)
            active()
            if remote != envelope or response.headers.get('x-amz-version-id') != receipt.version_id:
                raise ValueError('BACKUP_PUBLICATION_READBACK')
        finally:
            response.close(); response.release_conn()
        from .backup_catalogue import record_publication
        record_publication(catalogue, envelope, receipt.version_id)
        # Resume is after the final signed publication; failures above have no
        # automatic compensation and leave the service group paused.
        maintenance.resume_after_publication()
        active()
        return {'backup_id': str(checkpoint_id), 'manifest_sha256': sha256(raw).hexdigest(),
            'publication_version_id': receipt.version_id, 'state': 'published',
            'checkpoint_age_seconds':max(0,int((datetime.now(timezone.utc)-started).total_seconds()))}


def checkpoint_freshness(config, *, check_active, maximum_age_seconds=900):
    from .backup_catalogue import operator_catalogue
    from cryptography.hazmat.primitives.serialization import load_pem_private_key
    if type(maximum_age_seconds) is not int or not 1<=maximum_age_seconds<=900:
        raise ValueError('BACKUP_FRESHNESS_POLICY')
    key = load_pem_private_key(private_file(config['signing_key_file']),password=None)
    from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
    if sha256(key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw)).hexdigest()!=config['signing_public_key_sha256']:
        raise ValueError('BACKUP_SIGNING_KEY_PIN')
    with operator_catalogue(config['catalogue_root'],check_active=check_active) as catalogue:
        rows = catalogue.execute("SELECT envelope FROM checkpoints WHERE state='published'").fetchall()
        if len(rows)>100_000:raise ValueError('BACKUP_CATALOGUE_LIMIT')
        dates=[]
        for (raw,) in rows:
            if check_active() is False:raise RuntimeError('BACKUP_OPERATOR_INACTIVE')
            value=parse_json(raw,max_bytes=16<<20)
            key.public_key().verify(bytes.fromhex(value['signature']),canonical_bytes(value['manifest']))
            date=datetime.fromisoformat(value['manifest']['recovered_at'])
            if date.tzinfo is None:raise ValueError('BACKUP_CHECKPOINT_TIME')
            dates.append(date)
        if not dates:return {'fresh':False,'checkpoint_age_seconds':None,'maximum_age_seconds':maximum_age_seconds}
        age=int((datetime.now(timezone.utc)-max(dates)).total_seconds())
        return {'fresh':0<=age<=maximum_age_seconds,'checkpoint_age_seconds':age,'maximum_age_seconds':maximum_age_seconds}


def prune_operator_checkpoints(config, client, *, check_active):
    from .backup_catalogue import operator_catalogue, prune_checkpoints
    from cryptography.hazmat.primitives.serialization import load_pem_private_key, Encoding, PublicFormat
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    key = load_pem_private_key(private_file(config['signing_key_file']), password=None)
    if (not isinstance(key, Ed25519PrivateKey)
            or sha256(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)).hexdigest() != config['signing_public_key_sha256']):
        raise ValueError('BACKUP_SIGNING_KEY_PIN')
    with operator_catalogue(config['catalogue_root'], check_active=check_active) as catalogue:
        commands = HostCommands({k: PinnedTool(**v) for k,v in config['tools'].items()}, check_active=check_active)
        return prune_checkpoints(catalogue, client, config['offhost_bucket'], key,
            public_key_pin=config['signing_public_key_sha256'], check_active=check_active,
            commands=commands, pgbackrest_config=config['pgbackrest_config'],
            pgbackrest_config_sha256=config['pgbackrest_config_sha256'], stanza=config['stanza'])
