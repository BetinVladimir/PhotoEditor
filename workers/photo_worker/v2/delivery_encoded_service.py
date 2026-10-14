"""Two-phase delivery encode preparation bound to current SQL result context."""
from dataclasses import dataclass
from uuid import UUID
from .adoption import AdoptionInvalid, ArtifactRef
from .delivery_encoded_prepare import PreparedDeliveryEncode, prepare_delivery_encode
from .delivery_encoded_reader import DeliveryEncodeEvidenceReader


@dataclass(frozen=True)
class DeliveryEncodeContext:
    attempt: int
    fence: int
    result_id: UUID
    root: ArtifactRef
    params: ArtifactRef


@dataclass(frozen=True)
class PreparedCurrentDeliveryEncode:
    context: DeliveryEncodeContext
    evidence: PreparedDeliveryEncode
    tenant_id: UUID
    task_id: UUID


def _read_context(connection, tenant_id, task_id):
    row = connection.execute(
        'SELECT attempt,fence,result_id,operation,operation_version,root_artifact_id,root_sha256,'
        'params_artifact_id,params_sha256 FROM photo.v30_delivery_encode_context(%s,%s)',
        (tenant_id, task_id),
    ).fetchone()
    if (row is None or len(row) != 9 or type(row[0]) is not int or row[0] < 1
            or type(row[1]) is not int or row[1] < 1
            or not isinstance(row[2], UUID) or row[2].int == 0
            or row[3:5] != ('export.encode', '1')):
        raise AdoptionInvalid('DELIVERY_ENCODE_CONTEXT_INVALID')
    root = ArtifactRef.parse({'artifact_id': str(row[5]), 'sha256': row[6]})
    params = ArtifactRef.parse({'artifact_id': str(row[7]), 'sha256': row[8]})
    if root.artifact_id.int == 0 or params.artifact_id.int == 0 or root.artifact_id == params.artifact_id:
        raise AdoptionInvalid('DELIVERY_ENCODE_CONTEXT_ALIAS')
    return DeliveryEncodeContext(row[0], row[1], row[2], root, params)


def prepare_current_delivery_encode(connection, objects, *, tenant_id, task_id,
        allowed_bucket, scratch_root, max_bytes, max_pixels, check_active=lambda: None):
    """All object IO happens on an autocommit read connection.

    This returns immutable physical evidence, not a completed export. The final
    commit must bind this exact context and repeat authorization under locks.
    """
    check_active()
    if not connection.autocommit:
        raise ValueError('DELIVERY_ENCODE_REQUIRES_AUTOCOMMIT')
    for identifier in (tenant_id, task_id):
        if not isinstance(identifier, UUID) or identifier.int == 0:
            raise ValueError('DELIVERY_ENCODE_SCOPE_INVALID')
    context = _read_context(connection, tenant_id, task_id)
    reader = DeliveryEncodeEvidenceReader(connection, objects, tenant_id=tenant_id,
        task_id=task_id, attempt=context.attempt, fence=context.fence,
        result_id=context.result_id, allowed_bucket=allowed_bucket)
    prepared = prepare_delivery_encode(reader, context.params, context.root,
        scratch_root=scratch_root, max_bytes=max_bytes, max_pixels=max_pixels,
        check_active=check_active)
    check_active()
    if _read_context(connection, tenant_id, task_id) != context:
        raise AdoptionInvalid('DELIVERY_ENCODE_CONTEXT_CHANGED')
    check_active()
    return PreparedCurrentDeliveryEncode(context, prepared, tenant_id, task_id)


def _commit_prepared_delivery_encode(connection, *, tenant_id, task_id,
        prepared: PreparedCurrentDeliveryEncode, statement, check_active=lambda: None):
    """Caller owns the transaction; no object IO or commit occurs here.

    The receipt records verification only. It does not finalize the export, item,
    task adoption or run. Any exception must roll back the caller's transaction.
    """
    check_active()
    if not isinstance(prepared, PreparedCurrentDeliveryEncode):
        raise ValueError('DELIVERY_ENCODE_PREPARED_REQUIRED')
    if prepared.tenant_id != tenant_id or prepared.task_id != task_id:
        raise AdoptionInvalid('DELIVERY_ENCODE_PREPARED_SCOPE_CHANGED')
    context, evidence = prepared.context, prepared.evidence
    if context.root != evidence.manifest_ref:
        raise AdoptionInvalid('DELIVERY_ENCODE_PREPARED_ROOT_CHANGED')
    # Tenant scope is transaction-local; caller owns rollback/commit. No
    # metadata mode is selected from client input or the physical manifest.
    connection.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
    row = connection.execute(
        statement,
        (tenant_id, task_id, context.attempt, context.fence, context.result_id,
         evidence.params_jcs.decode('utf-8'), evidence.source_jcs.decode('utf-8'),
         evidence.manifest_jcs.decode('utf-8'), evidence.raster.bytes),
    ).fetchone()
    if row is None or len(row) != 1 or row[0] is not True:
        raise AdoptionInvalid('DELIVERY_ENCODE_VERIFICATION_REFUSED')


def record_prepared_delivery_encode(connection, *, tenant_id, task_id, prepared, check_active=lambda: None):
    """Record verification only in the caller's transaction, without object IO."""
    return _commit_prepared_delivery_encode(connection, tenant_id=tenant_id, task_id=task_id,
        prepared=prepared, check_active=check_active,
        statement='SELECT photo.record_v30_delivery_encode_verification(%s,%s,%s,%s,%s,%s,%s,%s,%s)')


