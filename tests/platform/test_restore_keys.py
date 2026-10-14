from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_keys import verify_restored_key_versions


@pytest.mark.parametrize('mode',['valid','missing','truthy','foreign','filtered','limit','duplicate','cancelled'])
def test_referenced_key_versions_require_real_probe_ack_and_privileged_catalogue(mode):
    events=[];versions=[('capture','capture-v2'),('mail','mail-v1')]
    if mode=='duplicate':versions=[versions[0],versions[0]]
    class Cursor:
        itersize=0
        def __enter__(self):return self
        def __exit__(self,*args):events.append('closed')
        def execute(self,sql):assert self.itersize==128 and 'nonce_ciphertext' in sql
        def __iter__(self):return iter(versions)
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            return SimpleNamespace(fetchone=lambda:('foreign' if mode=='foreign' else 'isolated',mode!='filtered'))
        def cursor(self,*,name):return Cursor()
    def probe(domain,key):
        events.append((domain,key))
        return 1 if mode=='truthy' else mode!='missing'
    def active():
        if mode=='cancelled' and any(isinstance(e,tuple) for e in events):raise RuntimeError('cancelled')
    def run():return verify_restored_key_versions(Connection(),expected_database='isolated',
        verify_key_version=probe,assert_isolated_and_paused=lambda:None,check_active=active,
        maximum_versions=1 if mode=='limit' else 4096)
    if mode=='valid':assert run().versions==tuple(versions)
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('foreign','filtered'):assert not any(isinstance(e,tuple) for e in events)


@pytest.mark.parametrize('denial',['activity','isolation'])
def test_key_catalogue_explicit_false_refuses_before_sql_or_probe(denial):
    class NoSQL:
        def __getattribute__(self,name):raise AssertionError('SQL touched before authority')
    def no_probe(*_):raise AssertionError('key probe reached')
    with pytest.raises(RuntimeError):
        verify_restored_key_versions(NoSQL(),expected_database='isolated',
            verify_key_version=no_probe,
            assert_isolated_and_paused=lambda:False if denial=='isolation' else None,
            check_active=lambda:False if denial=='activity' else None)
