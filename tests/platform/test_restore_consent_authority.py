from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_consent_authority import SQLCurrentConsentAuthority


@pytest.mark.parametrize('mode',['valid','foreign','filtered','partial','limit','changed_second_pass','cancelled'])
def test_current_authority_hash_and_stream_share_snapshot_and_require_full_consumption(mode):
    events=[]
    row=(uuid4(),uuid4(),uuid4(),'learning_photographer',None,'revoked',3,4,'v2')
    class Cursor:
        itersize=0
        def __init__(self,name):self.name=name
        def __enter__(self):events.append(self.name);return self
        def __exit__(self,*args):events.append('closed')
        def execute(self,sql):assert self.itersize==512 and sql.endswith('ORDER BY tenant_id,id')
        def __iter__(self):
            if mode=='changed_second_pass' and self.name=='current_consent_catalogue':
                yield (*row[:5],'granted',*row[6:])
            else:yield row
            if mode=='limit':yield row
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql)
            return SimpleNamespace(fetchone=lambda:('foreign' if mode=='foreign' else 'authority',mode!='filtered'))
        def cursor(self,*,name):return Cursor(name)
    def active():
        if mode=='cancelled' and 'current_consent_digest' in events:raise RuntimeError('cancelled')
    authority=SQLCurrentConsentAuthority(Connection(),expected_database='authority',
        assert_current_authority=lambda:None,check_active=active,maximum_grants=1)
    def run():
        with authority.snapshot() as (pin,rows):
            assert pin.grants==1
            if mode!='partial':assert len(list(rows))==1
    if mode=='valid':
        run();assert events.count('begin')==1 and events.count('closed')==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert events[-1]=='end'
