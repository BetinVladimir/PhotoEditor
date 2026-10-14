"""Two-phase encoded preview validation and fenced durable receipt."""
from psycopg.pq import TransactionStatus
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid,ArtifactRef
from .render_derivative_adoption import verify_render_derivative_result
from .render_derivative_store import PostgresRenderDerivativeReader


class RenderDerivativeAdoptionCoordinator:
    def __init__(self,read_connection,write_connection,objects,allowed_bucket,*,
                 target_icc_sha256,working_icc_sha256,check_active=lambda:None,linear_icc_sha256=None):
        if (read_connection is write_connection or not read_connection.autocommit
                or not write_connection.autocommit or not callable(check_active)):
            raise ValueError('Distinct autocommit connections and activity check required')
        for digest in (target_icc_sha256,working_icc_sha256,*((linear_icc_sha256,) if linear_icc_sha256 is not None else ())):
            if not isinstance(digest,str) or len(digest)!=64 or any(c not in '0123456789abcdef' for c in digest):
                raise ValueError('Trusted deployed ICC hashes required')
        self.read,self.write,self.objects,self.bucket=read_connection,write_connection,objects,allowed_bucket
        self.additional_working_sha=() if linear_icc_sha256 is None else (linear_icc_sha256,)
        self.target_sha,self.working_sha,self.check_active=target_icc_sha256,working_icc_sha256,check_active

    def _require_idle_connections(self):
        # autocommit still permits explicit outer transaction() blocks. A nested
        # transaction would only release a savepoint, not durably commit receipt.
        for connection in (self.read,self.write):
            info=getattr(connection,'info',None)
            if (not connection.autocommit or (info is not None
                    and info.transaction_status!=TransactionStatus.IDLE)):
                raise AdoptionInvalid('RENDER_DERIVATIVE_COORDINATOR_TRANSACTION_ACTIVE')

    def adopt(self,tenant_id,run_id,variant):
        self.check_active()
        self._require_idle_connections()
        if variant not in ('preview1600','preview2560'):
            raise AdoptionInvalid('RENDER_DERIVATIVE_VARIANT_INVALID')
        rows=self.read.execute('SELECT * FROM photo.v26_render_derivative_context(%s,%s,%s)',(tenant_id,run_id,variant)).fetchall()
        if len(rows)!=1:raise AdoptionInvalid('RENDER_DERIVATIVE_CONTEXT_STALE')
        task,result,attempt,fence,params_id,params_sha,manifest_id,manifest_sha=rows[0]
        reader=PostgresRenderDerivativeReader(self.read,self.objects,tenant_id=tenant_id,run_id=run_id,
            task_id=task,result_id=result,variant=variant,attempt=attempt,fence=fence,allowed_bucket=self.bucket)
        options={}
        if self.additional_working_sha:
            options["additional_working_icc_sha256"]=self.additional_working_sha
        try:
            manifest=verify_render_derivative_result(reader,ArtifactRef(params_id,params_sha),ArtifactRef(manifest_id,manifest_sha),
                target_icc_sha256=self.target_sha,working_icc_sha256=self.working_sha,check_active=self.check_active,**options)
        except AdoptionInvalid as exc:
            code=str(exc)
            if code not in {'RENDER_DERIVATIVE_ENCODING_INVALID','RENDER_DERIVATIVE_CONTRACT_INVALID'}:
                raise
            self.check_active()
            self._require_idle_connections()
            with self.write.transaction():
                self.check_active()
                row=self.write.execute('SELECT photo.reject_v26_render_derivative(%s,%s,%s,%s,%s,%s,%s)',
                    (tenant_id,run_id,variant,result,attempt,fence,code)).fetchone()
                if row is None or row[0] is not True:
                    raise AdoptionInvalid('RENDER_DERIVATIVE_COMMIT_STALE') from exc
            return False
        body=canonical_bytes(manifest).decode('utf-8')
        self.check_active()
        # All version-pinned reads and full decode have finished before this TX.
        self._require_idle_connections()
        with self.write.transaction():
            self.check_active()
            row=self.write.execute('SELECT photo.record_v26_render_derivative_receipt(%s,%s,%s,%s,%s,%s,%s)',
                (tenant_id,run_id,variant,result,attempt,fence,body)).fetchone()
            if row is None or row[0] is not True:
                raise AdoptionInvalid('RENDER_DERIVATIVE_COMMIT_STALE')
        return True
