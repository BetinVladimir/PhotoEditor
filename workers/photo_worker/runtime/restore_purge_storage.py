"""Concrete Python MinIO all-version/multipart absence observer.

Private multipart API is intentionally locked to minio-py7.2.18; unsupported
SDK versions fail closed. No deletes/aborts, pagination repair or success flags.
"""
from dataclasses import dataclass
from uuid import UUID
import minio
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePurgeStorageEvidence:
    schema_version: str
    tenant_id: UUID
    buckets: tuple[str, ...]
    prefixes: tuple[str, ...]
    passes: int


def verify_purged_tenant_storage(client,buckets,tenant_id,*,assert_fixture_scope,check_active):
    if (minio.__version__!='7.2.18' or not isinstance(client,minio.Minio)
            or not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(buckets,(tuple,list)) or not 1<=len(buckets)<=256
            or any(not isinstance(b,str) or not 3<=len(b)<=63 for b in buckets)
            or len(set(buckets))!=len(buckets)
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_STORAGE_INSTALLATION')
    installed=tuple(sorted(buckets))
    prefixes=tuple(part+'tenants/'+str(tenant_id)+'/' for part in ('','staging/','originals/'))
    def guard():
        require_restore_active(check_active)
        if assert_fixture_scope(tenant_id,installed) is False:
            raise RuntimeError('RESTORE_PURGE_FIXTURE_DENIED')
        require_restore_active(check_active)
    guard()
    for _ in range(2):
        for bucket in installed:
            for prefix in prefixes:
                guard()
                listing=iter(client.list_objects(bucket,prefix=prefix,recursive=True,include_version=True))
                try:
                    # Any returned item, including a delete marker, is a remnant.
                    sentinel=object()
                    if next(listing,sentinel) is not sentinel:raise ValueError('RESTORE_PURGE_STORAGE_OBJECT_REMAINS')
                finally:
                    close=getattr(listing,'close',None)
                    if callable(close):close()
                guard()
                result=client._list_multipart_uploads(bucket,delimiter='',prefix=prefix,max_uploads=1)
                guard()
                if (result.bucket_name!=bucket or type(result.is_truncated) is not bool
                        or not isinstance(result.uploads,list)):
                    raise ValueError('RESTORE_PURGE_STORAGE_MULTIPART_RECEIPT')
                if result.is_truncated or result.uploads:
                    raise ValueError('RESTORE_PURGE_STORAGE_MULTIPART_REMAINS')
    guard()
    return RestorePurgeStorageEvidence('restore-purge-storage/v1',tenant_id,installed,prefixes,2)
