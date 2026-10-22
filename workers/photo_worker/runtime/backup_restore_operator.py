"""Authenticated checkpoint materialization into empty isolated host volumes.

No restored Compose/configuration is executed. Tool and target namespace pins
come from a separate operator installation. This stage never enables writers.
"""
from hashlib import sha256
import os
from pathlib import Path, PurePosixPath
import tarfile
import tempfile
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from .backup_host import private_file, HostCommands, PinnedTool, ComposeMaintenance
from .backup_operator_lock import restore_operator_locked
from .backup_operator import file_digest


def load_restore_operator_config(path):
    value = parse_json(private_file(path, maximum=1 << 20), max_bytes=1 << 20)
    fields = {'schema_version', 'tools', 'compose_file', 'compose_sha256', 'compose_rendered_sha256', 'project',
        'pgbackrest_config', 'pgbackrest_config_sha256', 'stanza', 'scratch_root',
        'maximum_archive_bytes', 'offhost_endpoint', 'offhost_bucket', 'source_endpoint',
        'source_buckets', 'minio_target', 'config_target', 'deletions_target', 'postgres_target',
        'age_identity_file', 'age_identity_sha256', 'public_key_file', 'public_key_sha256', 'database', 'system_identifier',
        'expected_data_directory', 'journal_root', 'report_root', 'build_sha256',
        'independent_deletion_history_root', 'consent_database', 'consent_system_identifier',
        'keys_path', 'witness_path', 'witness_sha256', 'probe_config', 'probe_config_sha256',
        'probe_compose_file', 'probe_compose_sha256', 'probe_compose_rendered_sha256',
        'minio_uid', 'minio_gid', 'postgres_uid', 'postgres_gid', 'operator_state_pin'}
    if (not isinstance(value, dict) or set(value) != fields
            or value['schema_version'] != 'photo-restore-operator/v1'
            or any(not isinstance(value[x], str) or not value[x] for x in fields - {
                'tools', 'maximum_archive_bytes', 'source_buckets', 'minio_uid', 'minio_gid','postgres_uid','postgres_gid'})
            or any(type(value[x]) is not int or not 0 <= value[x] <= 2**31-1 for x in ('minio_uid','minio_gid','postgres_uid','postgres_gid'))
            or type(value['maximum_archive_bytes']) is not int
            or not 1 <= value['maximum_archive_bytes'] <= 2**53-1
            or not isinstance(value['tools'], dict)
            or set(value['tools']) != {'docker', 'compose', 'pgbackrest', 'age', 'tar'}
            or any(not isinstance(x, dict) or set(x) != {'path', 'sha256'} for x in value['tools'].values())
            or not isinstance(value['source_buckets'], list) or not value['source_buckets']
            or value['offhost_endpoint'] == value['source_endpoint']):
        raise ValueError('RESTORE_OPERATOR_CONFIG')
    return value


def assert_isolated_compose(target):
    value = parse_json(target.command('config', '--format', 'json'), max_bytes=16 << 20)
    services = value.get('services', {})
    networks = value.get('networks', {})
    # Dedicated storage-only Compose, with no published socket or external
    # network, is mechanically checked before any volume write or DB operation.
    if (set(services) != {'postgres', 'minio'} or not networks
            or any(n.get('internal') is not True or n.get('external') for n in networks.values())
            or any(s.get('ports') or s.get('network_mode') or s.get('privileged') for s in services.values())
            or target.running() - {'postgres', 'minio'}):
        raise ValueError('RESTORE_EXTERNAL_ISOLATION')
    target.commands.active()


PROBE_SERVICES = frozenset({'postgres','minio','rabbitmq','api','object-gateway',
    'scheduler','v2-adopter','publisher','worker-media','worker-cpu',
    'worker-archive','collection-worker','caddy'})


