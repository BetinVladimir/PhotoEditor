"""Exact source WorkingImage lineage for physically reused RGB/alpha."""
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json,ArtifactRef
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .restore_inventory import RestoreObject,_validate
from .restore_batch_result import RestoreBatchResultEvidence
from .restore_render_array import verify_restore_render_array
from .restore_authority import require_restore_fixture


def verify_fixture_source_array(connection,client,buckets,tenant_id,result,binding,source_raw,reference,role,*,
                                  assert_fixture_scope,check_active):
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(result,RestoreBatchResultEvidence)
            or not isinstance(binding,VersionBinding) or not isinstance(reference,ArtifactRef)
            or not isinstance(source_raw,bytes) or not 2<=len(source_raw)<=1048576
            or sha256(source_raw).hexdigest()!=binding.source.sha256 or role not in ('pixels','alpha')
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_SOURCE_ARRAY_INSTALLATION')
    source=WorkingImage.model_validate_json(source_raw)
    if (canonical_bytes(parse_json(source_raw))!=source_raw or canonical_bytes(source)!=source_raw
            or canonical_hash(source.canvas)!=binding.canvas_hash or source.dtype!='float32'
            or reference!=(source.pixels if role=='pixels' else source.alpha)):
        raise ValueError('RESTORE_SOURCE_ARRAY_BINDING')
    shape=(source.canvas.height,source.canvas.width)
    if role=='pixels':shape=(*shape,3)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,
        tenant_id,result,binding,reference,role)
    def catalogue():
        guard()
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(result.database,True):raise ValueError('RESTORE_SOURCE_ARRAY_AUTHORITY')
            row=connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes FROM photo.artifacts a
 JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='NpyArray'
 JOIN photo.tasks task ON task.tenant_id=i.tenant_id AND task.id=i.task_id
 JOIN photo.v2_task_inputs root ON root.tenant_id=task.tenant_id AND root.task_id=task.id AND root.contract='WorkingImage'
 JOIN photo.artifacts doc ON doc.tenant_id=root.tenant_id AND doc.id=root.artifact_id AND doc.sha256=root.sha256
 JOIN photo.asset_versions v ON v.tenant_id=doc.tenant_id AND v.master_artifact_id=doc.id AND v.content_hash=doc.sha256
 WHERE a.tenant_id=%s AND a.id=%s AND a.sha256=%s AND task.id=%s AND task.canonical_result_id=%s
 AND task.state='succeeded' AND task.attempt=%s AND task.fencing_token=%s
 AND task.operation='render.compose' AND task.operation_version='1'
 AND root.artifact_id=%s AND root.sha256=%s AND v.id=%s
 AND doc.state='ready' AND doc.verified_at IS NOT NULL AND doc.object_version IS NOT NULL
 AND doc.object_version NOT IN ('','null') AND (doc.expires_at IS NULL OR doc.expires_at>transaction_timestamp())
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/x-npy'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())''',
                (tenant_id,reference.artifact_id,reference.sha256,result.task_id,result.result_id,result.attempt,result.fence,
                 binding.source.artifact_id,binding.source.sha256,binding.asset_version_id)).fetchone()
            if not row or len(row)!=5:raise ValueError('RESTORE_SOURCE_ARRAY_LINEAGE')
            obj=RestoreObject(*row);_validate(obj)
            if not obj.key.startswith('tenants/'+str(tenant_id)+'/'):raise ValueError('RESTORE_SOURCE_ARRAY_SCOPE')
            guard()
        return obj
    before=catalogue()
    evidence=verify_restore_render_array(client,buckets,reference,before,shape,role,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    if catalogue()!=before:raise ValueError('RESTORE_SOURCE_ARRAY_CHANGED')
    guard();return evidence
