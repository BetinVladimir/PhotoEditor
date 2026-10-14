from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_acceptance import _validate_v2_workflow_observations
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime.restore_revoke_batch import RestoreRenderBatchTarget
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeBaselineEvidence
from photo_worker.runtime.restore_v2_workflow_runner import v2_workflow_fixture_descriptor


@pytest.mark.parametrize('mode',['valid','foreign_replay','wrong_pin','changed_download',
                                 'missing_manifest','zero_poll','extra_field'])
def test_v2_probe_report_rechecks_exact_fixture_and_delivered_presentation(mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    tenant,project,snapshot,actor,version,source,recipe,session=(uuid4() for _ in range(8))
    target=RestoreRenderBatchTarget('https://api.test',tenant,project,snapshot,'d'*64,
        'unique-key','Bearer private')
    binding=RestoreFreezeBaselineBinding(actor,version,source,'e'*64,'f'*64)
    frozen=RestoreFreezeBaselineEvidence('v1',uuid4(),recipe,'0'*64,'1'*64,'2'*64,
        datetime.now(timezone.utc))
    download=RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private')
    fixture=v2_workflow_fixture_descriptor(replay,('photos',),target,binding,frozen,
        download,session,b'P'*128,30)
    pin=sha256(canonical_bytes(fixture)).hexdigest()
    values={'fixture':fixture,'batch_id':str(uuid4()),'child_run_id':str(uuid4()),
        'review_id':str(uuid4()),'candidate_sha256':'3'*64,
        'before_preview_sha256':'4'*64,'after_preview_sha256':'5'*64,
        'after_manifest_sha256':'6'*64,'download_sha256':'5'*64,
        'download_manifest_sha256':'6'*64,'batch_polls':2,'terminal_polls':1}
    if mode=='foreign_replay':fixture['restore_run_id']=str(uuid4())
    if mode=='wrong_pin':pin='7'*64
    if mode=='changed_download':values['download_sha256']='8'*64
    if mode=='missing_manifest':values.pop('after_manifest_sha256')
    if mode=='zero_poll':values['terminal_polls']=0
    if mode=='extra_field':values['claim']='unverified'
    if mode=='valid':_validate_v2_workflow_observations(values,replay,pin)
    else:
        with pytest.raises(ValueError):_validate_v2_workflow_observations(values,replay,pin)
