"""Independent preview fixture pin; no acceptance report or writer authority."""
from dataclasses import asdict
from hashlib import sha256
import re
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_legacy_preview import RestoreLegacyPreviewTarget,legacy_preview_request_body
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_preview_download import RestorePreviewDownloadTarget
from .restore_preview_lifecycle import run_fixture_preview
from .restore_preview_source import verify_fixture_preview_source
from .restore_preview_icc import verify_preview_icc
from photo_worker.color.icc import ICCRegistry
from .restore_authority import require_restore_fixture


def preview_fixture_descriptor(replay,target,binding,download_target,*,source_width,source_height,srgb_profile,maximum_seconds,alpha_dtype='uint16'):
    if (not isinstance(replay,VerifiedRestoreReplay) or not isinstance(target,RestoreLegacyPreviewTarget)
            or not isinstance(binding,RestoreLegacyApplyBinding) or not isinstance(download_target,RestorePreviewDownloadTarget)
            or any(not isinstance(v,UUID) or v.int==0 for v in
                (binding.actor_id,binding.project_id,binding.asset_version_id,binding.source_artifact_id))
            or not isinstance(binding.source_sha256,str) or re.fullmatch('[0-9a-f]{64}',binding.source_sha256) is None
            or target.asset_version_id!=binding.asset_version_id
            or download_target.api_origin!=target.origin or download_target.authorization!=target.authorization
            or not isinstance(target.idempotency_key,str) or not target.idempotency_key
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or any(type(v) is not int or not 1<=v<=200000 for v in (source_width,source_height))
            or source_width*source_height>200000000
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300):
        raise ValueError('RESTORE_PREVIEW_INSTALLATION')
    if alpha_dtype not in ('uint16','float32'):raise ValueError('RESTORE_PREVIEW_ALPHA_DTYPE')
    request=legacy_preview_request_body(target)
    return _json({'schema_version':'restore-preview-fixture/v1','restore_run_id':replay.run_id,
        'database':replay.database,'build_sha256':replay.build_sha256,'recovery_sha256':replay.recovery_sha256,
        'replay_sha256':replay.replay_sha256,'tenant_id':target.tenant_id,'binding':asdict(binding),
        'api_origin':target.origin,'object_origin':download_target.object_origin,
        'request_sha256':sha256(request).hexdigest(),'request_key_sha256':sha256(target.idempotency_key.encode()).hexdigest(),
        'source_width':source_width,'source_height':source_height,'srgb_sha256':sha256(srgb_profile).hexdigest(),
        'maximum_seconds':maximum_seconds,'alpha_dtype':alpha_dtype})


def run_pinned_preview(replay,fixture_sha256,connection,client,buckets,target,binding,download_target,*,
        source_width,source_height,srgb_profile,maximum_seconds,tls_context,icc_registry,assert_installed_fixture,check_active,alpha_dtype='uint16'):
    """Operator must supply a pre-existing independently installed descriptor SHA.

    Physical WorkingImage root and geometry are rechecked before mutation.
    Source arrays and installed ICC registry are verified before preview admission.
    """
    if (not isinstance(icc_registry,ICCRegistry) or not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_PREVIEW_INSTALLATION')
    descriptor=preview_fixture_descriptor(replay,target,binding,download_target,source_width=source_width,
        source_height=source_height,srgb_profile=srgb_profile,maximum_seconds=maximum_seconds,alpha_dtype=alpha_dtype)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_PREVIEW_FIXTURE_PIN')
    def guard():require_restore_fixture(check_active,assert_installed_fixture)
    guard()
    source=verify_fixture_preview_source(connection,client,buckets,target.tenant_id,binding,
        expected_database=replay.database,alpha_dtype=alpha_dtype,check_active=guard)
    if (source.width,source.height)!=(source_width,source_height):
        raise ValueError('RESTORE_PREVIEW_SOURCE_GEOMETRY')
    verify_preview_icc(icc_registry,source,srgb_profile,check_active=guard)
    guard()
    return run_fixture_preview(connection,client,buckets,target,binding,download_target,expected_database=replay.database,
        source_width=source_width,source_height=source_height,srgb_profile=srgb_profile,maximum_seconds=maximum_seconds,
        tls_context=tls_context,assert_installed_fixture=assert_installed_fixture,check_active=check_active)
