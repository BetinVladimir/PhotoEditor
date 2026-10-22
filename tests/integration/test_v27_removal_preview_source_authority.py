"""Own SQL presentation authority; metadata fixture is not valid display evidence."""
from uuid import uuid4
import pytest
import psycopg
from removal_preview_receipt_fixture import presentation_source_fixture
from removal_transaction_state import tenant_transaction_state
from test_v27_removal_preview_sql_roles import database, isolated_database_config


def source(db,args,run):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row=db.execute('SELECT * FROM photo.semantic_removal_preview_presentation_source(%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2],args[3],args[5],args[6])).fetchone()
        db.execute('RESET ROLE')
        return row


@pytest.mark.parametrize('mutation',['cancel_run','cancel_task','unverified','expired','missing_canonical','wrong_role'])
def test_removal_source_denies_stale_producer_or_artifact_without_mutation(database,mutation):
    args,run,scope,task,result,root,digest=presentation_source_fixture(database)
    row=source(database,args,run)
    assert row[0]==task and row[1]==result and row[4]==root and row[5]==digest
    if mutation=='cancel_run':database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    elif mutation=='cancel_task':database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
    elif mutation=='unverified':database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(root,))
    elif mutation=='expired':database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(root,))
    elif mutation=='missing_canonical':
        before=tenant_transaction_state(database,args[0])
        with pytest.raises(psycopg.errors.CheckViolation) as failure:
            with database.transaction():
                database.execute('UPDATE photo.tasks SET canonical_result_id=NULL WHERE id=%s',(task,))
        assert failure.value.diag.constraint_name=='tasks_domain_4'
        assert tenant_transaction_state(database,args[0])==before
        assert source(database,args,run)==row
        return
    else:database.execute("UPDATE photo.artifacts SET output_role='preview_evidence' WHERE id=%s",(root,))
    before=tenant_transaction_state(database,args[0])
    assert source(database,args,run) is None
    assert tenant_transaction_state(database,args[0])==before


@pytest.mark.parametrize('mutation',['tenant','actor','session','missing_run','revoked_session'])
def test_removal_source_refuses_foreign_or_inaccessible_editor_scope(database,mutation):
    args,run,scope,*_=presentation_source_fixture(database)
    changed=list(args)
    if mutation=='tenant':changed[0]=uuid4()
    elif mutation=='actor':changed[2]=uuid4()
    elif mutation=='session':changed[3]=uuid4()
    elif mutation=='missing_run':run=uuid4()
    else:database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.Error):source(database,changed,run)
    assert tenant_transaction_state(database,args[0])==before


def sidecars(db,args,run,raw):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        rows=db.execute('SELECT * FROM photo.semantic_removal_preview_presentation_sidecars(%s,%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2],args[3],args[5],args[6],raw)).fetchall()
        db.execute('RESET ROLE')
        return rows


@pytest.mark.parametrize('role',['working_image','pixels','preview_evidence','before','after','affected','protected'])
@pytest.mark.parametrize('mutation',['unverified','expired','version'])
def test_removal_sidecar_failure_never_returns_partial_authorized_bundle(database,role,mutation):
    from removal_preview_receipt_fixture import authorized_sidecars_fixture
    args,run,scope,refs,raw=authorized_sidecars_fixture(database)
    rows=sidecars(database,args,run,raw)
    assert {row[0] for row in rows}==set(refs)
    assert len(rows)==7
    for row in rows:
        assert str(row[1])==refs[row[0]]['artifact_id'] and row[2]==refs[row[0]]['sha256']
    aid=refs[role]['artifact_id']
    if mutation=='unverified':database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(aid,))
    elif mutation=='expired':database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(aid,))
    else:
        before=tenant_transaction_state(database,args[0])
        with pytest.raises(psycopg.errors.CheckViolation) as exact_failure:
            with database.transaction():
                database.execute("UPDATE photo.artifacts SET object_version='' WHERE id=%s",(aid,))
        assert exact_failure.value.diag.message_primary=='immutable field artifacts.object_version'
        assert tenant_transaction_state(database,args[0])==before
        assert sidecars(database,args,run,raw)==rows
        return
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.Error) as error:
        sidecars(database,args,run,raw)
    assert error.value.sqlstate=='42501'
    assert tenant_transaction_state(database,args[0])==before


def test_removal_source_denies_noncompleted_task_without_canonical_result(database):
    args,run,scope,task,result,root,digest=presentation_source_fixture(database,_complete=False)
    state=database.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    assert state[0]!='succeeded' and state[1] is None
    before=tenant_transaction_state(database,args[0])
    assert source(database,args,run) is None
    assert tenant_transaction_state(database,args[0])==before


@pytest.mark.parametrize('role',['working_image','pixels','preview_evidence','before','after','affected','protected'])
@pytest.mark.parametrize('version',['','null'])
def test_removal_sidecars_deny_initial_missing_version_without_partial_bundle(database,role,version):
    from removal_preview_receipt_fixture import authorized_sidecars_fixture
    args,run,scope,refs,raw=authorized_sidecars_fixture(database,_initial_versions={role:version})
    assert database.execute('SELECT object_version FROM photo.artifacts WHERE id=%s',(refs[role]['artifact_id'],)).fetchone()==(version,)
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        sidecars(database,args,run,raw)
    assert tenant_transaction_state(database,args[0])==before
