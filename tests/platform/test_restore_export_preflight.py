from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_export_preflight as m


@pytest.mark.parametrize('mode',['valid','authority','existing','selection','preset_hash','preset_revision','payload','digest','inactive'])
@pytest.mark.parametrize('require_fresh_key',[True,False])
def test_export_preflight_exact_adopted_selection_and_preset(mode,require_fresh_key):
    target=m.RestoreExportTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    applied=m.RestoreLegacyApplyResult(NS(output_collection_id=uuid4(),run_id=uuid4()),uuid4(),uuid4(),uuid4(),
        RestoreObject('photos','tenants/fixture/master','v1','b'*64,512))
    config={'format':'jpeg'};digest=sha256(canonical_bytes(config)).hexdigest()
    payload={'position':7,'captured_at':None,'camera_hash':None}
    entry={'item_id':str(applied.item_id),'asset_version_id':str(applied.asset_version_id),
        'artifact_id':str(applied.artifact_id),'sha256':applied.object.sha256,**payload}
    selection_digest=sha256(canonical_bytes([entry])).hexdigest()
    expected=m.RestoreExportSelection('c'*64 if mode=='digest' else selection_digest,1,digest)
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT EXISTS'):row=(mode=='existing',)
            elif sql.startswith('SELECT p.revision'):
                assert args[-4:]==(applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)
                assert 'ci.asset_version_id=i.asset_version_id' in sql
                assert 'v.asset_id=ci.asset_id' in sql and 'c.revision=s.source_revision' in sql
                row=None if mode=='selection' else (2 if mode=='preset_revision' else 1,'f'*64 if mode=='preset_hash' else digest,config,{'position':True,'captured_at':None,'camera_hash':None} if mode=='payload' else payload)
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.verify_fixture_export_preflight(Connection(),target,binding,applied,expected,
        expected_database='restore',check_active=lambda:False if mode=='inactive' else None,
        require_fresh_key=require_fresh_key)
    if mode=='valid' or (mode=='existing' and not require_fresh_key):assert invoke()==expected
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls


@pytest.mark.parametrize('payload',[
    {'position':False,'captured_at':None,'camera_hash':None},
    {'position':9007199254740992,'captured_at':None,'camera_hash':None},
    {'position':1,'captured_at':None},
    {'position':1,'captured_at':12,'camera_hash':None},
])
def test_selection_digest_rejects_non_native_payload(payload):
    applied=m.RestoreLegacyApplyResult(NS(),uuid4(),uuid4(),uuid4(),RestoreObject('photo','key','v1','a'*64,12))
    with pytest.raises(ValueError,match='SELECTION_PAYLOAD'):m.one_item_selection_digest(applied,payload)


def test_selection_digest_includes_explicit_nulls_and_metadata():
    applied=m.RestoreLegacyApplyResult(NS(),uuid4(),uuid4(),uuid4(),RestoreObject('photo','key','v1','a'*64,12))
    payload={'position':7,'captured_at':None,'camera_hash':None}
    first=m.one_item_selection_digest(applied,payload)
    assert first!=m.one_item_selection_digest(applied,{**payload,'position':8})
    assert first!=m.one_item_selection_digest(applied,{**payload,'camera_hash':'camera'})
    assert first!=m.one_item_selection_digest(applied,{**payload,'captured_at':'2026-10-11T00:00:00Z'})
