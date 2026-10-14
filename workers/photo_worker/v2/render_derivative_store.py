"""Version-pinned evidence reader for Render derivative canonical results."""
from psycopg.pq import TransactionStatus
from .adoption import AdoptionInvalid,ArtifactEvidence,ArtifactRef
from .adoption_store import PostgresObjectEvidenceReader,_Location


class PostgresRenderDerivativeReader(PostgresObjectEvidenceReader):
    def __init__(self,connection,objects,*,tenant_id,run_id,task_id,result_id,
                 variant,attempt,fence,allowed_bucket):
        super().__init__(connection,objects,tenant_id=tenant_id,task_id=task_id,
                         attempt=attempt,fence=fence,allowed_bucket=allowed_bucket)
        if variant not in ('preview1600','preview2560') or run_id.int==0 or result_id.int==0:
            raise ValueError('Invalid derivative result scope')
        self.run_id,self.result_id,self.variant=run_id,result_id,variant

    def _require_idle(self):
        # autocommit does not exclude an explicit transaction() block. Object IO
        # must not inherit a caller's primary-DB transaction or snapshot.
        info=getattr(self.connection,'info',None)
        if info is not None and info.transaction_status!=TransactionStatus.IDLE:
            raise AdoptionInvalid('RENDER_DERIVATIVE_READ_TRANSACTION_ACTIVE')

    def read_json(self,ref,max_bytes):
        self._require_idle()
        return super().read_json(ref,max_bytes)

    def iter_bytes(self,ref,max_bytes):
        # This executes when iteration starts, including a deferred generator
        # consumed after its caller has opened a transaction.
        iterator=super().iter_bytes(ref,max_bytes)
        try:
            while True:
                self._require_idle()
                try:block=next(iterator)
                except StopIteration:return
                yield block
        finally:
            iterator.close()

    def _location(self,ref):
        self._require_idle()
        if ref in self._locations:return self._locations[ref]
        self.staged_outputs()
        rows=self.connection.execute(
            'SELECT * FROM photo.v26_render_derivative_evidence(%s,%s,%s,%s,%s,%s,%s)',
            (self.tenant_id,self.run_id,self.variant,self.result_id,self.attempt,self.fence,ref.artifact_id),
        ).fetchall()
        if len(rows)!=1:raise AdoptionInvalid('RENDER_DERIVATIVE_EVIDENCE_STALE')
        row=rows[0]
        if (row[0]!=self.allowed_bucket or row[3]!=ref.sha256 or row[4]<1
                or not isinstance(row[1],str) or not row[1]
                or not isinstance(row[2],str) or row[2] in ('','null')
                or row[7] not in ('input','output')):
            raise AdoptionInvalid('RENDER_DERIVATIVE_EVIDENCE_INVALID')
        location=_Location(row[0],row[1],row[2],ArtifactEvidence(ref,row[5],row[4],row[6],row[7]))
        self._locations[ref]=location
        return location

    def staged_outputs(self):
        self._require_idle()
        rows=self.connection.execute(
            'SELECT task_id,result_id,attempt,fence,artifact_id,sha256 FROM photo.v26_render_derivative_result(%s,%s,%s)',
            (self.tenant_id,self.run_id,self.variant),
        ).fetchall()
        if len(rows)!=2 or any(row[:4]!=(self.task_id,self.result_id,self.attempt,self.fence) for row in rows):
            raise AdoptionInvalid('RENDER_DERIVATIVE_RESULT_STALE')
        refs={ArtifactRef(row[4],row[5]) for row in rows}
        if len(refs)!=2:raise AdoptionInvalid('RENDER_DERIVATIVE_RESULT_STALE')
        return refs