def assert_isolated_probe_compose(target):
    value = parse_json(target.command('config','--format','json'), max_bytes=16<<20)
    services = value.get('services',{}); networks = value.get('networks',{})
    if (not {'postgres','minio','rabbitmq','api','object-gateway','scheduler','v2-adopter','publisher'}.issubset(services)
            or set(services)-PROBE_SERVICES or not networks
            or any(n.get('internal') is not True or n.get('external') for n in networks.values())
            or any(s.get('ports') or s.get('network_mode') or s.get('privileged') for s in services.values())
            or target.running()-set(services)):
        raise ValueError('RESTORE_PROBE_EXTERNAL_ISOLATION')
    target.commands.active()


def assert_probe_endpoint(target, url, service, *, schemes, ports, storage_only=False):
    """Bind operator/worker transports to current internal project containers."""
    from urllib.parse import urlsplit
    if storage_only:assert_isolated_compose(target)
    else:assert_isolated_probe_compose(target)
    parsed = urlsplit(url)
    if (service not in PROBE_SERVICES or parsed.scheme not in schemes
            or not parsed.hostname or parsed.port not in ports
            or parsed.fragment):
        raise ValueError('RESTORE_PROBE_ENDPOINT')
    identifiers = target.command('ps','--quiet',service).decode('utf-8').splitlines()
    if len(identifiers)!=1: raise ValueError('RESTORE_PROBE_ENDPOINT_CONTAINER')
    rows = parse_json(target.commands.run('docker',['inspect',identifiers[0]]),max_bytes=16<<20)
    if (not isinstance(rows,list) or len(rows)!=1
            or rows[0].get('Config',{}).get('Labels',{}).get('com.docker.compose.project')!=target.project
            or rows[0].get('Config',{}).get('Labels',{}).get('com.docker.compose.service')!=service
            or rows[0].get('State',{}).get('Running') is not True):
        raise ValueError('RESTORE_PROBE_ENDPOINT_CONTAINER')
    addresses = set()
    networks = rows[0].get('NetworkSettings',{}).get('Networks',{})
    if not networks: raise ValueError('RESTORE_PROBE_ENDPOINT_NETWORK')
    for name, installed in networks.items():
        details = parse_json(target.commands.run('docker',['network','inspect',name]),max_bytes=16<<20)
        if (not isinstance(details,list) or len(details)!=1 or details[0].get('Internal') is not True
                or details[0].get('Labels',{}).get('com.docker.compose.project')!=target.project):
            raise ValueError('RESTORE_PROBE_ENDPOINT_NETWORK')
        addresses.update(x for x in (installed.get('IPAddress'),installed.get('GlobalIPv6Address'),
            *(installed.get('Aliases') or [])) if isinstance(x,str) and x)
    if parsed.hostname not in addresses: raise ValueError('RESTORE_PROBE_ENDPOINT_SCOPE')
    target.commands.active()


def bind_operator_postgres(config, target, dsn, *, control=False):
    """Prevent an operator DSN from pointing at the live original cluster."""
    from psycopg.conninfo import conninfo_to_dict
    import ipaddress
    values=conninfo_to_dict(dsn)
    host=values.get('host'); port=values.get('port')
    try:ipaddress.ip_address(host)
    except (ValueError,TypeError):raise ValueError('RESTORE_OPERATOR_DATABASE_HOST') from None
    if (port!='5432' or values.get('service') or values.get('hostaddr') not in (None,'',host)
            or not values.get('dbname')
            or control and values['dbname']==config['database']
            or not control and values['dbname']!=config['database']):
        raise ValueError('RESTORE_OPERATOR_DATABASE_SCOPE')
    assert_probe_endpoint(target,'postgresql://'+('['+host+']' if ':' in host else host)+':5432',
        'postgres',schemes={'postgresql'},ports={5432},storage_only=True)
    # Explicit hostaddr prevents an inherited libpq PGHOSTADDR from redirecting
    # the connection after validating its host. No credentials are logged.
    values['hostaddr']=host
    return values


