"""Exact-version legacy Apply WorkingImage root; nested arrays remain separate."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json,ArtifactRef
from photo_worker.contracts.images import WorkingImage
from .restore_legacy_result import observe_fixture_legacy_apply_result
from .restore_objects import MinioRestoreObjects
from .restore_legacy_array_catalogue import verify_fixture_legacy_array
from .restore_legacy_quality_physical import verify_fixture_legacy_quality
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyPhysicalRoot:
    result: object
    canvas_sha256: str
    pixels: ArtifactRef
    alpha: ArtifactRef | None
    width: int
    height: int
    dtype: str
    arrays: tuple
    quality: object


def verify_fixture_legacy_working_image(connection,client,buckets,target,binding,ticket,*,expected_database,
                                        assert_fixture_scope,check_active,alpha_dtype='uint16'):
    if alpha_dtype not in ('uint16','float32'):raise ValueError('RESTORE_LEGACY_ALPHA_DTYPE')
    common=dict(expected_database=expected_database,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    result=observe_fixture_legacy_apply_result(connection,target,binding,ticket,**common)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    guard()
    obj=result.object
    if not 2<=obj.size<=1048576:raise ValueError('RESTORE_LEGACY_PHYSICAL_SIZE')
    store=MinioRestoreObjects(client,buckets,check_active=guard)
    with store.open_version(obj) as stream:
        chunks=[];size=0
        while True:
            chunk=stream.read(min(65536,obj.size-size+1))
            if not chunk:break
            size+=len(chunk)
            if size>obj.size:raise ValueError('RESTORE_LEGACY_PHYSICAL_SIZE')
            chunks.append(chunk)
    raw=b''.join(chunks)
    if size!=obj.size or sha256(raw).hexdigest()!=obj.sha256:raise ValueError('RESTORE_LEGACY_PHYSICAL_SHA')
    image=WorkingImage.model_validate_json(raw)
    if canonical_bytes(parse_json(raw))!=raw or canonical_bytes(image)!=raw:
        raise ValueError('RESTORE_LEGACY_PHYSICAL_CANONICAL')
    refs=(image.pixels,)+( (image.alpha,) if image.alpha is not None else () )
    if (image.canvas.width*image.canvas.height>200000000
            or any(ref.artifact_id.int==0 or ref.artifact_id==result.artifact_id or ref.sha256=='0'*64 for ref in refs)
            or len({ref.artifact_id for ref in refs})!=len(refs)):
        raise ValueError('RESTORE_LEGACY_PHYSICAL_REFS')
    guard()
    arrays=[verify_fixture_legacy_array(connection,client,buckets,target.tenant_id,result,image.pixels,
        (image.canvas.height,image.canvas.width,3),image.dtype,'working_rgb',check_active=guard)]
    if image.alpha is not None:
        arrays.append(verify_fixture_legacy_array(connection,client,buckets,target.tenant_id,result,image.alpha,
            (image.canvas.height,image.canvas.width),alpha_dtype,'working_alpha',check_active=guard))
    quality=verify_fixture_legacy_quality(connection,client,buckets,target.tenant_id,result,check_active=guard)
    if observe_fixture_legacy_apply_result(connection,target,binding,ticket,**common)!=result:
        raise ValueError('RESTORE_LEGACY_PHYSICAL_CHANGED')
    guard();return RestoreLegacyPhysicalRoot(result,canonical_hash(image.canvas),image.pixels,image.alpha,
        image.canvas.width,image.canvas.height,image.dtype,tuple(arrays),quality)
