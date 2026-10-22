"""Durable operator publication journal and fail-closed physical retention.

Withdraw the advertised checkpoint before deleting any payload. Crashes retain
withdrawn bytes for retry; prerequisites are never removed while a visible
dependent checkpoint remains. pgBackRest repository pruning is separate and
must not run independently of this journal.
"""
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
from hashlib import sha256
import io
import os
from pathlib import Path
import sqlite3
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from .backup_retention import BackupRetentionPolicy, PublishedBackup, plan_backup_retention


@contextmanager
def operator_catalogue(root, *, check_active):
    root = Path(root)
    if (not root.is_absolute() or root.resolve(strict=True) != root or not root.is_dir()
            or root.stat().st_mode & 0o077 or root.stat().st_uid not in (0,os.geteuid())):
        raise ValueError('BACKUP_CATALOGUE_INSTALLATION')
    lock = os.open(root / 'operator.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    connection = None
    try:
        import stat
        info=os.fstat(lock)
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_mode&0o077
                or info.st_uid not in (0,os.geteuid())):
            raise ValueError('BACKUP_CATALOGUE_LOCK_AUTHORITY')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if check_active() is False: raise RuntimeError('BACKUP_OPERATOR_INACTIVE')
        path = root / 'catalogue.sqlite'
        descriptor = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            info = os.fstat(descriptor)
            import stat
            if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_mode & 0o077
                    or info.st_uid not in (0,os.geteuid())):
                raise ValueError('BACKUP_CATALOGUE_AUTHORITY')
        finally: os.close(descriptor)
        connection = sqlite3.connect(path, timeout=5)
        connection.execute('PRAGMA journal_mode=WAL'); connection.execute('PRAGMA synchronous=FULL')
        connection.execute('CREATE TABLE IF NOT EXISTS checkpoints(id TEXT PRIMARY KEY, envelope BLOB NOT NULL, version TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN (\'published\',\'withdrawing\',\'pruned\')))')
        connection.commit()
        yield connection
        if check_active() is False: raise RuntimeError('BACKUP_OPERATOR_INACTIVE')
    finally:
        if connection is not None: connection.close()
        fcntl.flock(lock, fcntl.LOCK_UN); os.close(lock)


def record_publication(catalogue, envelope, version):
    parsed = parse_json(envelope, max_bytes=16 << 20)
    if canonical_bytes(parsed) != envelope or not version or version == 'null':
        raise ValueError('BACKUP_PUBLICATION_RECORD')
    identifier = parsed['manifest']['backup_id']
    if str(UUID(identifier)) != identifier: raise ValueError('BACKUP_PUBLICATION_RECORD')
    with catalogue:
        existing = catalogue.execute('SELECT envelope,version,state FROM checkpoints WHERE id=?', (identifier,)).fetchone()
        if existing is not None and existing != (envelope, version, 'published'):
            raise ValueError('BACKUP_PUBLICATION_CONFLICT')
        catalogue.execute('INSERT OR IGNORE INTO checkpoints VALUES(?,?,?,\'published\')', (identifier,envelope,version))


