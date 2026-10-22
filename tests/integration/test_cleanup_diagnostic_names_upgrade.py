"""02597: error-label-only upgrade and isolated SQL wrapper regressions.

Authored/unexecuted. Requires reviewed immutable metadata fixture bundle landed.
Transactional dependency doubles below are not physical/lease authority evidence.
"""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from conftest import database, db

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02597_cleanup_diagnostic_names.sql').read_text().split('-- +goose Down')[0]
SIGS=('photo.commit_task_success_before_background_cleanup_bundle(uuid,integer,bigint,uuid,text)',
      'photo.commit_task_success_before_retouch_bundle(uuid,integer,bigint,uuid,text)')
OLD=('SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE','SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE')
NEW=('SEMANTIC_BACKGROUND_CLEANUP_PREVIEW_RESULT_STALE','SEMANTIC_BACKGROUND_CLEANUP_PREVIEW_BUNDLE_INCOMPLETE')


def snapshot(connection):
    return [connection.execute('SELECT to_jsonb(p),pg_get_functiondef(p.oid) FROM pg_proc p WHERE oid=to_regprocedure(%s)',(s,)).fetchone() for s in SIGS]


def scoped_rows(connection,tenant):
    return {table:connection.execute('SELECT to_jsonb(t) FROM photo.'+table+' t WHERE '+('id' if table=='tenants' else 'tenant_id')+'=%s ORDER BY to_jsonb(t)::text',(tenant,)).fetchall()
            for table in ('tenants','memberships','tenant_usage','artifacts','snapshots','runs','tasks','task_results','output_uploads','quota_reservations','outbox','resource_reservations')}


@pytest.mark.migration_head(2596)
@pytest.mark.parametrize('drift',[None,'first_source','second_source','first_acl','second_acl','path','owner','security','volatility'])
def test_upgrade_two_function_atomicity_and_complete_metadata_invariance(database,drift):
    from test_v27_background_cleanup_preview_bundle import bundle
    with psycopg.connect(**database) as connection:
        tenant,task,result,_=bundle(connection)
        if drift in ('first_source','second_source'):
            index=int(drift=='second_source');row,definition=snapshot(connection)[index]
            connection.execute(definition.replace(row['prosrc'],row['prosrc']+'\n-- deliberate source drift\n'))
        elif drift in ('first_acl','second_acl'):
            connection.execute('GRANT EXECUTE ON FUNCTION '+SIGS[int(drift=='second_acl')]+' TO PUBLIC')
        elif drift=='path':connection.execute('ALTER FUNCTION '+SIGS[1]+' SET search_path=pg_catalog')
        elif drift=='owner':connection.execute('ALTER FUNCTION '+SIGS[1]+' OWNER TO photo_scheduler')
        elif drift=='security':connection.execute('ALTER FUNCTION '+SIGS[1]+' SECURITY INVOKER')
        elif drift=='volatility':connection.execute('ALTER FUNCTION '+SIGS[1]+' STABLE')
        connection.commit()
        before=snapshot(connection);rows=scoped_rows(connection,tenant)
        assert rows['tasks'] and rows['artifacts'] and rows['task_results']
        roles=connection.execute('SELECT oid,rolname,rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls FROM pg_roles ORDER BY oid').fetchall()
        memberships=connection.execute('SELECT to_jsonb(m) FROM pg_auth_members m ORDER BY to_jsonb(m)::text').fetchall()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException) as raised:
                with connection.transaction():connection.execute(PATCH)
            assert raised.value.sqlstate=='P0001'
            assert raised.value.diag.message_primary==('CLEANUP_DIAGNOSTIC_ACL_DRIFT' if drift in ('first_acl','second_acl') else 'CLEANUP_DIAGNOSTIC_SOURCE_DRIFT')
            # A second-function refusal must roll back the already-patched first.
            assert snapshot(connection)==before
        else:
            connection.execute(PATCH)
            after=snapshot(connection)
            for (a,_),(b,_) in zip(after,before):
                assert {k:v for k,v in a.items() if k!='prosrc'}=={k:v for k,v in b.items() if k!='prosrc'}
                assert a['prosrc']==b['prosrc'].replace(OLD[0],NEW[0]).replace(OLD[1],NEW[1])
        assert scoped_rows(connection,tenant)==rows
        assert connection.execute('SELECT oid,rolname,rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls FROM pg_roles ORDER BY oid').fetchall()==roles
        assert connection.execute('SELECT to_jsonb(m) FROM pg_auth_members m ORDER BY to_jsonb(m)::text').fetchall()==memberships


