"""Independent SQL catalogue for the adopted after-side render review preview."""
from dataclasses import dataclass
import re
from uuid import UUID
import psycopg
from photo_worker.contracts.base import ArtifactRef
from .restore_inventory import RestoreObject,_validate
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderPreviewObjects:
    database: str
    review_id: UUID
    source: ArtifactRef
    variant: str
    profile_hash: str
    manifest: ArtifactRef
    manifest_object: RestoreObject
    image: ArtifactRef
    image_object: RestoreObject
    image_media_type: str


def observe_fixture_after_preview_objects(connection,tenant_id,review_id,task_id,source,
                                          variant,profile_hash,buckets,*,expected_database,
                                          assert_fixture_scope,check_active,accepted_revision=None):
    """Pin SQL receipt/object versions independently of API signed links."""
    if (any(not isinstance(v,UUID) or v.int==0 for v in (tenant_id,review_id,task_id))
            or not isinstance(source,ArtifactRef) or source.artifact_id.int==0
            or variant not in ('preview1600','preview2560')
            or not isinstance(profile_hash,str)
            or re.fullmatch('[0-9a-f]{64}',profile_hash) is None
            or not isinstance(buckets,(tuple,list)) or not buckets
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or not isinstance(expected_database,str) or not expected_database
            or (accepted_revision is not None and
                (type(accepted_revision) is not int or not 2<=accepted_revision<9007199254740991))
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_INSTALLATION')
    allowed=frozenset(buckets)
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):
            raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_AUTHORITY')
        rows=connection.execute('''SELECT m.id,m.sha256,m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,
  a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.reviews v
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=v.tenant_id AND workflow.run_id=v.run_id
 JOIN photo.tasks origin ON origin.tenant_id=v.tenant_id
  AND origin.id=v.task_id AND origin.run_id=workflow.run_id
 JOIN photo.runs r ON r.tenant_id=origin.tenant_id AND r.v26_derivative_task_id=origin.id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id AND d.run_id=t.run_id
 CROSS JOIN LATERAL jsonb_array_elements(d.v26_derivative_receipt->'objects') mo
 CROSS JOIN LATERAL jsonb_array_elements(d.v26_derivative_receipt->'objects') io
 JOIN photo.artifacts m ON m.tenant_id=d.tenant_id AND m.id::text=mo->>'artifact_id' AND m.sha256=mo->>'sha256'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id::text=io->>'artifact_id' AND a.sha256=io->>'sha256'
 JOIN photo.output_uploads mu ON mu.tenant_id=m.tenant_id AND mu.artifact_id=m.id AND mu.task_id=t.id
 JOIN photo.output_uploads iu ON iu.tenant_id=a.tenant_id AND iu.artifact_id=a.id AND iu.task_id=t.id
 WHERE v.tenant_id=%s AND v.id=%s AND v.task_id=%s
  AND v.kind='render.compose'
  AND ((%s::bigint IS NULL AND v.state='pending' AND v.decision IS NULL)
    OR (%s::bigint IS NOT NULL AND v.state='decided' AND v.revision=%s
      AND v.decision='accept' AND v.decision_route='candidate'))
  AND v.candidate_artifact_id=%s AND v.candidate_hash=%s
  AND origin.operation='render.compose' AND origin.state='succeeded' AND NOT origin.cancel_requested
  AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.config_snapshot->>'source_result_id'=origin.canonical_result_id::text
  AND t.operation='media.preview' AND t.state='succeeded' AND NOT t.cancel_requested
  AND d.purpose='task_params' AND d.state='ready'
  AND d.v26_derivative_receipt->>'result_id'=t.canonical_result_id::text
  AND d.v26_derivative_receipt->>'attempt'=t.attempt::text
  AND d.v26_derivative_receipt->>'fence'=t.fencing_token::text
  AND d.v26_derivative_receipt->'manifest'->'source'=jsonb_build_object('artifact_id',%s::text,'sha256',%s)
  AND d.v26_derivative_receipt->'manifest'->>'variant'=%s
  AND d.v26_derivative_receipt->'manifest'->>'profile_hash'=%s
  AND m.id::text=d.v26_derivative_receipt->>'manifest_id'
  AND a.id::text=d.v26_derivative_receipt->'manifest'->'derivative'->>'artifact_id'
  AND a.sha256=d.v26_derivative_receipt->'manifest'->'derivative'->>'sha256'
  AND m.output_role='derivative_manifest' AND m.media_type='application/json'
  AND a.output_role='derivative' AND a.media_type IN ('image/jpeg','image/webp')
  AND m.state='ready' AND a.state='ready' AND m.verified_at IS NOT NULL AND a.verified_at IS NOT NULL
  AND m.created_by_task_id=t.id AND a.created_by_task_id=t.id
  AND m.produced_attempt=t.attempt AND a.produced_attempt=t.attempt
  AND m.produced_fence=t.fencing_token AND a.produced_fence=t.fencing_token
  AND m.bucket=mo->>'bucket' AND m.object_key=mo->>'key' AND m.object_version=mo->>'version'
  AND m.bytes=(mo->>'bytes')::bigint AND m.media_type=mo->>'media_type'
  AND a.bucket=io->>'bucket' AND a.object_key=io->>'key' AND a.object_version=io->>'version'
  AND a.bytes=(io->>'bytes')::bigint AND a.media_type=io->>'media_type'
  AND mu.state='ready' AND iu.state='ready' AND mu.attempt=t.attempt AND iu.attempt=t.attempt
  AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
  AND mu.object_version=m.object_version AND iu.object_version=a.object_version
  AND mu.bucket=m.bucket AND mu.object_key=m.object_key AND mu.sha256=m.sha256 AND mu.bytes=m.bytes
  AND mu.media_type=m.media_type AND mu.role=m.output_role
  AND iu.bucket=a.bucket AND iu.object_key=a.object_key AND iu.sha256=a.sha256 AND iu.bytes=a.bytes
  AND iu.media_type=a.media_type AND iu.role=a.output_role
  AND (m.expires_at IS NULL OR m.expires_at>transaction_timestamp())
  AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())
 LIMIT 2''',(tenant_id,review_id,task_id,
             accepted_revision,accepted_revision,accepted_revision,
             source.artifact_id,source.sha256,
             source.artifact_id,source.sha256,variant,profile_hash)).fetchall()
        guard()
    result=_decode_preview_objects(rows,expected_database,review_id,source,variant,profile_hash,allowed)
    guard();return result


