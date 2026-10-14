"""Verify pre-provisioned fixture bytes and readability before tenant deletion."""
from dataclasses import dataclass
from uuid import UUID
from .restore_inventory import RestoreObject, RestoreInventoryEvidence, verify_restore_inventory
from .restore_objects import MinioRestoreObjects
from .restore_access_probe import RestoreAccessReadableEvidence, observe_restore_access_readable
from .restore_s3_probe import RestoreS3AccessTarget, RestoreS3ReadableEvidence, observe_signed_s3_readable
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestorePurgeBaseline:
    schema_version: str
    tenant_id: UUID
    inventory: RestoreInventoryEvidence
    api: RestoreAccessReadableEvidence
    s3: RestoreS3ReadableEvidence


def verify_purge_fixture_baseline(client,buckets,tenant_id,references,*,api_target,api_sha256,
                                 s3_target,tls_context,assert_installed_fixture,check_active):
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(references,(tuple,list))
            or not 3<=len(references)<=512 or any(not isinstance(ref,RestoreObject) for ref in references)
            or not isinstance(s3_target,RestoreS3AccessTarget) or s3_target.tenant_id!=tenant_id
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_BASELINE_INSTALLATION')
    refs=tuple(references)
    prefixes=tuple(part+'tenants/'+str(tenant_id)+'/' for part in ('','staging/','originals/'))
    covered=set();selected=[]
    for ref in refs:
        matches=[prefix for prefix in prefixes if ref.key.startswith(prefix)]
        if len(matches)!=1:raise ValueError('RESTORE_PURGE_BASELINE_SCOPE')
        covered.add(matches[0])
        if (ref.bucket,ref.key,ref.version)==(s3_target.bucket,s3_target.key,s3_target.version):selected.append(ref)
    if covered!=set(prefixes) or len(selected)!=1:raise ValueError('RESTORE_PURGE_BASELINE_COVERAGE')
    def guard():require_restore_fixture(check_active,assert_installed_fixture)
    guard();objects=MinioRestoreObjects(client,buckets,check_active=guard)
    inventory=verify_restore_inventory(refs,objects.open_version,check_active=guard,
        maximum_objects=512,maximum_bytes=256<<20)
    api=observe_restore_access_readable(api_target,api_sha256,tls_context=tls_context,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    selected=selected[0]
    s3=observe_signed_s3_readable(s3_target,selected.sha256,selected.size,tls_context=tls_context,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    guard()
    return RestorePurgeBaseline('restore-purge-baseline/v1',tenant_id,inventory,api,s3)