def adopt_prepared_delivery_encode(connection, *, tenant_id, task_id, prepared, check_active=lambda: None):
    """Adopt strip export or open metadata barrier in caller-owned TX."""
    return _commit_prepared_delivery_encode(connection, tenant_id=tenant_id, task_id=task_id,
        prepared=prepared, check_active=check_active,
        statement='SELECT photo.adopt_v30_delivery_encode(%s,%s,%s,%s,%s,%s,%s,%s,%s)')


class DeliveryEncodeAdoptionService:
    """Prepare on read connection, then atomically adopt on write connection."""
    def __init__(self, read_connection, write_connection, objects, *, allowed_bucket,
            scratch_root, max_bytes, max_pixels, check_active=lambda: None):
        from pathlib import Path
        if (not read_connection.autocommit or not write_connection.autocommit
                or read_connection is write_connection):
            raise ValueError('DELIVERY_ENCODE_CONNECTIONS_INVALID')
        if (not isinstance(scratch_root, Path) or scratch_root.is_symlink()
                or not scratch_root.is_dir() or not allowed_bucket
                or type(max_bytes) is not int or not 1 <= max_bytes <= 2**53-1
                or type(max_pixels) is not int or not 1 <= max_pixels <= 200000000
                or not callable(check_active)):
            raise ValueError('DELIVERY_ENCODE_INSTALLATION_INVALID')
        self.read_connection = read_connection
        self.write_connection = write_connection
        self.objects = objects
        self.options = dict(allowed_bucket=allowed_bucket, scratch_root=scratch_root,
            max_bytes=max_bytes, max_pixels=max_pixels, check_active=check_active)
        self.check_active = check_active

    def adopt(self, tenant_id, task_id):
        self.check_active()
        prepared = prepare_current_delivery_encode(self.read_connection, self.objects,
            tenant_id=tenant_id, task_id=task_id, **self.options)
        self.check_active()
        with self.write_connection.transaction():
            self.check_active()
            adopt_prepared_delivery_encode(self.write_connection, tenant_id=tenant_id,
                task_id=task_id, prepared=prepared, check_active=self.check_active)
            # Cancellation before TX exit must roll back a just-written receipt
            # and item/export update, so callers never observe partial adoption.
            self.check_active()
        return True

    def process_pending(self, limit=10):
        """Process a bounded candidate page; invalid tasks do not hide later ones.

        Rejections are returned for the scheduler's reporting/rejection policy.
        They are not terminal task transitions; SQL stores a bounded retry delay.
        Unexpected infrastructure errors and cancellation propagate immediately.
        """
        self.check_active()
        if type(limit) is not int or not 1 <= limit <= 64:
            raise ValueError('DELIVERY_ENCODE_PAGE_INVALID')
        rows = self.read_connection.execute(
            'SELECT tenant_id,task_id FROM photo.v30_delivery_pending_encodes(%s)', (limit,),
        ).fetchall()
        if len(rows) > limit:
            raise AdoptionInvalid('DELIVERY_ENCODE_CANDIDATE_PAGE_INVALID')
        seen = set()
        for row in rows:
            if (len(row) != 2 or any(not isinstance(value, UUID) or value.int == 0 for value in row)
                    or tuple(row) in seen):
                raise AdoptionInvalid('DELIVERY_ENCODE_CANDIDATE_PAGE_INVALID')
            seen.add(tuple(row))
        adopted = 0
        rejected = []
        terminal = []
        for tenant_id, task_id in rows:
            self.check_active()
            try:
                if self.adopt(tenant_id, task_id) is not True:
                    raise AdoptionInvalid('DELIVERY_ENCODE_ADOPTION_UNCONFIRMED')
                adopted += 1
            except AdoptionInvalid as error:
                self.check_active()
                if str(error) in ('DELIVERY_ENCODE_CONTEXT_INVALID',
                                  'DELIVERY_ENCODE_CONTEXT_CHANGED'):
                    # Readiness and generation drift are not immutable output
                    # defects. A later pass must reread their current scope.
                    raise
                with self.write_connection.transaction():
                    self.check_active()
                    deferred = self.write_connection.execute(
                        'SELECT photo.defer_v30_delivery_encode(%s,%s)',
                        (tenant_id, task_id),
                    ).fetchone()
                    if deferred != (True,):
                        raise AdoptionInvalid('DELIVERY_ENCODE_BACKOFF_REFUSED')
                    terminal_receipt = self.write_connection.execute(
                        'SELECT photo.reject_v30_delivery_encode_after_retries(%s,%s,%s)',
                        (tenant_id, task_id, str(error)),
                    ).fetchone()
                    if terminal_receipt not in ((True,), (False,)):
                        raise AdoptionInvalid('DELIVERY_ENCODE_TERMINAL_RECEIPT_INVALID')
                    self.check_active()
                target = terminal if terminal_receipt == (True,) else rejected
                target.append((tenant_id, task_id, str(error)))
        return DeliveryEncodePageResult(len(rows), adopted, tuple(rejected), tuple(terminal))


@dataclass(frozen=True)
class DeliveryEncodePageResult:
    candidates: int
    adopted: int
    rejected: tuple
    terminal: tuple
