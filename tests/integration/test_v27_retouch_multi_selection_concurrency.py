"""Multi-target lock scenarios in conftest's disposable private database.

No external-DSN fixture import: committed setup lives only in the dropped DB.
These scenarios are authored, not evidence of an executed concurrency pass.
"""
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic
import json

import psycopg
import pytest
from retouch_multi_selection_fixture import multi_selection_fixture
from retouch_copy_apply_fixture import publish_retouch_decision_bundles
from test_v27_retouch_apply_decision_gate import lock_decision
from test_v27_retouch_apply_atomic_commit import snapshot


def setup(config):
    with psycopg.connect(**config) as db:
        bundles,args=multi_selection_fixture(db,with_alpha=True)
        request,artifact,body=publish_retouch_decision_bundles(db,bundles,reverse_targets=True)
        reverse=[*args[:5],request,artifact,body,args[8]]
        assert [item['target_id'] for item in json.loads(args[7])['targets']]==list(reversed(
            [item['target_id'] for item in json.loads(reverse[7])['targets']]))
        owners=[lock_decision(db,entry) for entry in (args,reverse)]
        return sorted(bundles,key=lambda bundle:str(bundle['target'])),args,reverse,owners


def wait_for_block(observer,writer,reader,future):
    deadline=monotonic()+5
    while monotonic()<deadline and not future.done():
        if observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',
                (writer.info.backend_pid,reader.info.backend_pid)).fetchone()[0]:
            return
        Event().wait(.01)
    raise AssertionError('expected lock dependency was not observed')


@pytest.mark.parametrize('index',[0,1])
@pytest.mark.parametrize('role',['target','preview','producer','result','working_image','pixels',
    'preview_presentation','preview_evidence','before','after','affected','protected'])
def test_multi_selection_holds_authority_rows_for_both_targets(database,index,role):
    bundles,args,_,owners=setup(database)
    bundle=bundles[index]
    rows=dict(target=('semantic_targets',bundle['target']),preview=('runs',bundle['run']),
        producer=('tasks',bundle['task']),result=('task_results',bundle['result']))
    rows.update({key:('artifacts',ref.artifact_id) for key,ref in bundle['refs'].items()})
    table,aid=rows[role]
    query='SELECT id FROM photo.'+table+' WHERE id=%s FOR UPDATE NOWAIT'
    with psycopg.connect(**database) as holder,psycopg.connect(**database) as contender:
        try:
            holder.execute('BEGIN')
            holder.execute("SET LOCAL statement_timeout='10s'")
            assert lock_decision(holder,args)==owners[0]
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with contender.transaction():contender.execute(query,(aid,)).fetchone()
            holder.rollback()
            with contender.transaction(force_rollback=True):
                assert contender.execute(query,(aid,)).fetchone()==(aid,)
        finally:
            holder.rollback();contender.rollback()


@pytest.mark.parametrize('reversed_request',[False,True])
@pytest.mark.parametrize('commit_revocation',[False,True])
def test_multi_selection_waits_then_rechecks_last_target_output(database,reversed_request,commit_revocation):
    bundles,args,reverse,owners=setup(database)
    chosen=reverse if reversed_request else args
    expected=owners[int(reversed_request)]
    last=bundles[-1]
    with psycopg.connect(**database) as writer,psycopg.connect(**database) as reader,\
            psycopg.connect(**database,autocommit=True) as observer:
        writer.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',
            (last['refs']['working_image'].artifact_id,))
        started=Event()
        def read():
            reader.execute("SET LOCAL statement_timeout='10s'")
            started.set()
            try:return lock_decision(reader,chosen),None
            except psycopg.Error as error:return None,error.sqlstate
            finally:reader.rollback()
        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(read)
            try:
                assert started.wait(2)
                wait_for_block(observer,writer,reader,future)
                # The lower-ID target is also locked while waiting for the last output.
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    with observer.transaction():
                        observer.execute('SELECT id FROM photo.semantic_targets WHERE id=%s FOR UPDATE NOWAIT',
                            (bundles[0]['target'],)).fetchone()
                if commit_revocation:writer.commit()
                else:writer.rollback()
                before=snapshot(observer,args[0])
                result,code=future.result(timeout=12)
                assert (result,code)==((None,'40001') if commit_revocation else (expected,None))
                assert snapshot(observer,args[0])==before
            finally:writer.rollback()


@pytest.mark.parametrize('holder_reversed',[False,True])
def test_opposite_decision_orders_serialize_without_partial_state(database,holder_reversed):
    _,args,reverse,owners=setup(database)
    requests=[args,reverse]
    with psycopg.connect(**database) as holder,psycopg.connect(**database) as reader,\
            psycopg.connect(**database,autocommit=True) as observer:
        holder.execute('BEGIN')
        holder.execute("SET LOCAL statement_timeout='10s'")
        assert lock_decision(holder,requests[int(holder_reversed)])==owners[int(holder_reversed)]
        before=snapshot(observer,args[0])
        started=Event()
        def read():
            reader.execute("SET LOCAL statement_timeout='10s'")
            started.set()
            try:return lock_decision(reader,requests[int(not holder_reversed)])
            finally:reader.rollback()
        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(read)
            try:
                assert started.wait(2)
                wait_for_block(observer,holder,reader,future)
                assert snapshot(observer,args[0])==before
                holder.rollback()
                assert future.result(timeout=12)==owners[int(not holder_reversed)]
                assert snapshot(observer,args[0])==before
            finally:holder.rollback()
