from contextlib import contextmanager
from dataclasses import replace
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_revoke_independence as m


@pytest.mark.parametrize('mode',['valid','same_version','same_consent','foreign_project','cross_link','authority','inactive'])
def test_fixture_independence_precedes_any_mutation(mode):
    first=m.RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    second=replace(first,consent_id=uuid4(),learning_version_id=uuid4())
    if mode=='same_version':second=replace(second,learning_version_id=first.learning_version_id)
    if mode=='same_consent':second=replace(second,consent_id=first.consent_id)
    if mode=='foreign_project':second=replace(second,project_id=uuid4())
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            calls.append(sql)
            row=('restore',mode!='authority') if sql.startswith('SELECT current_database') else (mode=='cross_link',)
            return NS(fetchone=lambda:row)
    def run():m.verify_revoke_fixture_independence(Connection(),first,second,expected_database='restore',
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':run()
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('same_version','same_consent','foreign_project','inactive'):assert calls==[]
    assert all(sql.startswith(('SET ','SELECT ')) for sql in calls)