def _decode_preview_objects(rows,database,review_id,source,variant,profile_hash,allowed):
    if len(rows)!=1 or len(rows[0])!=14:
        raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_NOT_READY')
    m_id,m_sha,m_bucket,m_key,m_version,m_size,m_media,a_id,a_sha,a_bucket,a_key,a_version,a_size,a_media=rows[0]
    if (any(not isinstance(v,UUID) or v.int==0 for v in (m_id,a_id))
            or len({source.artifact_id,m_id,a_id})!=3
            or m_bucket not in allowed or a_bucket not in allowed
            or m_media!='application/json' or a_media not in ('image/jpeg','image/webp')):
        raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_OBJECT')
    manifest_obj=RestoreObject(m_bucket,m_key,m_version,m_sha,m_size)
    image_obj=RestoreObject(a_bucket,a_key,a_version,a_sha,a_size)
    _validate(manifest_obj);_validate(image_obj)
    if not 2<=manifest_obj.size<=1048576 or not 2<=image_obj.size<=52*1048576:
        raise ValueError('RESTORE_RENDER_PREVIEW_CATALOGUE_SIZE')
    return RestoreRenderPreviewObjects(database,review_id,source,variant,profile_hash,
        ArtifactRef(artifact_id=m_id,sha256=m_sha),manifest_obj,
        ArtifactRef(artifact_id=a_id,sha256=a_sha),image_obj,a_media)


