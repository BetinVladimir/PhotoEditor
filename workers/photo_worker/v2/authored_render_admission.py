"""Exact operation/version SQL qualification witness; deliberately unmounted.

This is one conjunct of authored task admission, never a substitute for typed
instruction/source/physical closure, provider availability or adoption replay.
"""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.authored_render import decode_authored_params
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from .render_qualification_pin import RenderQualificationPin

KINDS = {('render.compose', '2'): 'final',
         ('render.authored_segment', '1'): 'segment',
         ('render.authored_geometry', '1'): 'geometry'}
FIELDS = frozenset(('schema_version', 'operation', 'operation_version', 'tenant_id',
    'run_id', 'task_id', 'worker_id', 'attempt', 'fence', 'params', 'input_digest',
    'qualification_pin'))


@dataclass(frozen=True)
class AuthoredRenderQualification:
    operation: str
    operation_version: str
    pin: RenderQualificationPin

    def __post_init__(self):
        if ((self.operation, self.operation_version) not in KINDS
                or not isinstance(self.pin, RenderQualificationPin) or self.pin.backend != 'cpu'):
            raise ValueError('AUTHORED_RENDER_QUALIFICATION_INVALID')


def decode_qualification(value, lease) -> AuthoredRenderQualification:
    """Bind the entire SQL observation to this actual claimed command/fence."""
    c = lease.command
    if not isinstance(value, dict) or set(value) != FIELDS:
        raise ValueError('AUTHORED_RENDER_QUALIFICATION_INVALID')
    if (type(value['schema_version']) is not int or value['schema_version'] != 1
            or type(value['attempt']) is not int or type(value['fence']) is not int
            or value['attempt'] != c.attempt or value['fence'] != lease.fence
            or value['operation'] != c.operation or value['operation_version'] != c.operation_version
            or value['input_digest'] != lease.input_digest
            or value['params'] != {'artifact_id': str(c.params.artifact_id), 'sha256': c.params.sha256}):
        raise ValueError('AUTHORED_RENDER_QUALIFICATION_INVALID')
    for field, expected in [('tenant_id', c.tenant_id), ('run_id', c.run_id),
                            ('task_id', c.task_id), ('worker_id', lease.worker_id)]:
        # SQL jsonb emits canonical textual UUIDs; no alternate numeric/coerced IDs.
        if not isinstance(value[field], str) or value[field] != str(expected) or UUID(value[field]).int == 0:
            raise ValueError('AUTHORED_RENDER_QUALIFICATION_INVALID')
    return AuthoredRenderQualification(c.operation, c.operation_version,
                                       RenderQualificationPin.from_value(value['qualification_pin']))


class SQLAuthoredRenderQualification:
    """Use the existing restricted TaskDatabase connection/retry/role lifecycle."""
    def __init__(self, database: TaskDatabase):
        if not isinstance(database, TaskDatabase):
            raise TypeError('TaskDatabase required')
        self.database = database

    def qualification(self, ctx, raw: bytes) -> AuthoredRenderQualification:
        ctx.check_active()
        c = ctx.lease.command
        kind = KINDS.get((c.operation, c.operation_version))
        if (kind is None or c.resource_class != 'gpu' or not isinstance(raw, bytes)
                or not 2 <= len(raw) <= 32 * 1024 * 1024 or sha256(raw).hexdigest() != c.params.sha256):
            raise OperationFailure('INPUT_INVALID')
        try:
            body = decode_authored_params(raw, kind)
            if body.tenant_id != c.tenant_id:
                raise ValueError('tenant mismatch')
            row = self.database._query(
                'SELECT photo.authored_render_worker_qualification(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (c.tenant_id, c.run_id, c.task_id, c.attempt, ctx.lease.fence,
                 ctx.lease.worker_id, c.params.artifact_id, c.params.sha256,
                 raw.decode('utf-8'), ctx.lease.input_digest))
            if not isinstance(row, (tuple, list)) or len(row) != 1:
                raise ValueError('invalid SQL row')
            witness = decode_qualification(row[0], ctx.lease)
        except ValueError as error:
            raise OperationFailure('INPUT_INVALID') from error
        ctx.check_active()
        return witness
