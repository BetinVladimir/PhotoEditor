"""Two-phase V2 result adoption; object reads finish before the commit TX."""
from __future__ import annotations

from dataclasses import dataclass
import random
import time
from uuid import UUID

from psycopg import Connection, Error
from psycopg.types.json import Jsonb

from .adoption import AdoptionInvalid, ArtifactRef, prepare_v2_adoption
from .adoption_store import PostgresObjectEvidenceReader, VersionedObjectStore
from .capture_adoption import prepare_capture_adoption
from .capture_prepare_adoption import prepare_capture_inputs_adoption


def _capture_rejection_code(error: Error) -> str | None:
    # Classify only explicit invariant failures, never arbitrary DB messages.
    if error.sqlstate != '23514':
        return None
    return {
        'V24_IDENTITY_CONSENT_REVOKED': 'CONSENT_REVOKED',
        'V24_CAPTURE_CONTEXT_STALE': 'CAPTURE_CONTEXT_STALE',
        'CAPTURE_CONSENT_EVIDENCE_MISSING': 'CAPTURE_CONSENT_EVIDENCE_MISSING',
    }.get(error.diag.message_primary)


@dataclass(frozen=True)
class AdoptionContext:
    attempt: int
    fence: int
    result_id: UUID
    operation: str
    version: str
    root: ArtifactRef


class AdoptionCoordinator:
    def __init__(self, read_connection: Connection, write_connection: Connection,
                 objects: VersionedObjectStore, allowed_bucket: str, *,
                 layer_scratch_root=None, layer_scratch_bytes=None,
                 region_target_icc_sha=None):
        if not read_connection.autocommit or read_connection is write_connection:
            raise ValueError("Use distinct autocommit read and transactional write connections")
        self.read_connection = read_connection
        self.write_connection = write_connection
        self.objects = objects
        self.allowed_bucket = allowed_bucket
        self.layer_scratch_root = layer_scratch_root
        self.layer_scratch_bytes = layer_scratch_bytes
        if region_target_icc_sha is not None and (
                not isinstance(region_target_icc_sha, str) or len(region_target_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in region_target_icc_sha)):
            raise ValueError("Trusted region target ICC SHA must be lowercase SHA256")
        self.region_target_icc_sha = region_target_icc_sha

    def context(self, tenant_id: UUID, task_id: UUID) -> AdoptionContext:
        row = self.read_connection.execute(
            "SELECT * FROM photo.v2_adoption_context(%s,%s)", (tenant_id, task_id),
        ).fetchone()
        if row is None:
            raise AdoptionInvalid("V2_ADOPTION_CONTEXT_STALE")
        return AdoptionContext(row[0], row[1], row[2], row[3], row[4],
                               ArtifactRef(row[5], row[6]))

    def adopt(self, tenant_id: UUID, task_id: UUID) -> bool:
        context = self.context(tenant_id, task_id)
        reader = PostgresObjectEvidenceReader(
            self.read_connection, self.objects, tenant_id=tenant_id, task_id=task_id,
            attempt=context.attempt, fence=context.fence, allowed_bucket=self.allowed_bucket,
        )
        if context.operation == 'capture.prepare':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v24_capture_prepare_adoption_params(%s,%s)", (tenant_id, task_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('CAPTURE_PREPARATION_PARAMS_STALE')
            prepared = prepare_capture_inputs_adoption(task_id, reader, ArtifactRef(row[0], row[1]), context.root)
        else:
            params_ref = None
            layer_alpha_mode = "require_float32"
            if context.operation == 'region.preview':
                if self.region_target_icc_sha is None:
                    raise AdoptionInvalid('REGION_ADOPTION_ICC_REQUIRED')
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v26_region_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id, task_id, context.attempt, context.fence, context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('REGION_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
            if context.operation == 'optical.lens':
                row = self.read_connection.execute(
                    "SELECT * FROM photo.v25_lens_adoption_params(%s,%s)", (tenant_id, task_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('LENS_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
            if context.operation == 'layer.compose':
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v25_layer_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id,task_id,context.attempt,context.fence,context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('LAYER_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0],row[1])
                policy = self.read_connection.execute(
                    'SELECT photo.v25_layer_adoption_alpha_policy(%s,%s,%s,%s,%s)',
                    (tenant_id,task_id,context.attempt,context.fence,context.result_id),
                ).fetchone()
                if policy is None or policy[0] not in ('require_float32','normalize_uint16'):
                    raise AdoptionInvalid('LAYER_ADOPTION_ALPHA_POLICY_STALE')
                layer_alpha_mode = policy[0]
            prepared = prepare_v2_adoption(task_id=task_id, operation=context.operation,
                                           version=context.version, root=context.root, reader=reader, params_ref=params_ref,
                                           layer_scratch_root=self.layer_scratch_root,
                                           layer_scratch_bytes=self.layer_scratch_bytes,layer_alpha_mode=layer_alpha_mode,
                                           region_target_icc_sha=self.region_target_icc_sha)
        # The SQL function locks tenant → run → task, then rechecks every
        # relation. Any cancel, revoke or fence change during preparation
        # returns false and rolls back all links/markers together.
        params = (tenant_id, task_id, context.attempt, context.fence, context.result_id,
                  Jsonb(list(prepared.inventory)), prepared.digest)
        statement = "SELECT photo.adopt_v2_plan(%s,%s,%s,%s,%s,%s,%s)"
        if context.operation == 'capture.prepare':
            statement = "SELECT photo.adopt_capture_prepared(%s,%s,%s,%s,%s,%s,%s)"
        if context.operation == 'capture.assess':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v24_capture_adoption_params(%s,%s)", (tenant_id, task_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('CAPTURE_ADOPTION_PARAMS_STALE')
            capture = prepare_capture_adoption(reader, ArtifactRef(row[0], row[1]), context.root)
            params += (capture.params_jcs.decode('utf-8'), capture.context_jcs.decode('utf-8'),
                       capture.report_jcs.decode('utf-8'))
            statement = "SELECT photo.adopt_capture_report(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
        for attempt in range(3):
            try:
                with self.write_connection.transaction():
                    row = self.write_connection.execute(
                        statement, params,
                    ).fetchone()
                    return bool(row and row[0])
            except Error as exc:
                if context.operation == 'capture.assess':
                    rejection = _capture_rejection_code(exc)
                    if rejection is not None:
                        raise AdoptionInvalid(rejection) from exc
                if (context.operation == 'optical.lens' and exc.sqlstate == '23514'
                        and exc.diag.message_primary == 'V25_LENS_SCOPE_STALE'):
                    raise AdoptionInvalid('LENS_SCOPE_STALE') from exc
                if exc.sqlstate not in {"40P01", "40001"}:
                    raise
                if attempt == 2:
                    raise AdoptionInvalid("RETRYABLE_CONFLICT") from exc
                time.sleep(random.uniform(0.025, 0.1))
        raise AssertionError("Unreachable adoption retry state")