def connect_restored_postgres(options, *, check_active, maximum_seconds=300):
    """Wait for the isolated PostgreSQL recovery/startup, retaining host lease.

    Only connection-unavailable or startup SQLSTATE errors are retryable;
    explicit authentication/database/permission errors remain immediate failures.
    No successful connection or recovered state is fabricated.
    """
    import time
    import psycopg
    if (not callable(check_active) or type(maximum_seconds) is not int
            or not 1 <= maximum_seconds <= 1200 or not isinstance(options, dict)):
        raise ValueError('RESTORE_DATABASE_READINESS_INSTALLATION')
    deadline = time.monotonic() + maximum_seconds
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        if time.monotonic() >= deadline:
            raise TimeoutError('RESTORE_DATABASE_READINESS_TIMEOUT')
    while True:
        active()
        try:
            bounded = dict(options)
            bounded['connect_timeout'] = max(1, min(5, int(deadline - time.monotonic())))
            connection = psycopg.connect(**bounded)
        except psycopg.OperationalError as exc:
            if exc.sqlstate not in (None, '57P03', '08001', '08006'):
                raise
            active()
            time.sleep(min(.25, max(0, deadline - time.monotonic())))
        else:
            try:
                active()
            except BaseException:
                connection.close()
                raise
            return connection


def start_isolated_probe_phase(config, storage_target, connection):
    """Explicit isolated fixture phase; never starts live/production services."""
    assert_isolated_compose(storage_target)
    probe = ComposeMaintenance(storage_target.commands, config['probe_compose_file'],
        config['probe_compose_sha256'], config['project'],config['probe_compose_rendered_sha256'])
    assert_isolated_probe_compose(probe)
    # Before changing DB connection policy, prove both pinned phase configs use
    # exactly the same existing storage containers/volumes, not new live mounts.
    storage = parse_json(storage_target.command('config','--format','json'), max_bytes=16<<20)
    proposed = parse_json(probe.command('config','--format','json'), max_bytes=16<<20)
    for service in ('postgres','minio'):
        a = storage['services'][service]; b = proposed['services'][service]
        if any(a.get(k)!=b.get(k) for k in ('image','volumes','environment','command','user','networks')):
            raise ValueError('RESTORE_PROBE_STORAGE_DRIFT')
    from psycopg import sql
    with connection.transaction():
        connection.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS true').format(sql.Identifier(config['database'])))
    names = sorted(set(proposed['services'])-{'postgres','minio'})
    try:
        probe.command('up','--detach','--no-deps',*names)
        assert_isolated_probe_compose(probe)
    except BaseException:
        # The caller has not received probe yet, so ownership of compensation
        # remains here. Never return an enabled fixture namespace on failure.
        stop_isolated_probe_phase(config,probe,connection)
        raise
    return probe


def stop_isolated_probe_phase(config, probe, connection):
    """Failures leave fixture writers stopped; this is not production cutover."""
    # Cancellation forbids new work, but must not prohibit stopping already
    # authorized isolated fixture services. Cleanup gets only the same pinned
    # project/config and a bounded fixed stop vector, never a start capability.
    from psycopg import sql
    try:
        cleanup_commands = HostCommands(probe.commands.tools,check_active=lambda:True,timeout=120)
        import re
        # Do not re-execute a changed Compose file. Discover only exact IDs in
        # the already validated dedicated project; repeat immutable labels and
        # stop only the fixed fixture service allowlist, via pinned local Docker.
        ids=cleanup_commands.run('docker',['ps','--all','--filter',
            'label=com.docker.compose.project='+probe.project,'--format','{{.ID}}']).decode().splitlines()
        if len(ids)>256 or any(re.fullmatch('[0-9a-f]{12,64}',identifier) is None for identifier in ids):
            raise ValueError('RESTORE_CLEANUP_CONTAINER_PROJECTION')
        stop=[];unknown=False
        for identifier in ids:
            details=parse_json(cleanup_commands.run('docker',['inspect',identifier]),max_bytes=16<<20)
            if not isinstance(details,list) or len(details)!=1:raise ValueError('RESTORE_CLEANUP_CONTAINER_PROJECTION')
            labels=details[0].get('Config',{}).get('Labels',{})
            if labels.get('com.docker.compose.project')!=probe.project:
                raise ValueError('RESTORE_CLEANUP_PROJECT')
            if labels.get('com.docker.compose.service') in PROBE_SERVICES-{'postgres','minio'}:
                stop.append(identifier)
            elif labels.get('com.docker.compose.service') not in {'postgres','minio'}:
                unknown=True
        if stop:cleanup_commands.run('docker',['stop','--time','90',*stop])
        for identifier in stop:
            details=parse_json(cleanup_commands.run('docker',['inspect',identifier]),max_bytes=16<<20)
            if (not isinstance(details,list) or len(details)!=1
                    or details[0].get('State',{}).get('Running') is not False):
                raise ValueError('RESTORE_PROBE_WRITERS_NOT_STOPPED')
        remaining=cleanup_commands.run('docker',['ps','--filter',
            'label=com.docker.compose.project='+probe.project,'--format','{{.ID}}']).decode().splitlines()
        if len(remaining)>256 or any(re.fullmatch('[0-9a-f]{12,64}',identifier) is None for identifier in remaining):
            raise ValueError('RESTORE_CLEANUP_CONTAINER_PROJECTION')
        for identifier in remaining:
            details=parse_json(cleanup_commands.run('docker',['inspect',identifier]),max_bytes=16<<20)
            if not isinstance(details,list) or len(details)!=1:
                raise ValueError('RESTORE_CLEANUP_CONTAINER_PROJECTION')
            labels=details[0].get('Config',{}).get('Labels',{})
            if (labels.get('com.docker.compose.project')!=probe.project
                    or labels.get('com.docker.compose.service') not in {'postgres','minio'}):
                raise ValueError('RESTORE_PROBE_WRITERS_NOT_STOPPED')
        if unknown:raise ValueError('RESTORE_PROBE_WRITERS_NOT_STOPPED')
    finally:
        with connection.transaction():
            connection.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(config['database'])))
            connection.execute("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=%s AND pid<>pg_backend_pid() AND backend_type='client backend'",(config['database'],))


