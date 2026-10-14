"""Independent fixture identity for one native legacy Apply restore observation."""
from dataclasses import asdict
from hashlib import sha256
from uuid import UUID
import re
from photo_worker.contracts.base import canonical_bytes
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_legacy_apply import RestoreLegacyApplyTarget,legacy_apply_request_body
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_apply_lifecycle import run_fixture_legacy_apply
from .restore_preview_download import RestorePreviewDownloadTarget
from .restore_authority import require_restore_fixture


def apply_fixture_descriptor(replay,target,binding,download_target,*,maximum_seconds,alpha_dtype='uint16'):
    if (not isinstance(replay,VerifiedRestoreReplay) or not isinstance(target,RestoreLegacyApplyTarget)
            or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(download_target,RestorePreviewDownloadTarget)
            or download_target.api_origin!=target.origin or download_target.authorization!=target.authorization
            or any(not isinstance(v,UUID) or v.int==0 for v in
                (target.tenant_id,target.collection_id,target.item_id,target.style_version_id,
                 binding.actor_id,binding.project_id,binding.asset_version_id,binding.source_artifact_id))
            or not isinstance(binding.source_sha256,str) or re.fullmatch('[0-9a-f]{64}',binding.source_sha256) is None
            or type(target.source_revision) is not int or not 1<=target.source_revision<9007199254740991
            or not isinstance(target.idempotency_key,str) or not 1<=len(target.idempotency_key)<=128
            or any(not 33<=ord(c)<=126 for c in target.idempotency_key)
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or alpha_dtype not in ('uint16','float32')):
        raise ValueError('RESTORE_APPLY_INSTALLATION')
    return _json({'schema_version':'restore-apply-fixture/v1','restore_run_id':replay.run_id,
        'database':replay.database,'build_sha256':replay.build_sha256,'recovery_sha256':replay.recovery_sha256,
        'replay_sha256':replay.replay_sha256,'tenant_id':target.tenant_id,'binding':asdict(binding),
        'api_origin':target.origin,'object_origin':download_target.object_origin,'collection_id':target.collection_id,'item_id':target.item_id,
        'source_revision':target.source_revision,'style_version_id':target.style_version_id,
        'request_sha256':sha256(legacy_apply_request_body(target)).hexdigest(),
        'request_key_sha256':sha256(target.idempotency_key.encode()).hexdigest(),
        'maximum_seconds':maximum_seconds,'alpha_dtype':alpha_dtype})


def run_pinned_legacy_apply(replay,fixture_sha256,connection,client,buckets,target,binding,download_target,*,maximum_seconds,
        tls_context,assert_installed_fixture,check_active,alpha_dtype='uint16'):
    """No report, mutation retry or writer activation; pin must be independently installed."""
    if (not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_APPLY_INSTALLATION')
    descriptor=apply_fixture_descriptor(replay,target,binding,download_target,maximum_seconds=maximum_seconds,alpha_dtype=alpha_dtype)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_APPLY_FIXTURE_PIN')
    require_restore_fixture(check_active,assert_installed_fixture)
    return run_fixture_legacy_apply(connection,client,buckets,target,binding,download_target,expected_database=replay.database,
        maximum_seconds=maximum_seconds,tls_context=tls_context,assert_installed_fixture=assert_installed_fixture,
        check_active=check_active,alpha_dtype=alpha_dtype)
