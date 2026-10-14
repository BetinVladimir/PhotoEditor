"""Physical canonical ImageResult and direct registered child verification."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.optical.contracts import ImageResult
from photo_worker.contracts.images import WorkingImage
from .restore_batch_result import observe_fixture_batch_result
from .restore_objects import MinioRestoreObjects
from .restore_inventory import RestoreObject,verify_restore_inventory
from .restore_render_semantics import validate_restore_render_documents,RestoreRenderSemanticEvidence
from photo_worker.operations.session.contracts import VersionBinding
from .restore_render_arrays import verify_fixture_render_arrays
from .restore_parent_reports import verify_fixture_parent_reports
from .restore_authority import require_restore_fixture
import psycopg


@dataclass(frozen=True)
class RestoreBatchPhysicalEvidence:
    schema_version: str
    result: object
    image_result: ImageResult
    working_image: WorkingImage
    children: tuple
    inventory: object
    semantics: RestoreRenderSemanticEvidence
    arrays: tuple
    parents: object


def verify_fixture_batch_physical(connection,client,buckets,tenant_id,adoption,*,binding,source_raw,assert_fixture_scope,check_active):
    if not isinstance(binding,VersionBinding):raise ValueError('RESTORE_BATCH_PHYSICAL_BINDING')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,tenant_id,adoption)
    evidence=observe_fixture_batch_result(connection,tenant_id,adoption,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    store=MinioRestoreObjects(client,buckets,check_active=guard)
    with store.open_version(evidence.object) as stream:
        parts=[];received=0
        while True:
            chunk=stream.read(min(65536,evidence.object.size-received+1))
            if not chunk:break
            received+=len(chunk)
            if received>evidence.object.size:raise ValueError('RESTORE_BATCH_PHYSICAL_SIZE')
            parts.append(chunk)
    raw=b''.join(parts)
    if len(raw)!=evidence.object.size or sha256(raw).hexdigest()!=evidence.object.sha256:
        raise ValueError('RESTORE_BATCH_PHYSICAL_SHA')
    value=parse_json(raw)
    result=ImageResult.model_validate_json(raw)
    if canonical_bytes(value)!=raw or canonical_bytes(result)!=raw:raise ValueError('RESTORE_BATCH_PHYSICAL_CANONICAL')
    refs=[result.working_image,result.report,result.changed_mask]
    if result.generated_mask is not None:refs.append(result.generated_mask)
    if (any(r.artifact_id.int==0 or r.artifact_id==evidence.artifact_id for r in refs)
            or len({r.artifact_id for r in refs})!=len(refs)):
        raise ValueError('RESTORE_BATCH_PHYSICAL_ALIAS')
    def catalogue():
        guard()
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        children=[]
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(adoption.database,True):raise ValueError('RESTORE_BATCH_PHYSICAL_AUTHORITY')
            for ref in refs:
                row=connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes FROM photo.artifacts a
 WHERE a.tenant_id=%s AND a.id=%s AND a.sha256=%s
 AND ((a.created_by_task_id=%s AND a.produced_attempt=%s AND a.produced_fence=%s)
 OR (%s AND EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=a.tenant_id AND i.task_id=%s
 AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='WorkingImage')))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())''',
                    (tenant_id,ref.artifact_id,ref.sha256,evidence.task_id,evidence.attempt,evidence.fence,
                     ref==result.working_image,evidence.task_id)).fetchone()
                if not row or len(row)!=5:raise ValueError('RESTORE_BATCH_PHYSICAL_CHILD')
                obj=RestoreObject(*row)
                if (type(obj.size) is not int or not 2<=obj.size<=1048576
                        or not obj.key.startswith('tenants/'+str(tenant_id)+'/')):
                    raise ValueError('RESTORE_BATCH_PHYSICAL_CHILD_SCOPE')
                children.append(obj);guard()
        guard();return tuple(children)
    children=catalogue()
    inventory=verify_restore_inventory(sorted(children,key=lambda r:tuple(v.encode() for v in (r.bucket,r.key,r.version))),
        store.open_version,check_active=guard,maximum_objects=4,maximum_bytes=4*1048576)
    documents=[]
    for ref in children:
        guard()
        with store.open_version(ref) as stream:
            parts=[];received=0
            while True:
                chunk=stream.read(min(65536,ref.size-received+1))
                if not chunk:break
                received+=len(chunk)
                if received>ref.size:raise ValueError('RESTORE_BATCH_PHYSICAL_SIZE')
                parts.append(chunk)
        raw_child=b''.join(parts)
        if len(raw_child)!=ref.size or sha256(raw_child).hexdigest()!=ref.sha256:
            raise ValueError('RESTORE_BATCH_PHYSICAL_SHA')
        documents.append(raw_child)
    semantics=validate_restore_render_documents(documents[0],documents[1],documents[2],
        documents[3] if len(documents)==4 else None,binding=binding,check_active=guard)
    working_image=WorkingImage.model_validate_json(documents[0])
    if (sha256(documents[0]).hexdigest()!=result.working_image.sha256
            or canonical_bytes(working_image)!=documents[0]):
        raise ValueError('RESTORE_BATCH_PHYSICAL_WORKING_IMAGE')
    arrays=verify_fixture_render_arrays(connection,client,buckets,tenant_id,evidence,binding,source_raw,
        documents[0],documents[2],documents[3] if len(documents)==4 else None,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    parents=verify_fixture_parent_reports(connection,client,buckets,tenant_id,evidence,binding,documents[1],
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    if catalogue()!=children:raise ValueError('RESTORE_BATCH_PHYSICAL_CHANGED')
    final=observe_fixture_batch_result(connection,tenant_id,adoption,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    if final!=evidence:raise ValueError('RESTORE_BATCH_PHYSICAL_CHANGED')
    guard()
    return RestoreBatchPhysicalEvidence('restore-batch-physical/v1',evidence,result,working_image,children,
                                        inventory,semantics,arrays,parents)
