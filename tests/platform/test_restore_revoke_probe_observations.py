from copy import deepcopy
from datetime import datetime,timezone
from uuid import uuid4
import pytest
from photo_worker.runtime.restore_acceptance import _validate_revoke_negative_observations
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from restore_revoke_fixture import revoke_probe_observations


@pytest.mark.parametrize('mutation',[
    'none','wrong_pin','foreign_replay','same_grant','wrong_child','no_barrier',
    'changed_attempt','result_present','not_cancelled','unrevoked_running',
    'unrevoked_admission','missing_positive_batch','missing_negative_batch',
    'changed_freeze_guard','changed_batch_guard','wrong_frozen_object',
    'wrong_positive_child','wrong_batch_request','wrong_freeze_request',
    'wrong_access_denial','wrong_denied_batch','wrong_physical_task',
    'wrong_child_digest','missing_array','wrong_inventory_digest','wrong_result_object',
    'wrong_result_size','foreign_result_key','wrong_working_image_sha'])
def test_revoke_report_read_side_closure(mutation):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,
        datetime.now(timezone.utc))
    pin,source=revoke_probe_observations(replay);observations=deepcopy(source)
    fixture=observations['fixture'];running=observations['running'];admission=observations['admission']
    if mutation=='wrong_pin':pin='0'*64
    if mutation=='foreign_replay':fixture['running']['replay_sha256']='0'*64
    if mutation=='same_grant':fixture['running']['fixture']['consent_id']=fixture['admission']['fixture']['consent_id']
    if mutation=='wrong_child':running['running']['child_run_id']=str(uuid4())
    if mutation=='no_barrier':running['barrier_cancelled']=False
    if mutation=='changed_attempt':running['cancelled']['running']['attempt']=2
    if mutation=='result_present':running['cancelled']['result_absent']=False
    if mutation=='not_cancelled':running['cancelled']['state']='succeeded'
    if mutation=='unrevoked_running':running['committed_consent']['revoked']=False
    if mutation=='unrevoked_admission':admission['final_consent']['revoked']=False
    if mutation=='missing_positive_batch':admission['positive_batch']=None
    if mutation=='missing_negative_batch':admission['negative_batch']=None
    if mutation=='changed_freeze_guard':admission['denied_freeze'][2][1]['changed']=True
    if mutation=='changed_batch_guard':admission['negative_batch'][2][1]['changed']=True
    if mutation=='wrong_frozen_object':admission['physical_freeze']['object']['sha256']='0'*64
    if mutation=='wrong_positive_child':admission['positive_batch']['ticket']['child_run_id']=str(uuid4())
    if mutation=='wrong_batch_request':admission['positive_batch']['ticket']['request_sha256']='0'*64
    if mutation=='wrong_freeze_request':admission['baseline_freeze']['request_sha256']='0'*64
    if mutation=='wrong_access_denial':admission['revoke']['denied_access']['code']='FORBIDDEN'
    if mutation=='wrong_denied_batch':admission['negative_batch'][1]['code']='OTHER'
    if admission['positive_batch'] is not None:
        physical=admission['positive_batch']['physical']
        if mutation=='wrong_physical_task':physical['result']['task_id']=str(uuid4())
        if mutation=='wrong_child_digest':physical['children'][0]['sha256']='0'*64
        if mutation=='missing_array':physical['arrays']=[]
        if mutation=='wrong_inventory_digest':physical['inventory']['inventory_sha256']='0'*64
        if mutation=='wrong_result_object':physical['result']['object']['sha256']='0'*64
        if mutation=='wrong_result_size':physical['result']['object']['size']+=1
        if mutation=='foreign_result_key':physical['result']['object']['key']='foreign/result'
        if mutation=='wrong_working_image_sha':physical['image_result']['working_image']['sha256']='0'*64
    if mutation=='none':
        _validate_revoke_negative_observations(observations,replay,pin)
    else:
        with pytest.raises(ValueError):
            _validate_revoke_negative_observations(observations,replay,pin)