def read_checkpoint(client, bucket, backup_id, publication_version, public_key_path, public_pin, active):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    identifier = UUID(backup_id)
    if str(identifier) != backup_id or identifier.int == 0 or not publication_version or publication_version == 'null':
        raise ValueError('RESTORE_CHECKPOINT_IDENTITY')
    public = private_file(public_key_path, maximum=32)
    if len(public) != 32 or sha256(public).hexdigest() != public_pin:
        raise ValueError('RESTORE_CHECKPOINT_PUBLIC_KEY_PIN')
    from minio.error import S3Error
    # An exact historical COMMITTED version is not sufficient after retention
    # withdrawal. Only a definitive missing-key response permits restore.
    try:
        client.stat_object(bucket, 'checkpoints/' + backup_id + '/WITHDRAWN.json')
    except S3Error as exc:
        if exc.code not in ('NoSuchKey', 'NoSuchObject'): raise
    else:
        raise ValueError('RESTORE_CHECKPOINT_WITHDRAWN')
    active()
    response = client.get_object(bucket, 'checkpoints/' + backup_id + '/COMMITTED.json', version_id=publication_version)
    try:
        raw = response.read((16 << 20) + 1); active()
        if len(raw) > 16 << 20 or response.headers.get('x-amz-version-id') != publication_version:
            raise ValueError('RESTORE_CHECKPOINT_PUBLICATION')
    finally:
        response.close(); response.release_conn()
    envelope = parse_json(raw, max_bytes=16 << 20)
    if (not isinstance(envelope, dict) or set(envelope) != {'manifest', 'signature'}
            or canonical_bytes(envelope) != raw or not isinstance(envelope['signature'], str)):
        raise ValueError('RESTORE_CHECKPOINT_ENVELOPE')
    manifest = envelope['manifest']
    Ed25519PublicKey.from_public_bytes(public).verify(bytes.fromhex(envelope['signature']), canonical_bytes(manifest))
    expected_keys={'schema_version','backup_id','recovered_at','database','source_project','system_identifier',
        'key_versions','storage_images','postgres','inventory','recovery_target','deletion_checkpoint','objects',
        'signing_public_key_sha256','compose_sha256','compose_rendered_sha256','tools',
        'pgbackrest_config_sha256','age_recipients_sha256'}
    if (not isinstance(manifest, dict) or manifest.get('schema_version') != 'photo-checkpoint/v1'
            or set(manifest)!=expected_keys
            or manifest.get('backup_id') != backup_id or manifest.get('signing_public_key_sha256') != public_pin
            or not isinstance(manifest.get('objects'), list)
            or len(manifest['objects']) != 3
            or {x.get('name') for x in manifest['objects'] if isinstance(x, dict)} != {'minio', 'config', 'deletions'}):
        raise ValueError('RESTORE_CHECKPOINT_MANIFEST')
    import re
    recovery=manifest['recovery_target']
    if (not isinstance(recovery,dict) or set(recovery)!={'name','lsn','wal','timeline','wal_bytes','wal_sha256'}
            or recovery['name']!='photo_checkpoint_'+identifier.hex
            or not isinstance(recovery['lsn'],str) or re.fullmatch('[0-9A-F]{1,8}/[0-9A-F]{1,8}',recovery['lsn']) is None
            or not isinstance(recovery['wal'],str) or re.fullmatch('[0-9A-F]{24}',recovery['wal']) is None
            or type(recovery['timeline']) is not int or recovery['timeline']!=int(recovery['wal'][:8],16)
            or type(recovery['wal_bytes']) is not int or not 1<<20<=recovery['wal_bytes']<=1<<30
            or not isinstance(recovery['wal_sha256'],str) or re.fullmatch('[0-9a-f]{64}',recovery['wal_sha256']) is None):
        raise ValueError('RESTORE_CHECKPOINT_WAL')
    for item in manifest['objects']:
        if (set(item) != {'name', 'key', 'version_id', 'bytes', 'sha256'}
                or item['key'] != 'checkpoints/' + backup_id + '/' + item['name'] + '.tar.age'
                or not isinstance(item['version_id'], str) or item['version_id'] in ('', 'null')
                or type(item['bytes']) is not int or not 0 < item['bytes'] <= 2**53-1
                or not isinstance(item['sha256'], str) or len(item['sha256']) != 64):
            raise ValueError('RESTORE_CHECKPOINT_OBJECT')
    active()
    return manifest


