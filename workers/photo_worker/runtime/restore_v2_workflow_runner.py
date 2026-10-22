"""One-frame V2 restore probe from fresh admission to verified delivery."""
from dataclasses import dataclass
from datetime import datetime,timezone
from hashlib import sha256
import os
from math import ceil
import re
from time import monotonic,sleep
from uuid import UUID

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from .restore_batch_baseline import run_fixture_batch_baseline
from .restore_render_review_pending import find_fixture_pending_render_review
from .restore_render_review_previews import run_fixture_render_review_previews
from .restore_render_review_lifecycle import run_fixture_render_review
from .restore_render_terminal import observe_fixture_render_terminal
from .restore_render_output_download import download_fixture_render_output
from .restore_preview_download import RestorePreviewDownloadTarget,validate_preview_download_installation
from .restore_probe_report import validate_probe_report_root,persist_restore_probe_report
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_revoke_batch import RestoreRenderBatchTarget
from .restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeBaselineEvidence
from .restore_acceptance import _require_restore_guard


@dataclass(frozen=True)
class RestoreV2WorkflowResult:
    batch: object
    previews: object
    review: object
    terminal: object
    download: object
    terminal_polls: int


def run_fixture_v2_workflow(connection,client,buckets,target,binding,frozen,source_raw,
                            download_target,*,expected_database,session_id,srgb_profile,
                            tls_context,maximum_seconds,assert_installed_fixture,check_active):
    """One POST for batch and one POST for review; no uncertain-outcome retry."""
    if (not isinstance(target,RestoreRenderBatchTarget)
            or not isinstance(binding,RestoreFreezeBaselineBinding)
            or not isinstance(frozen,RestoreFreezeBaselineEvidence)
            or not isinstance(download_target,RestorePreviewDownloadTarget)
            or target.tenant_id.int==0 or binding.actor_id.int==0
            or target.origin!=download_target.api_origin
            or target.authorization!=download_target.authorization
            or not isinstance(expected_database,str) or not expected_database
            or not isinstance(session_id,UUID) or session_id.int==0
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=1200
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_V2_WORKFLOW_INSTALLATION')
    validate_preview_download_installation(download_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    deadline=monotonic()+maximum_seconds
    def guard():
        _require_restore_guard(check_active,'RESTORE_V2_WORKFLOW_INACTIVE')
        _require_restore_guard(assert_installed_fixture,'RESTORE_V2_WORKFLOW_FIXTURE')
        _require_restore_guard(check_active,'RESTORE_V2_WORKFLOW_INACTIVE')
        if monotonic()>=deadline:raise ValueError('RESTORE_V2_WORKFLOW_TIMEOUT')
    def remaining():
        guard();return min(300,max(1,ceil(deadline-monotonic())))
    source_binding=VersionBinding(asset_version_id=binding.asset_version_id,
        source=ArtifactRef(artifact_id=binding.source_artifact_id,sha256=binding.source_sha256),
        canvas_hash=binding.canvas_hash)
    guard()
    batch=run_fixture_batch_baseline(connection,client,buckets,target,binding,frozen,source_raw,
        expected_database=expected_database,session_id=session_id,tls_context=tls_context,
        maximum_seconds=remaining(),assert_installed_fixture=guard,check_active=check_active)
    guard()
    pending=find_fixture_pending_render_review(connection,target.tenant_id,batch.adoption,batch.physical,
        assert_fixture_scope=guard,check_active=check_active)
    previews=run_fixture_render_review_previews(connection,target.tenant_id,batch.adoption,
        batch.physical,pending,source_binding,source_raw,buckets,download_target,
        srgb_profile=srgb_profile,tls_context=tls_context,maximum_seconds=remaining(),
        assert_installed_fixture=guard,check_active=check_active)
    if previews.pending!=pending:raise ValueError('RESTORE_V2_WORKFLOW_PREVIEWS_CHANGED')
    guard()
    review=run_fixture_render_review(connection,target.tenant_id,binding.actor_id,
        batch.adoption,batch.physical,origin=target.origin,authorization=target.authorization,
        tls_context=tls_context,maximum_seconds=remaining(),assert_installed_fixture=guard,
        check_active=check_active)
    if review.pending!=pending or review.observed.candidate!=pending.candidate:
        raise ValueError('RESTORE_V2_WORKFLOW_REVIEW_CHANGED')
    polls=0
    while True:
        guard();polls+=1
        try:
            terminal=observe_fixture_render_terminal(connection,target.tenant_id,binding.actor_id,
                batch.adoption,review.observed,assert_fixture_scope=guard,check_active=check_active)
            break
        except ValueError as exc:
            if str(exc)!='RESTORE_RENDER_TERMINAL_NOT_COMPLETE':raise
        guard();sleep(min(.25,max(0,deadline-monotonic())))
    guard()
    download=download_fixture_render_output(connection,download_target,target.tenant_id,
        binding.actor_id,batch.adoption,review.observed,terminal,previews.after_catalogue,
        tls_context=tls_context,assert_installed_fixture=guard,check_active=check_active)
    guard()
    return RestoreV2WorkflowResult(batch,previews,review,terminal,download,polls)


def v2_workflow_fixture_descriptor(replay,buckets,target,binding,frozen,download_target,
                                   session_id,srgb_profile,maximum_seconds):
    """Credential-free stable identity of the installed positive render fixture."""
    if (not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(target,RestoreRenderBatchTarget)
            or not isinstance(binding,RestoreFreezeBaselineBinding)
            or not isinstance(frozen,RestoreFreezeBaselineEvidence)
            or not isinstance(download_target,RestorePreviewDownloadTarget)
            or not isinstance(buckets,(tuple,list)) or not buckets
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or not isinstance(session_id,UUID) or session_id.int==0
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=1200
            or target.origin!=download_target.api_origin
            or target.authorization!=download_target.authorization):
        raise ValueError('RESTORE_V2_WORKFLOW_FIXTURE')
    return _json({'schema_version':'restore-v2-workflow-fixture/v1',
        'restore_run_id':replay.run_id,'database':replay.database,
        'build_sha256':replay.build_sha256,'recovery_sha256':replay.recovery_sha256,
        'replay_sha256':replay.replay_sha256,
        'tenant_id':target.tenant_id,'project_id':target.project_id,
        'snapshot_id':target.snapshot_id,'policy_sha256':target.policy_sha256,
        'idempotency_key_sha256':sha256(target.idempotency_key.encode()).hexdigest(),
        'actor_id':binding.actor_id,'asset_version_id':binding.asset_version_id,
        'source_artifact_id':binding.source_artifact_id,'source_sha256':binding.source_sha256,
        'canvas_hash':binding.canvas_hash,'frozen_document_id':frozen.frozen_document_id,
        'recipe_artifact_id':frozen.recipe_artifact_id,'recipe_sha256':frozen.recipe_sha256,
        'session_id':session_id,'api_origin':target.origin,
        'object_origin':download_target.object_origin,'buckets':sorted(set(buckets)),
        'srgb_sha256':sha256(srgb_profile).hexdigest(),'maximum_seconds':maximum_seconds})


def run_fixture_v2_workflow_report(report_root,replay,fixture_sha256,connection,client,buckets,
                                   target,binding,frozen,source_raw,download_target,*,
                                   session_id,srgb_profile,tls_context,maximum_seconds,
                                   assert_installed_fixture,check_active):
    """Publish a private all-true probe receipt only after every live stage."""
    if (not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(fixture_sha256,str)
            or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(check_active)
            or not callable(assert_installed_fixture)):
        raise ValueError('RESTORE_V2_WORKFLOW_REPLAY')
    _require_restore_guard(check_active,'RESTORE_V2_WORKFLOW_INACTIVE')
    root=validate_probe_report_root(report_root)
    descriptor=v2_workflow_fixture_descriptor(replay,buckets,target,binding,frozen,
        download_target,session_id,srgb_profile,maximum_seconds)
    raw=canonical_bytes(descriptor)
    if len(raw)>131072 or sha256(raw).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_V2_WORKFLOW_FIXTURE_PIN')
    stem=str(replay.run_id)+'.v2_workflow'
    if os.path.lexists(root/(stem+'.json')) or os.path.lexists(root/(stem+'.pending')):
        raise ValueError('RESTORE_V2_WORKFLOW_ALREADY_REPORTED')
    started=datetime.now(timezone.utc)
    if started<replay.completed_at:raise ValueError('RESTORE_V2_WORKFLOW_TIME')
    result=run_fixture_v2_workflow(connection,client,buckets,target,binding,frozen,
        source_raw,download_target,expected_database=replay.database,session_id=session_id,
        srgb_profile=srgb_profile,tls_context=tls_context,maximum_seconds=maximum_seconds,
        assert_installed_fixture=assert_installed_fixture,check_active=check_active)
    finished=datetime.now(timezone.utc)
    observations={
        'fixture':descriptor,
        'batch_id':str(result.batch.adoption.batch_id),
        'child_run_id':str(result.batch.adoption.child_run_id),
        'review_id':str(result.review.observed.review_id),
        'candidate_sha256':result.review.observed.candidate.sha256,
        'before_preview_sha256':result.previews.before.verified.image.sha256,
        'after_preview_sha256':result.previews.after.verified.image.sha256,
        'after_manifest_sha256':result.previews.after.verified.manifest.sha256,
        'download_sha256':result.download.derivative_sha256,
        'download_manifest_sha256':result.download.manifest_sha256,
        'batch_polls':result.batch.polls,'terminal_polls':result.terminal_polls}
    if (result.download.child_run_id!=result.batch.adoption.child_run_id
            or result.download.derivative_sha256!=result.previews.after.verified.image.sha256
            or result.download.manifest_sha256!=result.previews.after.verified.manifest.sha256):
        raise ValueError('RESTORE_V2_WORKFLOW_DOWNLOAD_CHANGED')
    def guard():
        _require_restore_guard(check_active,'RESTORE_V2_WORKFLOW_INACTIVE')
        _require_restore_guard(assert_installed_fixture,'RESTORE_V2_WORKFLOW_FIXTURE')
        _require_restore_guard(check_active,'RESTORE_V2_WORKFLOW_INACTIVE')
    guard()
    return persist_restore_probe_report(root,replay,'v2_workflow',fixture_sha256,
        started,finished,observations,check_active=guard)
