"""Pinned legacy preview/Apply through encoded export and owner delivery."""
from dataclasses import dataclass
from datetime import datetime,timezone
from hashlib import sha256
import os
import re
from pathlib import Path
from time import monotonic
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams
from .restore_download_intent import RestoreDownloadIntentTarget,validate_fixture_download_intent
from .restore_export import RestoreExportTarget,validate_fixture_export
from .restore_legacy_lifecycle import (
    RestoreLegacyInstallation,legacy_fixture_descriptor,run_pinned_legacy_observations)
from .restore_legacy_selection_binding import bind_fixture_legacy_selection
from .restore_probe_report import validate_probe_report_root,persist_restore_probe_report
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_report import _json
from .restore_selection_export import selection_export_descriptor,run_pinned_selection_export
from .restore_acceptance import _require_restore_guard


@dataclass(frozen=True)
class RestoreLegacyExportPlan:
    preset_id: UUID
    preset_revision: int
    preset_sha256: str
    selection_key: str
    export_key: str
    intent_key: str
    params: EncodeParams
    scratch_root: Path
    maximum_seconds: int


def legacy_complete_fixture_descriptor(replay,buckets,installation,plan,srgb_profile,pepper,
                                       maximum_seconds):
    """Pre-Apply pin contains static inputs; dynamic IDs require SQL lineage."""
    if (not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(installation,RestoreLegacyInstallation)
            or not isinstance(plan,RestoreLegacyExportPlan)
            or not isinstance(plan.preset_id,UUID) or plan.preset_id.int==0
            or type(plan.preset_revision) is not int or plan.preset_revision<1
            or not isinstance(plan.preset_sha256,str)
            or re.fullmatch('[0-9a-f]{64}',plan.preset_sha256) is None
            or not isinstance(plan.params,EncodeParams)
            or not isinstance(buckets,(tuple,list)) or not buckets
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or type(plan.maximum_seconds) is not int or not 1<=plan.maximum_seconds<=300
            or plan.maximum_seconds!=installation.maximum_seconds
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=1200
            or type(pepper) is not bytes or len(pepper)<32):
        raise ValueError('RESTORE_LEGACY_COMPLETE_INSTALLATION')
    keys=(installation.preview_target.idempotency_key,installation.apply_target.idempotency_key,
          plan.selection_key,plan.export_key,plan.intent_key)
    if (any(not isinstance(key,str) or not 1<=len(key)<=128
            or any(not 33<=ord(c)<=126 for c in key) for key in keys)
            or len(set(keys))!=5):
        raise ValueError('RESTORE_LEGACY_COMPLETE_KEYS')
    scratch=Path(plan.scratch_root)
    if not scratch.is_absolute() or scratch.resolve(strict=True)!=scratch or not scratch.is_dir():
        raise ValueError('RESTORE_LEGACY_COMPLETE_SCRATCH')
    legacy=legacy_fixture_descriptor(replay,installation,srgb_profile)
    params_template={key:value for key,value in plan.params.model_dump(mode='json',exclude_none=True).items()
                     if key!='image'}
    return _json({'schema_version':'restore-legacy-complete-fixture/v1',
        'legacy':legacy,'preset_id':plan.preset_id,
        'preset_revision':plan.preset_revision,'preset_sha256':plan.preset_sha256,
        'selection_key_sha256':sha256(plan.selection_key.encode()).hexdigest(),
        'export_key_sha256':sha256(plan.export_key.encode()).hexdigest(),
        'intent_key_sha256':sha256(plan.intent_key.encode()).hexdigest(),
        'encode_params_template':params_template,
        'encode_params_template_sha256':sha256(canonical_bytes(params_template)).hexdigest(),
        'scratch_root':scratch,'buckets':sorted(set(buckets)),
        'export_maximum_seconds':plan.maximum_seconds,
        'maximum_seconds':maximum_seconds,
        'pepper_sha256':sha256(pepper).hexdigest()})