def extract_empty_archive(archive, destination, *, maximum_bytes, check_active, owner=None):
    """Manual regular-file extraction: never tar path/link/device semantics."""
    root = Path(destination)
    if (not root.is_absolute() or root.resolve(strict=True) != root or not root.is_dir()
            or root.stat().st_mode & 0o077 or any(root.iterdir())):
        raise ValueError('RESTORE_TARGET_NOT_EMPTY_PRIVATE')
    count = 0; total = 0; seen = set()
    if owner is not None and (not isinstance(owner,tuple) or len(owner)!=2
            or any(type(x) is not int or not 0<=x<=2**31-1 for x in owner)):
        raise ValueError('RESTORE_TARGET_OWNER')
    with tarfile.open(archive, mode='r|') as source:
        for member in source:
            if check_active() is False: raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
            path = PurePosixPath(member.name)
            if member.name in ('.', './') and member.isdir(): continue
            if (path.is_absolute() or '..' in path.parts or not path.parts
                    or not (member.isfile() or member.isdir()) or member.size < 0):
                raise ValueError('RESTORE_ARCHIVE_ENTRY')
            relative = str(path)
            count += 1; total += member.size
            if count > 10_000_000 or total > maximum_bytes or relative in seen:
                raise ValueError('RESTORE_ARCHIVE_LIMIT')
            seen.add(relative)
            target = root.joinpath(*path.parts)
            target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if member.isdir():
                target.mkdir(exist_ok=True, mode=0o700)
                continue
            reader = source.extractfile(member)
            if reader is None: raise ValueError('RESTORE_ARCHIVE_FILE')
            with reader, open(target, 'xb') as output:
                os.chmod(target, 0o600)
                left = member.size
                while left:
                    if check_active() is False: raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
                    chunk = reader.read(min(left, 1 << 20))
                    if not chunk: raise ValueError('RESTORE_ARCHIVE_TRUNCATED')
                    output.write(chunk); left -= len(chunk)
                output.flush(); os.fsync(output.fileno())
    if owner is not None:
        for directory, directories, files in os.walk(root):
            if check_active() is False: raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
            for name in files: os.chown(Path(directory)/name,*owner,follow_symlinks=False)
            os.chown(directory,*owner,follow_symlinks=False)
    if check_active() is False: raise RuntimeError('RESTORE_OPERATOR_INACTIVE')


