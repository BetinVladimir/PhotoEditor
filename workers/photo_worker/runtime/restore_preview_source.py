"""Exact restored preview source document authority; nested pixels are separate."""
from dataclasses import dataclass
from hashlib import sha256
import psycopg
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.contracts.images import WorkingImage
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_preview_arrays import verify_fixture_preview_array
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePreviewSourceEvidence:
    object: RestoreObject
    canvas_sha256: str
    width: int
    height: int
    profile_id: str
    icc_sha256: str
    arrays: tuple


def read_preview_source_object(connection,tenant_id,binding,*,expected_database,check_active):
    if not isinstance(binding,RestoreLegacyApplyBinding) or not callable(check_active):
        raise ValueError('RESTORE_PREVIEW_SOURCE_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_PREVIEW_SOURCE_AUTHORITY')
        row=connection.execute('''SELECT ar.bucket,ar.object_key,ar.object_version,ar.sha256,ar.bytes
 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash
 WHERE v.tenant_id=%s AND v.id=%s AND a.project_id=%s AND a.deleted_at IS NULL
 AND ar.id=%s AND ar.sha256=%s AND ar.output_role='working_image' AND ar.media_type='application/json'
 AND ar.state='ready' AND ar.verified_at IS NOT NULL AND ar.bytes BETWEEN 2 AND 1048576
 AND ar.object_version IS NOT NULL AND ar.object_version NOT IN ('','null')
 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())''',
            (tenant_id,binding.asset_version_id,binding.project_id,binding.source_artifact_id,binding.source_sha256)).fetchone()
        if not row or len(row)!=5:raise ValueError('RESTORE_PREVIEW_SOURCE_MISSING')
        obj=RestoreObject(*row);_validate(obj)
        if not obj.key.startswith('tenants/'+str(tenant_id)+'/'):raise ValueError('RESTORE_PREVIEW_SOURCE_SCOPE')
        require_restore_active(check_active)
    require_restore_active(check_active)
    return obj


def verify_fixture_preview_source(connection,client,buckets,tenant_id,binding,*,expected_database,check_active,alpha_dtype='uint16'):
    if alpha_dtype not in ('uint16','float32'):raise ValueError('RESTORE_PREVIEW_SOURCE_ALPHA_DTYPE')
    def observe():return read_preview_source_object(connection,tenant_id,binding,
        expected_database=expected_database,check_active=check_active)
    before=observe()
    if not 2<=before.size<=1048576 or before.sha256!=binding.source_sha256:
        raise ValueError('RESTORE_PREVIEW_SOURCE_SIZE_OR_BINDING')
    require_restore_active(check_active)
    store=MinioRestoreObjects(client,buckets,check_active=lambda:require_restore_active(check_active))
    with store.open_version(before) as stream:
        chunks=[];size=0
        while True:
            block=stream.read(min(65536,before.size-size+1))
            if not block:break
            size+=len(block)
            if size>before.size:raise ValueError('RESTORE_PREVIEW_SOURCE_SIZE')
            chunks.append(block)
    raw=b''.join(chunks)
    if len(raw)!=before.size or sha256(raw).hexdigest()!=before.sha256:
        raise ValueError('RESTORE_PREVIEW_SOURCE_SHA')
    image=WorkingImage.model_validate_json(raw)
    if canonical_bytes(parse_json(raw))!=raw or canonical_bytes(image)!=raw:
        raise ValueError('RESTORE_PREVIEW_SOURCE_CANONICAL')
    refs=(image.pixels,)+((image.alpha,) if image.alpha is not None else ())
    if (any(ref.artifact_id.int==0 or ref.artifact_id==binding.source_artifact_id or ref.sha256=='0'*64 for ref in refs)
            or len({ref.artifact_id for ref in refs})!=len(refs)):
        raise ValueError('RESTORE_PREVIEW_SOURCE_REFS')
    if image.dtype!='float32' or image.canvas.width*image.canvas.height>200000000:
        raise ValueError('RESTORE_PREVIEW_SOURCE_FORMAT')
    arrays=[verify_fixture_preview_array(connection,client,buckets,tenant_id,binding,image.pixels,
        (image.canvas.height,image.canvas.width,3),image.dtype,'working_rgb',
        expected_database=expected_database,check_active=lambda:require_restore_active(check_active))]
    if image.alpha is not None:
        arrays.append(verify_fixture_preview_array(connection,client,buckets,tenant_id,binding,image.alpha,
            (image.canvas.height,image.canvas.width),alpha_dtype,'working_alpha',
            expected_database=expected_database,check_active=lambda:require_restore_active(check_active)))
    if observe()!=before:raise ValueError('RESTORE_PREVIEW_SOURCE_CHANGED')
    require_restore_active(check_active);return RestorePreviewSourceEvidence(before,canonical_hash(image.canvas),image.canvas.width,
        image.canvas.height,image.color.profile_id,image.color.icc_sha256,tuple(arrays))