def run_legacy_apply_probe(replay,fixture_sha256,*,report_root,connection,client,buckets,
                           installation,plan,srgb_profile,icc_registry,pepper,tls_context,
                           maximum_seconds,assert_installed_fixture,check_active):
    """Never retry after a stage's unknown POST or one-shot intent redemption."""
    if (not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=1200
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_COMPLETE_INSTALLATION')
    _require_restore_guard(check_active,'RESTORE_LEGACY_COMPLETE_INACTIVE')
    root=validate_probe_report_root(report_root)
    descriptor=legacy_complete_fixture_descriptor(replay,buckets,installation,plan,srgb_profile,
        pepper,maximum_seconds)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_LEGACY_COMPLETE_PIN')
    stem=str(replay.run_id)+'.legacy_apply'
    if os.path.lexists(root/(stem+'.json')) or os.path.lexists(root/(stem+'.pending')):
        raise ValueError('RESTORE_LEGACY_COMPLETE_ALREADY_REPORTED')
    deadline=monotonic()+maximum_seconds
    def guard():
        _require_restore_guard(check_active,'RESTORE_LEGACY_COMPLETE_INACTIVE')
        _require_restore_guard(assert_installed_fixture,'RESTORE_LEGACY_COMPLETE_FIXTURE')
        _require_restore_guard(check_active,'RESTORE_LEGACY_COMPLETE_INACTIVE')
        if monotonic()>=deadline:raise ValueError('RESTORE_LEGACY_COMPLETE_TIMEOUT')
    # Validate all later native targets and storage before the first mutation.
    i=installation
    template=RestoreExportTarget(i.apply_target.origin,i.apply_target.tenant_id,
        plan.preset_id,plan.preset_id,plan.export_key,i.apply_target.authorization)
    validate_fixture_export(template,tls_context=tls_context,
        assert_fixture_scope=guard,check_active=guard)
    validate_fixture_download_intent(RestoreDownloadIntentTarget(template.origin,
        template.tenant_id,plan.preset_id,1,plan.intent_key,template.authorization),
        tls_context=tls_context,assert_fixture_scope=guard,check_active=guard)
    guard();started=datetime.now(timezone.utc)
    if started<replay.completed_at:raise ValueError('RESTORE_LEGACY_COMPLETE_TIME')
    legacy=run_pinned_legacy_observations(replay,sha256(canonical_bytes(descriptor['legacy'])).hexdigest(),
        connection,client,buckets,i,srgb_profile=srgb_profile,icc_registry=icc_registry,
        tls_context=tls_context,assert_installed_fixture=guard,check_active=guard)
    if (legacy.apply.download.artifact_id!=str(legacy.apply.result.artifact_id)
            or legacy.apply.download.sha256!=legacy.apply.result.object.sha256):
        raise ValueError('RESTORE_LEGACY_COMPLETE_APPLY_DOWNLOAD')
    guard()
    selection=bind_fixture_legacy_selection(connection,i.apply_target,i.binding,legacy.apply.result,
        preset_id=plan.preset_id,preset_revision=plan.preset_revision,
        preset_sha256=plan.preset_sha256,selection_key=plan.selection_key,
        expected_database=replay.database,assert_fixture_scope=guard,check_active=guard)
    applied_ref=ArtifactRef(artifact_id=legacy.apply.result.artifact_id,
                            sha256=legacy.apply.result.object.sha256)
    params=plan.params.model_copy(update={'image':applied_ref})
    dynamic=selection_export_descriptor(replay,selection.target,plan.preset_id,
        plan.export_key,i.binding,selection.expected,i.apply_target,legacy.apply.ticket,
        i.download_target,params=params,intent_key=plan.intent_key,
        scratch_root=plan.scratch_root,maximum_seconds=plan.maximum_seconds,
        alpha_dtype=i.alpha_dtype)
    dynamic_sha=sha256(canonical_bytes(dynamic)).hexdigest()
    exported=run_pinned_selection_export(replay,dynamic_sha,connection,client,buckets,
        selection.target,plan.preset_id,plan.export_key,i.binding,selection.expected,
        i.apply_target,legacy.apply.ticket,i.download_target,params=params,
        intent_key=plan.intent_key,scratch_root=plan.scratch_root,
        maximum_seconds=plan.maximum_seconds,pepper=pepper,tls_context=tls_context,
        assert_installed_fixture=guard,check_active=guard,alpha_dtype=i.alpha_dtype)
    guard()
    completed=exported.export
    encoded=completed.physical.result
    volume=completed.delivery.volume
    downloaded=completed.delivery.download
    if (exported.fixture_sha256!=dynamic_sha
            or exported.selection.ticket.digest!=selection.expected.snapshot_digest
            or completed.physical.sha256!=encoded.object.sha256
            or completed.physical.bytes_verified!=encoded.object.size
            or downloaded.artifact_id!=str(volume.artifact_id)
            or downloaded.sha256!=volume.object.sha256
            or downloaded.bytes_verified!=volume.object.size
            or volume.delivery_kind!=encoded.delivery_kind
            or (encoded.delivery_kind=='single' and
                (completed.archive is not None or volume.object.sha256!=encoded.object.sha256))
            or (encoded.delivery_kind=='zip' and completed.archive is None)
            or (completed.archive is not None and
                (completed.archive.archive_sha256!=volume.object.sha256
                 or completed.archive.encoded_sha256!=encoded.object.sha256))):
        raise ValueError('RESTORE_LEGACY_COMPLETE_DELIVERY')
    current=bind_fixture_legacy_selection(connection,i.apply_target,i.binding,legacy.apply.result,
        preset_id=plan.preset_id,preset_revision=plan.preset_revision,
        preset_sha256=plan.preset_sha256,selection_key=plan.selection_key,
        expected_database=replay.database,assert_fixture_scope=guard,check_active=guard)
    if current!=selection:raise ValueError('RESTORE_LEGACY_COMPLETE_CHANGED')
    guard();finished=datetime.now(timezone.utc)
    observations={'fixture':descriptor,'dynamic_selection_export':_json(dynamic),
        'dynamic_selection_export_sha256':dynamic_sha,
        'apply_run_id':str(legacy.apply.ticket.run_id),
        'apply_output_collection_id':str(legacy.apply.ticket.output_collection_id),
        'apply_document_artifact_id':str(legacy.apply.result.artifact_id),
        'selection_snapshot_id':str(exported.selection.ticket.snapshot_id),
        'selection_digest':selection.expected.snapshot_digest,
        'encoded_artifact_id':str(encoded.artifact_id),
        'encoded_sha256':completed.physical.sha256,
        'volume_artifact_id':str(volume.artifact_id),
        'delivery_kind':encoded.delivery_kind,
        'archive_sha256':completed.archive.archive_sha256 if completed.archive is not None else None,
        'volume_sha256':volume.object.sha256,
        'download_sha256':downloaded.sha256,
        'preview_sha256':legacy.preview.download.sha256,
        'apply_document_sha256':legacy.apply.download.sha256,
        'preview_polls':legacy.preview.polls,'apply_polls':legacy.apply.polls,
        'export_polls':completed.polls}
    return persist_restore_probe_report(root,replay,'legacy_apply',fixture_sha256,
        started,finished,observations,check_active=guard)
