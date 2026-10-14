from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_export_catalogue as m


@pytest.mark.parametrize('mode',['valid','authority','receipt','registration','preset','config','inactive'])
def test_export_registration_frozen_config_and_canonical_saved_response(mode):
    target=m.RestoreExportTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    job,run=uuid4(),uuid4();digest=lambda v:sha256(canonical_bytes(v)).hexdigest()
    preset_config={'format':'jpeg'};selection=m.RestoreExportSelection('b'*64,1,digest(preset_config))
    response=canonical_bytes({'download_job_id':str(job),'status':'queued'})
    fingerprint=sha256(m.export_request_body(target)).hexdigest()
    ticket=m.RestoreExportTicket(job,fingerprint,sha256(response+b'\n').hexdigest())
    preset={'id':str(target.preset_id),'revision':2 if mode=='preset' else 1,'hash':selection.preset_sha256,'config':preset_config}
    config={'schema_version':1,'pipeline':'download','pipeline_version':3,'input_snapshot_id':str(target.selection_snapshot_id),
        'input_snapshot_digest':selection.snapshot_digest,'preset_id':str(target.preset_id),'preset_revision':1,
        'preset_hash':selection.preset_sha256,'preset':preset_config,'failure_policy':'continue_items',
        'scope':'all' if mode=='config' else 'selected'}
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT request_hash'):row=(fingerprint,200 if mode=='receipt' else 202,response,job)
            elif sql.startswith('SELECT r.id'):row=None if mode=='registration' else (run,config,digest(config),preset)
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.observe_fixture_export_registration(Connection(),target,binding,ticket,selection,
        expected_database='restore',check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().run_id==run and ticket.response_sha256!=sha256(response).hexdigest()
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls
