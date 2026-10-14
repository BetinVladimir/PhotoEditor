from copy import deepcopy
from datetime import datetime,timezone
from uuid import uuid4

import pytest

from photo_worker.runtime.restore_acceptance import _validate_legacy_apply_observations
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from restore_legacy_fixture import legacy_probe_observations


@pytest.mark.parametrize('mode',['valid','foreign_replay','wrong_pin','changed_volume',
                                 'wrong_export_key','wrong_apply_ticket','missing_dynamic',
                                 'zip_without_archive','changed_template'])
def test_legacy_probe_report_rechecks_dynamic_selection_and_encoded_delivery(mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    pin,original=legacy_probe_observations(replay)
    observed=deepcopy(original)
    if mode=='foreign_replay':observed['fixture']['legacy']['apply']['restore_run_id']=str(uuid4())
    if mode=='wrong_pin':pin='0'*64
    if mode=='changed_volume':observed['download_sha256']='f'*64
    if mode=='wrong_export_key':observed['dynamic_selection_export']['request_key_sha256']='f'*64
    if mode=='wrong_apply_ticket':observed['dynamic_selection_export']['apply_ticket']['run_id']=str(uuid4())
    if mode=='missing_dynamic':observed.pop('dynamic_selection_export')
    if mode=='zip_without_archive':observed['delivery_kind']='zip'
    if mode=='changed_template':observed['fixture']['encode_params_template']['quality']=1
    if mode=='valid':_validate_legacy_apply_observations(observed,replay,pin)
    else:
        with pytest.raises(ValueError):_validate_legacy_apply_observations(observed,replay,pin)