def prune_checkpoints(catalogue, client, bucket, signing_key, *, public_key_pin, check_active,
        commands, pgbackrest_config, pgbackrest_config_sha256, stanza, now=None):
    """Only authenticated operator-journal publications are deletion candidates.

    Each current checkpoint contains a full PostgreSQL base; no shared object
    payload is removed. The independent live deletion history is never pruned.
    """
    def active():
        if check_active() is False: raise RuntimeError('BACKUP_OPERATOR_INACTIVE')
    from cryptography.hazmat.primitives.serialization import Encoding,PublicFormat
    if sha256(signing_key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw)).hexdigest()!=public_key_pin:
        raise ValueError('BACKUP_PRUNE_SIGNING_KEY_PIN')
    rows = catalogue.execute('SELECT id,envelope,version,state FROM checkpoints WHERE state<>\'pruned\' ORDER BY id').fetchall()
    if len(rows) > 100_000: raise ValueError('BACKUP_CATALOGUE_LIMIT')
    records = {}; published = []
    for identifier, raw, version, state in rows:
        active()
        value = parse_json(raw, max_bytes=16 << 20)
        if canonical_bytes(value) != raw or set(value) != {'manifest','signature'}:
            raise ValueError('BACKUP_CATALOGUE_RECORD')
        manifest = value['manifest']
        signing_key.public_key().verify(bytes.fromhex(value['signature']), canonical_bytes(manifest))
        if (manifest['backup_id'] != identifier or manifest['signing_public_key_sha256'] != public_key_pin
                or manifest['postgres']['type'] != 'full'):
            raise ValueError('BACKUP_CATALOGUE_AUTHORITY')
        identifier_uuid = UUID(identifier)
        records[identifier_uuid] = (manifest, version, state)
        if state == 'published':
            published.append(PublishedBackup(identifier_uuid, datetime.fromisoformat(manifest['recovered_at']),
                sha256(canonical_bytes(manifest)).hexdigest(), (), False))
    plan = plan_backup_retention(published, BackupRetentionPolicy(),
        now=now or datetime.now(timezone.utc), check_active=active)
    candidates = set(plan.removable) | {i for i,(_,_,state) in records.items() if state == 'withdrawing'}
    retained_labels = {manifest['postgres']['label'] for i,(manifest,_,state) in records.items() if i not in candidates}
    labels = [manifest['postgres']['label'] for manifest,_,_ in records.values()]
    if len(set(labels))!=len(labels):
        raise ValueError('BACKUP_DUPLICATE_BASE_AUTHORITY')
    if any(records[i][0]['postgres']['label'] in retained_labels for i in candidates):
        raise ValueError('BACKUP_RETAINED_BASE_AUTHORITY')
    if not retained_labels and candidates:
        raise ValueError('BACKUP_PRUNE_REQUIRES_RETAINED_BASE')
    removed = []
    for identifier in sorted(candidates, key=str):
        active(); manifest, version, state = records[identifier]
        # Durable local intent precedes remote withdrawal. A crash after either
        # action is resumable without re-advertising a partly deleted point.
        with catalogue:
            catalogue.execute('UPDATE checkpoints SET state=\'withdrawing\' WHERE id=?', (str(identifier),))
        withdrawn = {'schema_version':'photo-checkpoint-withdrawn/v1','backup_id':str(identifier),
            'publication_version_id':version,'manifest_sha256':sha256(canonical_bytes(manifest)).hexdigest()}
        marker = canonical_bytes({'withdrawal':withdrawn,'signature':signing_key.sign(canonical_bytes(withdrawn)).hex()})
        key = 'checkpoints/' + str(identifier) + '/WITHDRAWN.json'
        receipt = client.put_object(bucket,key,io.BytesIO(marker),len(marker),content_type='application/json')
        active()
        if not receipt.version_id or receipt.version_id == 'null': raise ValueError('BACKUP_WITHDRAWAL_VERSION')
        response = client.get_object(bucket,key,version_id=receipt.version_id)
        try:
            if response.read(len(marker)+1) != marker: raise ValueError('BACKUP_WITHDRAWAL_READBACK')
        finally: response.close(); response.release_conn()
        active()
        client.remove_object(bucket,'checkpoints/' + str(identifier) + '/COMMITTED.json',version_id=version)
        for item in manifest['objects']:
            active()
            expected = 'checkpoints/' + str(identifier) + '/' + item['name'] + '.tar.age'
            if item['name'] not in ('minio','config','deletions') or item['key'] != expected:
                raise ValueError('BACKUP_PRUNE_OBJECT_AUTHORITY')
            client.remove_object(bucket,expected,version_id=item['version_id'])
        from .backup_host import private_file
        if sha256(private_file(pgbackrest_config)).hexdigest() != pgbackrest_config_sha256:
            raise ValueError('BACKUP_CONFIG_CHANGED')
        args = ['--config='+pgbackrest_config, '--stanza='+stanza, '--repo=1']
        before = parse_json(commands.run('pgbackrest', [*args,'--output=json','info']), max_bytes=16<<20)
        if not isinstance(before,list) or len(before)!=1 or before[0].get('name')!=stanza:
            raise ValueError('BACKUP_PRUNE_POSTGRES_CATALOGUE')
        backups = before[0].get('backup',[])
        labels = {b['label'] for b in backups}
        label = manifest['postgres']['label']
        if not retained_labels.issubset(labels):
            raise ValueError('BACKUP_RETAINED_BASE_MISSING')
        if any(b.get('label')!=label and label in (b.get('reference') or []) for b in backups):
            raise ValueError('BACKUP_EXTERNAL_DEPENDENT_BASE')
        if label in labels:
            commands.run('pgbackrest', [*args,'--set='+label,
                '--repo1-retention-full=9999999','--repo1-retention-archive=9999999','expire'])
        after = parse_json(commands.run('pgbackrest', [*args,'--output=json','info']), max_bytes=16<<20)
        remaining = {b['label'] for b in after[0].get('backup',[])}
        if label in remaining or not retained_labels.issubset(remaining):
            raise ValueError('BACKUP_PRUNE_POSTGRES_READBACK')
        active()
        with catalogue: catalogue.execute('UPDATE checkpoints SET state=\'pruned\' WHERE id=?',(str(identifier),))
        removed.append(str(identifier))
    return {'pruned':removed,'postgres_repository_pruned':True}