def observe_fixture_before_asset_preview(connection,tenant_id,review_id,source,
                                         variant,profile_hash,buckets,*,expected_database,
                                         assert_fixture_scope,check_active):
    """Resolve the live accepted original asset preview fallback used by API."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(review_id,UUID) or review_id.int==0
            or not isinstance(source,ArtifactRef) or source.artifact_id.int==0
            or variant not in ('preview1600','preview2560')
            or not isinstance(profile_hash,str)
            or re.fullmatch('[0-9a-f]{64}',profile_hash) is None
            or not isinstance(buckets,(tuple,list)) or not buckets
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or not isinstance(expected_database,str) or not expected_database
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_BEFORE_PREVIEW_CATALOGUE_INSTALLATION')
    allowed=frozenset(buckets)
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):
            raise ValueError('RESTORE_BEFORE_PREVIEW_CATALOGUE_AUTHORITY')
        rows=connection.execute('''SELECT m.id,m.sha256,m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,
  a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.reviews review
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=review.tenant_id AND workflow.run_id=review.run_id
 JOIN photo.asset_versions v ON v.tenant_id=review.tenant_id AND v.master_artifact_id=review.base_artifact_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.derivatives d ON d.tenant_id=v.tenant_id AND d.asset_version_id=v.id
 JOIN photo.tasks t ON t.tenant_id=d.tenant_id AND t.id=d.task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 JOIN photo.artifacts m ON m.tenant_id=t.tenant_id AND m.created_by_task_id=t.id AND m.output_role='derivative_manifest'
 JOIN photo.output_uploads mu ON mu.tenant_id=m.tenant_id AND mu.artifact_id=m.id AND mu.task_id=t.id
 JOIN photo.output_uploads iu ON iu.tenant_id=a.tenant_id AND iu.artifact_id=a.id AND iu.task_id=t.id
 WHERE review.tenant_id=%s AND review.id=%s AND review.kind='render.compose'
  AND review.state='pending' AND review.base_artifact_id=%s
  AND asset.project_id=workflow.project_id AND asset.deleted_at IS NULL
  AND asset.upload_state='verified' AND v.review_state='accepted'
  AND v.master_artifact_id=%s AND v.content_hash=%s
  AND d.variant=%s AND d.profile_hash=%s AND d.status='ready'
  AND t.operation='media.preview' AND t.operation_version='1'
  AND t.state='succeeded' AND NOT t.cancel_requested
  AND r.status='completed' AND NOT r.cancel_requested
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',%s::text,'sha256',%s))
  AND jsonb_array_length(z.outputs)=2
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',m.id::text,'sha256',m.sha256),
   jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
  AND m.state='ready' AND a.state='ready' AND m.verified_at IS NOT NULL AND a.verified_at IS NOT NULL
  AND m.media_type='application/json' AND a.output_role='derivative'
  AND a.created_by_task_id=t.id
  AND m.produced_attempt=t.attempt AND a.produced_attempt=t.attempt
  AND m.produced_fence=t.fencing_token AND a.produced_fence=t.fencing_token
  AND mu.state='ready' AND iu.state='ready' AND mu.attempt=t.attempt AND iu.attempt=t.attempt
  AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
  AND mu.bucket=m.bucket AND mu.object_key=m.object_key AND mu.object_version=m.object_version
  AND mu.sha256=m.sha256 AND mu.bytes=m.bytes AND mu.media_type=m.media_type AND mu.role=m.output_role
  AND iu.bucket=a.bucket AND iu.object_key=a.object_key AND iu.object_version=a.object_version
  AND iu.sha256=a.sha256 AND iu.bytes=a.bytes AND iu.media_type=a.media_type AND iu.role=a.output_role
  AND (m.expires_at IS NULL OR m.expires_at>transaction_timestamp())
  AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())
 LIMIT 2''',(tenant_id,review_id,source.artifact_id,source.artifact_id,source.sha256,
             variant,profile_hash,source.artifact_id,source.sha256)).fetchall()
        guard()
    result=_decode_preview_objects(rows,expected_database,review_id,source,variant,profile_hash,allowed)
    guard();return result


def observe_fixture_before_render_preview(connection,tenant_id,review_id,source,
                                         variant,profile_hash,buckets,*,expected_database,
                                         assert_fixture_scope,check_active):
    """Resolve the primary before-side V26 preview of the review base image."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(review_id,UUID) or review_id.int==0
            or not isinstance(source,ArtifactRef) or source.artifact_id.int==0
            or variant not in ('preview1600','preview2560')
            or not isinstance(profile_hash,str)
            or re.fullmatch('[0-9a-f]{64}',profile_hash) is None
            or not isinstance(buckets,(tuple,list)) or not buckets
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or not isinstance(expected_database,str) or not expected_database
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_BEFORE_RENDER_PREVIEW_INSTALLATION')
    allowed=frozenset(buckets)
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):
            raise ValueError('RESTORE_BEFORE_RENDER_PREVIEW_AUTHORITY')
        rows=connection.execute('''SELECT m.id,m.sha256,m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,
  a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.reviews v
 JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
 JOIN photo.artifacts base ON base.tenant_id=v.tenant_id AND base.id=v.base_artifact_id
 JOIN photo.tasks origin ON origin.tenant_id=base.tenant_id AND origin.id=base.created_by_task_id
 JOIN photo.v2_workflows preview_workflow ON preview_workflow.tenant_id=origin.tenant_id
  AND preview_workflow.run_id=origin.run_id AND preview_workflow.project_id=w.project_id
 JOIN photo.runs r ON r.tenant_id=origin.tenant_id AND r.v26_derivative_task_id=origin.id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id AND d.run_id=t.run_id
 CROSS JOIN LATERAL jsonb_array_elements(d.v26_derivative_receipt->'objects') mo
 CROSS JOIN LATERAL jsonb_array_elements(d.v26_derivative_receipt->'objects') io
 JOIN photo.artifacts m ON m.tenant_id=d.tenant_id AND m.id::text=mo->>'artifact_id' AND m.sha256=mo->>'sha256'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id::text=io->>'artifact_id' AND a.sha256=io->>'sha256'
 JOIN photo.output_uploads mu ON mu.tenant_id=m.tenant_id AND mu.artifact_id=m.id AND mu.task_id=t.id
 JOIN photo.output_uploads iu ON iu.tenant_id=a.tenant_id AND iu.artifact_id=a.id AND iu.task_id=t.id
 WHERE v.tenant_id=%s AND v.id=%s AND v.kind='render.compose' AND v.state='pending'
  AND v.base_artifact_id=%s
  AND origin.state='succeeded' AND NOT origin.cancel_requested
  AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.config_snapshot->>'source_result_id'=origin.canonical_result_id::text
  AND t.operation='media.preview' AND t.state='succeeded' AND NOT t.cancel_requested
  AND d.purpose='task_params' AND d.state='ready'
  AND d.v26_derivative_receipt->>'result_id'=t.canonical_result_id::text
  AND d.v26_derivative_receipt->>'attempt'=t.attempt::text
  AND d.v26_derivative_receipt->>'fence'=t.fencing_token::text
  AND d.v26_derivative_receipt->'manifest'->'source'=jsonb_build_object('artifact_id',%s::text,'sha256',%s)
  AND d.v26_derivative_receipt->'manifest'->>'variant'=%s
  AND d.v26_derivative_receipt->'manifest'->>'profile_hash'=%s
  AND m.id::text=d.v26_derivative_receipt->>'manifest_id'
  AND a.id::text=d.v26_derivative_receipt->'manifest'->'derivative'->>'artifact_id'
  AND a.sha256=d.v26_derivative_receipt->'manifest'->'derivative'->>'sha256'
  AND m.output_role='derivative_manifest' AND m.media_type='application/json'
  AND a.output_role='derivative' AND a.media_type IN ('image/jpeg','image/webp')
  AND m.state='ready' AND a.state='ready' AND m.verified_at IS NOT NULL AND a.verified_at IS NOT NULL
  AND m.created_by_task_id=t.id AND a.created_by_task_id=t.id
  AND m.produced_attempt=t.attempt AND a.produced_attempt=t.attempt
  AND m.produced_fence=t.fencing_token AND a.produced_fence=t.fencing_token
  AND m.bucket=mo->>'bucket' AND m.object_key=mo->>'key' AND m.object_version=mo->>'version'
  AND m.bytes=(mo->>'bytes')::bigint AND m.media_type=mo->>'media_type'
  AND a.bucket=io->>'bucket' AND a.object_key=io->>'key' AND a.object_version=io->>'version'
  AND a.bytes=(io->>'bytes')::bigint AND a.media_type=io->>'media_type'
  AND mu.state='ready' AND iu.state='ready' AND mu.attempt=t.attempt AND iu.attempt=t.attempt
  AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
  AND mu.object_version=m.object_version AND iu.object_version=a.object_version
  AND mu.bucket=m.bucket AND mu.object_key=m.object_key AND mu.sha256=m.sha256 AND mu.bytes=m.bytes
  AND mu.media_type=m.media_type AND mu.role=m.output_role
  AND iu.bucket=a.bucket AND iu.object_key=a.object_key AND iu.sha256=a.sha256 AND iu.bytes=a.bytes
  AND iu.media_type=a.media_type AND iu.role=a.output_role
  AND (m.expires_at IS NULL OR m.expires_at>transaction_timestamp())
  AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())
 ORDER BY r.created_at DESC,t.id LIMIT 1''',(tenant_id,review_id,source.artifact_id,source.artifact_id,source.sha256,
             variant,profile_hash)).fetchall()
        guard()
    result=_decode_preview_objects(rows,expected_database,review_id,source,variant,profile_hash,allowed)
    guard();return result
