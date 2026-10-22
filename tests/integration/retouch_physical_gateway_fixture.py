"""Disposable SQL/MinIO transport for the real TaskArtifacts implementation.

Exercises live gateway SQL and versioned bytes. Does not qualify HTTP/mTLS:
certificate hash is explicitly provisioned fixture identity, not a TLS assertion.
"""
from contextlib import contextmanager
from hashlib import sha256
import os
import stat
from uuid import uuid4

from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.gateway import ArtifactMetadata, GatewayError


class PhysicalInputStream:
    def __init__(self, response, size, metadata, check_active):
        self.response, self.size, self.metadata = response, size, metadata
        self.check_active = check_active
        self.remaining = size
        self.closed = False

    def read(self, size=-1):
        if self.closed or type(size) is not int or size < 0:
            raise ValueError('A live bounded input read is required')
        self.check_active()
        raw = self.response.read(min(size, self.remaining))
        if size and self.remaining and not raw:
            raise GatewayError('ARTIFACT_SIZE_MISMATCH')
        self.remaining -= len(raw)
        self.check_active()
        return raw

    def close(self):
        if not self.closed:
            self.closed = True
            self.response.close()
            self.response.release_conn()

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()


class PhysicalGateway:
    def __init__(self, db, objects, context, certificate_sha):
        self.db, self.client, self.bucket = db, objects[0], objects[1]
        self.context, self.certificate_sha = context, certificate_sha

    def _identity(self):
        lease = self.context.lease
        return (lease.command.task_id, lease.command.attempt, lease.fence,
                lease.worker_id, self.certificate_sha)

    def _query(self, sql, args):
        self.context.check_active()
        with self.db.transaction():
            self.db.execute('SET LOCAL ROLE photo_gateway')
            try:
                with self.db.transaction():
                    row = self.db.execute(sql, args).fetchone()
            finally:
                self.db.execute('RESET ROLE')
        self.context.check_active()
        return row

    def _authorize(self):
        row = self._query('SELECT tenant_id,run_id FROM photo.authorize_gateway_task(%s,%s,%s,%s,%s)',
                          self._identity())
        command = self.context.lease.command
        if row != (command.tenant_id, command.run_id):
            raise GatewayError('LEASE_LOST')

    def open_stream(self, ref):
        self._authorize()
        row = self._query('SELECT artifact_id,bucket,object_key,object_version,sha256,bytes,media_type '
                          'FROM photo.gateway_input(%s,%s,%s,%s,%s,%s)',
                          (*self._identity(), ref.artifact_id))
        if row is None or row[0] != ref.artifact_id or row[1] != self.bucket or row[4] != ref.sha256:
            raise GatewayError('ARTIFACT_MISSING')
        if row[3] in (None, '', 'null'):
            raise GatewayError('GATEWAY_RESPONSE_INVALID')
        response = self.client.get_object(row[1], row[2], version_id=row[3])
        metadata = ArtifactMetadata(row[5], row[6], sha256(row[3].encode()).hexdigest())
        return PhysicalInputStream(response, row[5], metadata, self.context.check_active)

    def metadata(self, ref):
        with self.open_stream(ref) as stream:
            return stream.metadata

    def upload(self, path, role, media_type, *, expected_identity=None):
        self._authorize()
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            before = os.fstat(source.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
                raise GatewayError('ARTIFACT_CHANGED')
            if expected_identity is not None and (before.st_dev, before.st_ino) != expected_identity:
                raise GatewayError('ARTIFACT_CHANGED')
            digest = sha256()
            while block := source.read(1024 * 1024):
                self.context.check_active()
                digest.update(block)
            checksum = digest.hexdigest()
            ticket = self._query('SELECT outcome,upload_id,artifact_id,object_key,sha256 '
                'FROM photo.begin_output(' + ','.join(['%s'] * 11) + ')',
                (*self._identity(), uuid4(), role, before.st_size, checksum, media_type, self.bucket))
            if ticket is None or ticket[0] != 'created' or ticket[4] != checksum:
                raise GatewayError('OUTPUT_RESERVATION_REFUSED')
            source.seek(0)
            uploaded = self.client.put_object(self.bucket, ticket[3], source,
                                              before.st_size, content_type=media_type)
            after = os.fstat(source.fileno())
            if (before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_size, after.st_mtime_ns, after.st_ctime_ns):
                raise GatewayError('ARTIFACT_CHANGED')
            if uploaded.version_id in (None, '', 'null'):
                raise GatewayError('GATEWAY_RESPONSE_INVALID')
            # Read the exact uploaded version before declaring the SQL output ready.
            response = self.client.get_object(self.bucket, ticket[3], version_id=uploaded.version_id)
            observed, total = sha256(), 0
            try:
                while block := response.read(1024 * 1024):
                    self.context.check_active()
                    total += len(block)
                    observed.update(block)
            finally:
                response.close()
                response.release_conn()
            if total != before.st_size or observed.hexdigest() != checksum:
                raise GatewayError('ARTIFACT_HASH_MISMATCH')
            completed = self._query('SELECT outcome,artifact_id,sha256 FROM photo.complete_output('
                + ','.join(['%s'] * 10) + ')', (*self._identity(), ticket[1], uploaded.version_id,
                    total, checksum, media_type))
            if completed != ('committed', ticket[2], checksum):
                raise GatewayError('OUTPUT_COMPLETION_REFUSED')
            return ArtifactRef(artifact_id=ticket[2], sha256=checksum)


@contextmanager
def physical_task_artifacts(db, objects, context, root):
    """Provision fixture certificate identity and load the production V2 index."""
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.scratch import ScratchSpace
    certificate_sha = sha256(b'retouch-disposable-gateway-' + uuid4().bytes).hexdigest()
    command = context.lease.command
    db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s '
               'WHERE tenant_id=%s AND worker_id=%s',
               (certificate_sha, command.tenant_id, context.lease.worker_id))
    gateway = PhysicalGateway(db, objects, context, certificate_sha)
    scratch = ScratchSpace(root, command.tenant_id, command.task_id, command.attempt,
        context.lease.execution_profile['resource_estimate']['scratch_bytes'])
    with TaskArtifacts(context, gateway, scratch) as artifacts:
        artifacts.load_v2_index()
        yield artifacts
