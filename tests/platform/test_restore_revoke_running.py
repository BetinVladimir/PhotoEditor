from contextlib import contextmanager
from dataclasses import replace
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.runtime import restore_revoke_running as module
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode',['valid','missing','zero_attempt','boolean_fence','foreign_database','unprivileged','busy','scope_lost'])
def test_running_witness_requires_live_generation_and_exact_database(mode):
    fixture=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    child=uuid4();queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.INTRANS if mode=='busy' else psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):
                row=('other' if mode=='foreign_database' else 'restore',mode!='unprivileged')
            elif sql.startswith('SELECT t.attempt'):
                assert args==(fixture.tenant_id,child,uuid5(child,'workflow'),fixture.project_id,
                    uuid5(uuid5(child,'workflow'),'render.compose:0'),fixture.consent_id,fixture.epoch,
                    fixture.purpose,fixture.person_id)
                row=None if mode=='missing' else (0 if mode=='zero_attempt' else 2,True if mode=='boolean_fence' else 7)
            else:row=None
            return NS(fetchone=lambda:row)
    def scope(*args):
        assert args==(fixture,child)
        if mode=='scope_lost':raise ValueError('lost fence')
    def run():return module.observe_fixture_running_render(Connection(),fixture,child,
        expected_database='restore',assert_fixture_scope=scope,check_active=lambda:None)
    if mode=='valid':
        result=run()
        assert result.attempt==2 and result.fence==7 and result.child_run_id==child
    else:
        with pytest.raises(ValueError):run()
    if mode in ('busy','scope_lost'):assert queries==[]


@pytest.mark.parametrize('mode',['valid','not_settled','foreign_database','foreign_tenant','changed_task','zero_fence','busy'])
def test_cancelled_witness_requires_same_generation_and_absent_result(mode):
    fixture=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    child=uuid4();workflow=uuid5(child,'workflow');task=uuid5(workflow,'render.compose:0')
    witness=module.RestoreRunningRenderWitness('restore',fixture.tenant_id,child,workflow,task,2,7)
    if mode=='foreign_tenant':witness=replace(witness,tenant_id=uuid4())
    if mode=='changed_task':witness=replace(witness,task_id=uuid4())
    if mode=='zero_fence':witness=replace(witness,fence=0)
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.INTRANS if mode=='busy' else psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('other' if mode=='foreign_database' else 'restore',True)
            elif sql.startswith('SELECT EXISTS'):
                assert args==(fixture.tenant_id,child,workflow,fixture.project_id,task,2,7,
                    fixture.consent_id,fixture.epoch,fixture.purpose,fixture.person_id)
                row=(mode!='not_settled',)
            else:row=None
            return NS(fetchone=lambda:row)
    def run():return module.observe_fixture_cancelled_render(Connection(),fixture,witness,
        assert_fixture_scope=lambda *a:None,check_active=lambda:None)
    if mode=='valid':
        result=run();assert result.running==witness and result.result_absent and result.state=='cancelled'
    else:
        with pytest.raises(ValueError):run()
    if mode in ('foreign_tenant','changed_task','zero_fence','busy'):assert queries==[]


@pytest.mark.parametrize('observer',['running','cancelled'])
@pytest.mark.parametrize('denial',['activity','fixture'])
def test_render_revoke_witness_explicit_false_refuses_before_sql(observer,denial):
    fixture=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    child=uuid4();workflow=uuid5(child,'workflow');task=uuid5(workflow,'render.compose:0')
    witness=module.RestoreRunningRenderWitness('restore',fixture.tenant_id,child,workflow,task,2,7)
    class NoSQL:
        def __getattribute__(self,name):raise AssertionError('SQL reached')
    kwargs=dict(assert_fixture_scope=lambda *_:False if denial=='fixture' else None,
                check_active=lambda:False if denial=='activity' else None)
    with pytest.raises(RuntimeError):
        if observer=='running':
            module.observe_fixture_running_render(NoSQL(),fixture,child,expected_database='restore',**kwargs)
        else:
            module.observe_fixture_cancelled_render(NoSQL(),fixture,witness,**kwargs)
