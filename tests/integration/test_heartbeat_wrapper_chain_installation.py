"""Actual catalog mutations must not bypass the installed heartbeat entrypoint."""
import pytest
from contextlib import contextmanager
from hashlib import sha256
from psycopg import sql
from photo_worker.runtime.database import TaskDatabase

NAMES=(
 'heartbeat_task','heartbeat_task_before_shown_retouch',
 'heartbeat_task_before_retouch_preview','heartbeat_task_before_shown_background_cleanup',
 'heartbeat_task_before_background_cleanup_preview','heartbeat_task_before_shown_removal',
 'heartbeat_task_before_removal_preview','heartbeat_task_pre_shown_media',
 'heartbeat_task_pre_v23_series_device',
)
ABI='(uuid,integer,bigint,uuid,jsonb)'

def probe(db,monkeypatch):
    value=TaskDatabase('unused')
    monkeypatch.setattr(value,'_query',lambda query,args:db.execute(query,args).fetchone())
    return value

def test_complete_heartbeat_wrapper_chain_is_installed(db,monkeypatch):
    assert probe(db,monkeypatch).task_result_fence_ready() is True

@pytest.mark.parametrize('name',NAMES)
@pytest.mark.parametrize('damage',('body','owner','acl','config','security','alias'))
def test_heartbeat_chain_drift_refused(db,monkeypatch,name,damage):
    value=probe(db,monkeypatch)
    assert value.task_result_fence_ready() is True
    signature='photo.'+name+ABI
    with db.transaction(force_rollback=True):
        if damage=='body':
            definition,source=db.execute('SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
            if name==NAMES[-1]:
                assert '-- 02537:' in source
                changed=source.replace('-- 02537:','-- fence removed:',1)
            else:
                changed='BEGIN RETURN QUERY SELECT false,NULL::timestamptz,false;END'
            db.execute(definition.replace(source,changed,1))
        elif damage=='owner':
            db.execute(sql.SQL('ALTER FUNCTION {} OWNER TO photo_api').format(sql.SQL(signature)))
        elif damage=='acl':
            db.execute(sql.SQL('GRANT EXECUTE ON FUNCTION {} TO photo_api').format(sql.SQL(signature)))
        elif damage=='config':
            db.execute(sql.SQL('ALTER FUNCTION {} SET search_path=public,photo').format(sql.SQL(signature)))
        elif damage=='security':
            db.execute(sql.SQL('ALTER FUNCTION {} SECURITY INVOKER').format(sql.SQL(signature)))
        else:
            db.execute(sql.SQL('ALTER FUNCTION {} RENAME TO heartbeat_unrelated_copy').format(sql.SQL(signature)))
        assert value.task_result_fence_ready() is False


@contextmanager
def native_variant(db):
    from pathlib import Path
    fixture=Path(__file__).with_name('fixtures')/'heartbeat-native-helpers.sql'
    with db.transaction(force_rollback=True):
        db.execute(fixture.read_text())
        definition,source=db.execute("SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)",('photo.heartbeat_task'+ABI,)).fetchone()
        if 'authored_native_replay_parent_lock' not in source:
            assert source.count('BEGIN\n')==1
            prefix=" PERFORM photo.authored_mask_lock_execution_parent(p_task);\nIF photo.authored_native_replay_parent_lock(p_task) IS DISTINCT FROM true THEN RAISE EXCEPTION 'AUTHORED_NATIVE_REPLAY_PARENT_RETRY' USING ERRCODE='40001';END IF;\n\n"
            changed=source.replace('BEGIN\n','BEGIN\n'+prefix,1)
            assert sha256(changed.encode()).hexdigest()=='ad68c2cc04acd441dafe096f07dd9404c7b1ed10e972a66c64c1b020d950597f'
            db.execute(definition.replace(source,changed,1))
        yield


def test_exact_native_heartbeat_variant_is_installed(db,monkeypatch):
    value=probe(db,monkeypatch)
    with native_variant(db):
        assert value.task_result_fence_ready() is True


@pytest.mark.parametrize('name',('authored_mask_lock_execution_parent','authored_native_replay_parent_lock'))
@pytest.mark.parametrize('damage',('body','owner','acl','config','security','alias'))
def test_native_heartbeat_helper_drift_refused(db,monkeypatch,name,damage):
    value=probe(db,monkeypatch)
    with native_variant(db):
        assert value.task_result_fence_ready() is True
        signature='photo.'+name+'(uuid)'
        if damage=='body':
            definition,source=db.execute('SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
            db.execute(definition.replace(source,source+'\n-- changed body\n',1))
        elif damage=='owner':db.execute(sql.SQL('ALTER FUNCTION {} OWNER TO photo_api').format(sql.SQL(signature)))
        elif damage=='acl':db.execute(sql.SQL('GRANT EXECUTE ON FUNCTION {} TO photo_worker').format(sql.SQL(signature)))
        elif damage=='config':db.execute(sql.SQL('ALTER FUNCTION {} SET search_path=public,photo').format(sql.SQL(signature)))
        elif damage=='security':db.execute(sql.SQL('ALTER FUNCTION {} SECURITY INVOKER').format(sql.SQL(signature)))
        else:db.execute(sql.SQL('ALTER FUNCTION {} RENAME TO unrelated_native_helper').format(sql.SQL(signature)))
        assert value.task_result_fence_ready() is False
