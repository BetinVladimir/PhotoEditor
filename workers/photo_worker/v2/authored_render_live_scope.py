"""Existing installed source gates AND exact-pair qualification, unmounted.

No supplied canonical-scope object or provider boolean grants this boundary.
The existing SQL gate repeats actual original/current Params, full instruction,
predecessor, actor/session, operation-specific profile and physical input rows.
Physical content and independent compute/adoption remain separate requirements.
"""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.authored_render import decode_authored_params
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from .authored_render_admission import SQLAuthoredRenderQualification, KINDS

SQL = {
 ('render.compose','2'): 'SELECT photo.authored_render_final_worker_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
 ('render.authored_segment','1'): 'SELECT photo.authored_render_segment_worker_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
 ('render.authored_geometry','1'): 'SELECT photo.authored_render_geometry_worker_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
}


@dataclass(frozen=True)
class LiveAuthoredWitness:
    raw: bytes
    qualification: object


class SQLAuthoredLiveScope:
    def __init__(self, database):
        if not isinstance(database, TaskDatabase):
            raise TypeError('TaskDatabase required')
        self.database = database
        self.qualifications = SQLAuthoredRenderQualification(database)

    def admit(self, ctx, raw: bytes) -> LiveAuthoredWitness:
        ctx.check_active()
        c, lease = ctx.lease.command, ctx.lease
        pair = (c.operation,c.operation_version)
        if (pair not in SQL or c.resource_class!='gpu' or not isinstance(raw,bytes)
                or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=c.params.sha256):
            raise OperationFailure('AUTHORED_SOURCE_SCOPE_STALE')
        try:
            params = decode_authored_params(raw,KINDS[pair])
            if params.tenant_id!=c.tenant_id or canonical_bytes(params)!=raw:
                raise ValueError('noncanonical or foreign Params')
        except ValueError as e:
            raise OperationFailure('AUTHORED_SOURCE_SCOPE_STALE') from e
        row = self.database._query(SQL[pair],(c.tenant_id,c.run_id,c.task_id,c.attempt,
            lease.fence,lease.worker_id,c.params.artifact_id,c.params.sha256,
            raw.decode('utf-8'),lease.input_digest))
        if not isinstance(row,(tuple,list)) or len(row)!=1 or row[0] is not True:
            raise OperationFailure('AUTHORED_SOURCE_SCOPE_STALE')
        qualification = self.qualifications.qualification(ctx,raw)
        profile = lease.execution_profile
        if (not isinstance(profile,dict) or profile.get('operation')!=c.operation
                or profile.get('operation_version')!=c.operation_version
                or profile.get('native_qualification_pin')!=qualification.pin.__dict__):
            raise OperationFailure('AUTHORED_SOURCE_PROFILE_CHANGED')
        ctx.check_active()
        return LiveAuthoredWitness(raw,qualification)

    def checkpoint(self,ctx,witness):
        if type(witness) is not LiveAuthoredWitness:
            raise OperationFailure('AUTHORED_SOURCE_WITNESS_REQUIRED')
        if self.admit(ctx,witness.raw)!=witness:
            raise OperationFailure('AUTHORED_SOURCE_SCOPE_CHANGED')

    @contextmanager
    def current(self,ctx,raw):
        """Surround all owned physical IO/compute; late failure cannot succeed."""
        witness=self.admit(ctx,raw)
        try:
            yield witness
        except BaseException as original:
            try:self.checkpoint(ctx,witness)
            except BaseException as late:
                raise BaseExceptionGroup('authored operation and authority failures',[original,late])
            raise
        else:
            self.checkpoint(ctx,witness)
