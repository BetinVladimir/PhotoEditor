"""Exact registered report bytes for the production bounded parent DAG walker."""
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.optical.contracts import ImageReport,ImageResult
from .restore_inventory import RestoreObject
from .restore_objects import MinioRestoreObjects
from .restore_batch_result import RestoreBatchResultEvidence
from .restore_authority import require_restore_fixture


class RestoreParentReportReader:
    def __init__(self,connection,client,buckets,tenant_id,result,*,assert_fixture_scope,check_active):
        if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(result,RestoreBatchResultEvidence)
                or not callable(assert_fixture_scope) or not callable(check_active)):
            raise ValueError('RESTORE_PARENT_REPORT_INSTALLATION')
        self.connection=connection;self.tenant_id=tenant_id;self.result=result
        self.scope=assert_fixture_scope;self.active=check_active
        self.objects=MinioRestoreObjects(client,buckets,check_active=self.guard)

    def guard(self):
        require_restore_fixture(self.active,self.scope,self.tenant_id,self.result)

    def catalogue(self,ref,contract='ImageReport'):
        self.guard();c=self.connection
        if c.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with c.transaction():
            c.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            c.execute("SET LOCAL statement_timeout='5s'");c.execute('SET LOCAL row_security=off')
            scope=c.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(self.result.database,True):raise ValueError('RESTORE_PARENT_REPORT_AUTHORITY')
            row=c.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 WHERE a.tenant_id=%s AND a.id=%s AND a.sha256=%s AND i.task_id=%s AND i.contract=%s
 AND t.canonical_result_id=%s AND t.state='succeeded' AND t.attempt=%s AND t.fencing_token=%s
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())''',
                (self.tenant_id,ref.artifact_id,ref.sha256,self.result.task_id,contract,self.result.result_id,self.result.attempt,self.result.fence)).fetchone()
            if not row or len(row)!=5:raise ValueError('RESTORE_PARENT_REPORT_REGISTRATION')
            obj=RestoreObject(*row)
            if (type(obj.size) is not int or not 2<=obj.size<=1048576
                    or not obj.key.startswith('tenants/'+str(self.tenant_id)+'/')):
                raise ValueError('RESTORE_PARENT_REPORT_SCOPE')
            self.guard()
        return obj

    def contract(self,ref,model):
        if not isinstance(ref,ArtifactRef) or ref.artifact_id.int==0 or model not in (ImageReport,ImageResult):
            raise ValueError('RESTORE_PARENT_REPORT_REF')
        obj=self.catalogue(ref,model.__name__)
        with self.objects.open_version(obj) as stream:
            parts=[];size=0
            while True:
                data=stream.read(min(65536,obj.size-size+1))
                if not data:break
                size+=len(data)
                if size>obj.size:raise ValueError('RESTORE_PARENT_REPORT_SIZE')
                parts.append(data)
        raw=b''.join(parts)
        if len(raw)!=obj.size or sha256(raw).hexdigest()!=ref.sha256:raise ValueError('RESTORE_PARENT_REPORT_SHA')
        report=model.model_validate_json(raw)
        if canonical_bytes(report)!=raw:raise ValueError('RESTORE_PARENT_REPORT_CANONICAL')
        if self.catalogue(ref,model.__name__)!=obj:raise ValueError('RESTORE_PARENT_REPORT_CHANGED')
        self.guard();return report


def verify_fixture_parent_reports(connection,client,buckets,tenant_id,result,binding,report_raw,*,assert_fixture_scope,check_active):
    from types import SimpleNamespace
    from photo_worker.v2.render_adoption_inputs import load_render_adoption_inputs
    from photo_worker.v2.render_reports import collect_render_parent_reports
    reader=RestoreParentReportReader(connection,client,buckets,tenant_id,result,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    context=SimpleNamespace(operation='render.compose',version='1',attempt=result.attempt,fence=result.fence,result_id=result.result_id)
    def roots():
        reader.guard()
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
            scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(result.database,True):raise ValueError('RESTORE_PARENT_REPORT_AUTHORITY')
            scoped=load_render_adoption_inputs(connection,tenant_id,result.task_id,context)
            reader.guard()
        return scoped
    before=roots()
    parents=tuple(reader.contract(ArtifactRef(artifact_id=r.artifact_id,sha256=r.sha256),ImageResult) for r in before.parent_roots)
    evidence=collect_render_parent_reports(reader,parents,binding,check_active=reader.guard)
    report=ImageReport.model_validate_json(report_raw)
    if (canonical_bytes(report)!=report_raw or tuple(report.parent_reports or ())!=evidence.reports
            or evidence.review_required and report.status!='review'):
        raise ValueError('RESTORE_PARENT_REPORT_INHERITANCE')
    if roots()!=before:raise ValueError('RESTORE_PARENT_REPORT_ROOTS_CHANGED')
    reader.guard();return evidence