@restore_operator_locked('config')
def restore_physical_checkpoint(config, manifest, client, *, check_active):
    commands = HostCommands({k: PinnedTool(**v) for k, v in config['tools'].items()}, check_active=check_active)
    active = commands.active
    target = ComposeMaintenance(commands, config['compose_file'], config['compose_sha256'], config['project'],config['compose_rendered_sha256'])
    if config['project'] == manifest.get('source_project'):
        raise ValueError('RESTORE_LIVE_PROJECT_FORBIDDEN')
    assert_isolated_compose(target)
    installed = parse_json(target.command('config','--format','json'), max_bytes=16<<20)
    for service, source, destination in (
            ('minio',config['minio_target'],'/data'),
            ('postgres',config['postgres_target'],config['expected_data_directory'])):
        matches = [v for v in installed['services'][service].get('volumes',[])
            if v.get('target')==destination]
        if (len(matches)!=1 or matches[0].get('type')!='bind'
                or matches[0].get('source')!=source or matches[0].get('read_only')):
            raise ValueError('RESTORE_TARGET_VOLUME_BINDING')
        if installed['services'][service].get('image')!=manifest.get('storage_images',{}).get(service):
            raise ValueError('RESTORE_STORAGE_IMAGE_MISMATCH')
    # Separate Compose project and network are mandatory installed targets.
    # Any running service makes volume mutation ambiguous: refuse, don't stop it.
    if target.running(): raise ValueError('RESTORE_TARGET_RUNNING')
    for path_key in ('minio_target', 'config_target', 'deletions_target', 'postgres_target'):
        path = Path(config[path_key])
        if (not path.is_absolute() or path.resolve(strict=True) != path or not path.is_dir()
                or path.stat().st_mode & 0o077 or any(path.iterdir())):
            raise ValueError('RESTORE_TARGET_NOT_EMPTY_PRIVATE')
    targets = [Path(config[x]) for x in ('minio_target', 'config_target', 'deletions_target', 'postgres_target')]
    if any(a == b or a in b.parents or b in a.parents for i,a in enumerate(targets) for b in targets[i+1:]):
        raise ValueError('RESTORE_TARGET_OVERLAP')
    # A fresh namespace needs actual containers before start. Creating pinned
    # stopped storage is safe only after all empty/bind/image/isolation checks.
    target.command('create','--no-build','--no-deps','postgres','minio')
    for service,source,destination in (('minio',config['minio_target'],'/data'),
            ('postgres',config['postgres_target'],config['expected_data_directory'])):
        ids=target.command('ps','--all','--quiet',service).decode().splitlines()
        if len(ids)!=1:raise ValueError('RESTORE_STORAGE_CONTAINER')
        actual=parse_json(commands.run('docker',['inspect',ids[0]]),max_bytes=16<<20)
        if (len(actual)!=1 or actual[0].get('State',{}).get('Running') is True
                or not any(v.get('Type')=='bind' and v.get('Source')==source and v.get('Destination')==destination
                    for v in actual[0].get('Mounts',[]))):
            raise ValueError('RESTORE_STORAGE_CONTAINER_BINDING')
    if sha256(private_file(config['pgbackrest_config'])).hexdigest() != config['pgbackrest_config_sha256']:
        raise ValueError('RESTORE_PGBACKREST_CONFIG_PIN')
    with tempfile.TemporaryDirectory(prefix='restore-wal-',dir=config['scratch_root']) as wal_root:
        recovery=manifest['recovery_target']; wal_path=Path(wal_root)/recovery['wal']
        commands.run('pgbackrest',['--config='+config['pgbackrest_config'],'--stanza='+config['stanza'],
            '--repo=1','archive-get',recovery['wal'],str(wal_path)],output_path=wal_path,
            maximum_path_bytes=recovery['wal_bytes'])
        if file_digest(wal_path,active,recovery['wal_bytes'])!=(recovery['wal_bytes'],recovery['wal_sha256']):
            raise ValueError('RESTORE_CHECKPOINT_WAL_INTEGRITY')
    with tempfile.TemporaryDirectory(prefix='restore-checkpoint-', dir=config['scratch_root']) as temporary:
        temporary = Path(temporary)
        for item in manifest['objects']:
            if item['bytes'] > config['maximum_archive_bytes']: raise ValueError('RESTORE_ARCHIVE_LIMIT')
            import shutil
            if shutil.disk_usage(temporary).free < item['bytes']*2+(16<<20):
                raise ValueError('RESTORE_SCRATCH_CAPACITY')
            encrypted = temporary / (item['name'] + '.age')
            response = client.get_object(config['offhost_bucket'], item['key'], version_id=item['version_id'])
            try:
                if response.headers.get('x-amz-version-id') != item['version_id']:
                    raise ValueError('RESTORE_OFFHOST_VERSION')
                with open(encrypted, 'xb') as output:
                    count = 0
                    while chunk := response.read(1 << 20):
                        active(); count += len(chunk)
                        if count > item['bytes']: raise ValueError('RESTORE_OFFHOST_SIZE')
                        output.write(chunk)
                if file_digest(encrypted, active, item['bytes']) != (item['bytes'], item['sha256']):
                    raise ValueError('RESTORE_OFFHOST_INTEGRITY')
            finally:
                response.close(); response.release_conn()
            archive = temporary / (item['name'] + '.tar')
            # Decryption identity is an independently installed privileged path,
            # never selected by the signed backup or a restored config file.
            if sha256(private_file(config['age_identity_file'])).hexdigest()!=config['age_identity_sha256']:
                raise ValueError('RESTORE_AGE_IDENTITY_PIN')
            commands.run('age', ['--decrypt', '--identity', config['age_identity_file'],
                '--output', str(archive), str(encrypted)], output_path=archive,
                maximum_path_bytes=config['maximum_archive_bytes'])
            file_digest(archive, active, config['maximum_archive_bytes'])
            destination = Path(config[item['name'] + '_target'])
            if item['name'] == 'deletions':
                destination = destination / manifest['backup_id']
                destination.mkdir(mode=0o700)
            extract_empty_archive(archive, destination,
                maximum_bytes=config['maximum_archive_bytes'], check_active=active,
                owner=(config['minio_uid'],config['minio_gid']) if item['name']=='minio' else None)
            encrypted.unlink(); archive.unlink()
    base = manifest['postgres']
    label = base.get('label'); recovery=manifest.get('recovery_target',{});lsn=recovery.get('lsn')
    import re
    if (not isinstance(label, str) or re.fullmatch(r'[0-9]{8}-[0-9]{6}F', label) is None
            or not isinstance(lsn, str) or re.fullmatch('[0-9A-F]{1,8}/[0-9A-F]{1,8}', lsn) is None
            or recovery.get('name')!='photo_checkpoint_'+UUID(manifest['backup_id']).hex
            or not isinstance(recovery.get('wal'),str) or re.fullmatch('[0-9A-F]{24}',recovery['wal']) is None
            or type(recovery.get('timeline')) is not int or recovery['timeline']!=int(recovery['wal'][:8],16)):
        raise ValueError('RESTORE_POSTGRES_CHECKPOINT')
    active()
    commands.run('pgbackrest', ['--config=' + config['pgbackrest_config'],
        '--stanza=' + config['stanza'], '--pg1-path=' + config['postgres_target'],
        '--set=' + label, '--type=name', '--target=' + recovery['name'],
        '--target-timeline='+str(recovery['timeline']),'--target-action=promote', 'restore'])
    # The exact restored PostgreSQL image and installed daemon identity must
    # agree before starting. Never discover this via a failed live startup.
    for directory,directories,files in os.walk(config['postgres_target']):
        active()
        for path in [Path(directory),*(Path(directory)/name for name in files)]:
            info=path.lstat()
            if info.st_uid!=config['postgres_uid'] or info.st_gid!=config['postgres_gid']:
                raise ValueError('RESTORE_POSTGRES_OWNER_MISMATCH')
    active()
    if target.running(): raise ValueError('RESTORE_TARGET_RUNNING')
    # Starts only isolated storage, never application/outgoing services.
    target.command('start', 'postgres', 'minio')
    active()
    return target


