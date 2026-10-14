from copy import deepcopy
from datetime import datetime,timezone
from uuid import uuid4
import pytest
from photo_worker.runtime.restore_acceptance import _validate_purge_negative_observations
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from restore_purge_fixture import purge_probe_observations


@pytest.mark.parametrize('mutation',[
    'none','foreign_replay','foreign_job','missing_tables','missing_storage_pass',
    'wrong_inventory','wrong_baseline','wrong_api_denial','wrong_s3_object',
    'missing_prefix','wrong_fixture_pin'])
def test_purge_report_read_side_closure(mutation):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,
        datetime.now(timezone.utc))
    pin,source=purge_probe_observations(replay)
    observations=deepcopy(source)
    if mutation=='foreign_replay':observations['fixture']['replay_sha256']='0'*64
    if mutation=='foreign_job':observations['purge']['metadata']['deletion_job_id']=str(uuid4())
    if mutation=='missing_tables':observations['purge']['metadata']['checked_tables'].remove('tasks')
    if mutation=='missing_storage_pass':observations['purge']['storage']['passes']=1
    if mutation=='wrong_inventory':observations['baseline']['inventory']['inventory_sha256']='0'*64
    if mutation=='wrong_baseline':observations['baseline']['s3']['sha256']='0'*64
    if mutation=='wrong_api_denial':observations['purge']['api_access']['status']=200
    if mutation=='wrong_s3_object':observations['purge']['s3_access']['key']='foreign'
    if mutation=='missing_prefix':observations['purge']['storage']['prefixes'].pop()
    if mutation=='wrong_fixture_pin':pin='0'*64
    if mutation=='none':
        _validate_purge_negative_observations(observations,replay,pin)
    else:
        with pytest.raises(ValueError):
            _validate_purge_negative_observations(observations,replay,pin)
