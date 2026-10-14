"""Canonical V2 restore probe document fixture for acceptance reader tests."""
from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime.restore_revoke_batch import RestoreRenderBatchTarget
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeBaselineEvidence
from photo_worker.runtime.restore_v2_workflow_runner import v2_workflow_fixture_descriptor


def v2_probe_observations(replay):
    tenant,project,snapshot,actor,version,source,recipe,session=(uuid4() for _ in range(8))
    target=RestoreRenderBatchTarget('https://api.test',tenant,project,snapshot,'d'*64,
        'fixture-key','Bearer private')
    binding=RestoreFreezeBaselineBinding(actor,version,source,'e'*64,'f'*64)
    frozen=RestoreFreezeBaselineEvidence('v1',uuid4(),recipe,'0'*64,'1'*64,'2'*64,
        datetime.now(timezone.utc))
    download=RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private')
    fixture=v2_workflow_fixture_descriptor(replay,('photos',),target,binding,frozen,
        download,session,b'P'*128,30)
    observations={'fixture':fixture,'batch_id':str(uuid4()),'child_run_id':str(uuid4()),
        'review_id':str(uuid4()),'candidate_sha256':'3'*64,
        'before_preview_sha256':'4'*64,'after_preview_sha256':'5'*64,
        'after_manifest_sha256':'6'*64,'download_sha256':'5'*64,
        'download_manifest_sha256':'6'*64,'batch_polls':1,'terminal_polls':1}
    return sha256(canonical_bytes(fixture)).hexdigest(),observations