def test_fresh_head_exact_names_and_private_predecessor_acl(db):
    rows=snapshot(db)
    for index,(row,_) in enumerate(rows):
        assert OLD[0] not in row['prosrc'] and OLD[1] not in row['prosrc']
        assert row['prosrc'].count(NEW[0])==1
        assert row['prosrc'].count(NEW[1])==index
        for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_semantic_admission'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,SIGS[index])).fetchone()==(False,)


@pytest.mark.parametrize('wrapper,refusal',[(0,'current'),(1,'current'),(1,'bundle')])
def test_exact_cleanup_error_and_rolled_back_effects(db,wrapper,refusal):
    from test_v27_background_cleanup_preview_bundle import isolated_commit, replace_body
    actual_first=snapshot(db)[0][1]
    tenant,task,result,refs=isolated_commit(db)
    # Isolate the predecessor, preserving the exact patched wrapper under test.
    # These dependency doubles are transactional unit evidence, not authority.
    if wrapper==0:
        predecessor_stub=snapshot(db)[0][0]['prosrc']
        db.execute(actual_first)
        replace_body(db,'photo.commit_task_success_before_background_cleanup_preview(uuid,integer,bigint,uuid,text)',predecessor_stub)
    if refusal=='current':
        replace_body(db,'photo.semantic_background_cleanup_preview_run_current(uuid,uuid)','BEGIN RETURN false; END;')
        expected=NEW[0]
    else:
        db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(refs['after'],))
        expected=NEW[1]
    before=scoped_rows(db,tenant)
    with pytest.raises(psycopg.errors.CheckViolation) as raised:
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('SELECT * FROM '+SIGS[wrapper].split('(')[0]+'(%s,1,1,%s,%s)',(task,uuid4(),'{}')).fetchone()
    assert raised.value.sqlstate=='23514'
    assert raised.value.diag.message_primary==expected
    assert scoped_rows(db,tenant)==before


@pytest.mark.parametrize('wrapper',[0,1])
def test_valid_current_control_commits_and_replays_once(db,wrapper):
    from test_v27_background_cleanup_preview_bundle import isolated_commit, replace_body
    actual_first=snapshot(db)[0][1]
    tenant,task,result,_=isolated_commit(db)
    if wrapper==0:
        predecessor_stub=snapshot(db)[0][0]['prosrc']
        db.execute(actual_first)
        replace_body(db,'photo.commit_task_success_before_background_cleanup_preview(uuid,integer,bigint,uuid,text)',predecessor_stub)
    db.execute('SET LOCAL ROLE photo_fn_owner')
    first=db.execute('SELECT * FROM '+SIGS[wrapper].split('(')[0]+'(%s,1,1,%s,%s)',(task,uuid4(),'{}')).fetchone()
    assert first[:2]==('committed',result)
    db.execute('RESET ROLE')
    before=scoped_rows(db,tenant)
    db.execute('SET LOCAL ROLE photo_fn_owner')
    replay=db.execute('SELECT * FROM '+SIGS[wrapper].split('(')[0]+'(%s,1,1,%s,%s)',(task,uuid4(),'{}')).fetchone()
    assert replay[:2]==('replayed',result)
    db.execute('RESET ROLE')
    assert scoped_rows(db,tenant)==before