@restore_operator_locked('config')
def reconcile_checkpoint(config, manifest, target, connection, consent_connection, client, *, check_active):
    """Join real isolation, deletion, consent, key and compute-recovery gates."""
    from datetime import datetime
    from uuid import uuid4
    from .deletion_history_store import DeletionHistoryStore
    from .restore_consent_authority import SQLCurrentConsentAuthority
    from .restore_recovery import recover_isolated_and_report
    if not callable(check_active):
        raise ValueError('RESTORE_RECONCILE_CANCELLATION_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        target.commands.active()
        if check_active() is False:
            raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        return True
    active()
    if manifest['database'] != config['database'] or manifest['system_identifier']!=config['system_identifier']:
        raise ValueError('RESTORE_DATABASE_CHECKPOINT_MISMATCH')
    def isolation():
        active(); assert_isolated_compose(target)
        if target.running() != {'postgres', 'minio'}:
            raise ValueError('RESTORE_STORAGE_NOT_RUNNING')
        active()
    def independent_consent():
        active()
        assert_isolated_compose(target)
        with consent_connection.transaction():
            consent_connection.execute("SET LOCAL statement_timeout='5s'")
            consent_connection.execute("SET LOCAL lock_timeout='1s'")
            row = consent_connection.execute("SELECT current_database(),system_identifier::text,pg_is_in_recovery() FROM pg_control_system()").fetchone()
            if row != (config['consent_database'], config['consent_system_identifier'], False):
                raise ValueError('RESTORE_INDEPENDENT_CONSENT_AUTHORITY')
            if row[1] == manifest['system_identifier']:
                raise ValueError('RESTORE_CONSENT_RESTORED_CLUSTER_FORBIDDEN')
        active()
    # Connect first, then close all new connections. Existing SQL isolation
    # guard verifies this state and absence of any other client every phase.
    isolation()
    with connection.transaction():
        from psycopg import sql
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='1s'")
        active()
        connection.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(config['database'])))
    consent = SQLCurrentConsentAuthority(consent_connection,
        expected_database=config['consent_database'], assert_current_authority=independent_consent,
        check_active=active)
    history = DeletionHistoryStore(config['independent_deletion_history_root'], check_active=active)
    restored_history = Path(config['deletions_target']) / manifest['backup_id']
    deletion_manifest = restored_history / 'manifest.json'
    active()
    raw = private_file(deletion_manifest)
    active()
    evidence,receipt = recover_isolated_and_report(connection, client, tuple(config['source_buckets']),
        config['scratch_root'], config['journal_root'], uuid4(), restored_history,
        sha256(raw).hexdigest(), expected_database=config['database'],
        expected_system_identifier=manifest['system_identifier'],
        expected_data_directory=config['expected_data_directory'],
        assert_external_isolation=isolation, check_active=active,
        history_store=history, consent_authority=consent,
        keys_path=config['keys_path'], witness_path=config['witness_path'],
        witness_sha256=config['witness_sha256'], report_root=config['report_root'],
        build_sha256=config['build_sha256'], backup_at=datetime.fromisoformat(manifest['recovered_at']),
        last_wal_lsn=manifest['recovery_target']['lsn'])
    from .backup_cutover_operator import _exclusive_event
    # Immutable link from authenticated physical input to the actual recovery
    # report; staged approval must pin this, not infer a checkpoint from LSN.
    active()
    _exclusive_event(config['report_root'],str(evidence.preflight.run_id)+'.checkpoint.json',{
        'schema_version':'restore-materialization-receipt/v1','run_id':str(evidence.preflight.run_id),
        'recovery_sha256':receipt.sha256,'manifest':manifest,
        'manifest_sha256':sha256(canonical_bytes(manifest)).hexdigest(),
        'target_project':config['project'],'data_directory':config['expected_data_directory']})
    return evidence,receipt
