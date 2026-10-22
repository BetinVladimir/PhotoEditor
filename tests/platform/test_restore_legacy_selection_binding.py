from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4

import psycopg
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_legacy_apply import RestoreLegacyApplyTarget
from photo_worker.runtime.restore_legacy_catalogue import RestoreLegacyApplyBinding,RestoreLegacyApplyRegistration
from photo_worker.runtime.restore_legacy_result import RestoreLegacyApplyResult
from photo_worker.runtime.restore_export_preflight import one_item_selection_digest
from photo_worker.runtime.restore_legacy_selection_binding import bind_fixture_legacy_selection


@pytest.mark.parametrize('mode',['valid','max_revision','missing','duplicate','foreign_database',
                                 'changed_preset','changed_payload','cancelled','inactive','fixture_denied'])
def test_selection_binding_uses_exact_dynamic_apply_output_and_pinned_preset(mode):
    tenant,project,version,source,collection,item,style,actor,run,output,new_version,artifact,preset=(uuid4() for _ in range(13))
    apply=RestoreLegacyApplyTarget('https://api.test',tenant,collection,item,style,1,
        'apply-key','Bearer private')
    binding=RestoreLegacyApplyBinding(actor,project,version,source,'a'*64)
    registration=RestoreLegacyApplyRegistration('isolated',run,output,uuid4(),'b'*64)
    obj=RestoreObject('photos','tenants/'+str(tenant)+'/image','v1','c'*64,128)
    applied=RestoreLegacyApplyResult(registration,uuid4(),new_version,artifact,obj)
    payload={'position':0,'captured_at':None,'camera_hash':None}
    config={'format':'jpeg'};preset_sha=sha256(canonical_bytes(config)).hexdigest()
    revision=9007199254740991 if mode=='max_revision' else 2
    row=(revision,payload,1,'f'*64 if mode=='changed_preset' else preset_sha,config)
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,params=None):
            if 'FROM photo.collections c' in sql:
                assert 'i.created_by_run_id=c.source_run_id' in sql
                assert '(SELECT count(*) FROM photo.collection_items' in sql
                assert params==(preset,tenant,output,project,run,collection,
                                applied.item_id,new_version,version,artifact,obj.sha256)
                if mode=='changed_payload':
                    return NS(fetchall=lambda:[(2,{'position':'wrong'},1,preset_sha,config)])
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
        if mode=='inactive':return False
    def bind_selection():return bind_fixture_legacy_selection(Connection(),apply,binding,applied,
        preset_id=preset,preset_revision=1,preset_sha256=preset_sha,selection_key='selection-key',
        expected_database='isolated',assert_fixture_scope=lambda:False if mode=='fixture_denied' else None,
        check_active=active)
    if mode in ('valid','max_revision'):
        observed=bind_selection()
        assert observed.target.collection_id==output and observed.target.item_id==applied.item_id
        assert observed.target.collection_revision==revision
        assert observed.expected.snapshot_digest==one_item_selection_digest(applied,payload)
        assert observed.payload_sha256==sha256(canonical_bytes(payload)).hexdigest()
    else:
        with pytest.raises((ValueError,RuntimeError)):bind_selection()
    if mode in ('inactive','fixture_denied'):assert not calls
