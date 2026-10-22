"""Read-only own persisted recovery, not network commit fault injection."""
from uuid import uuid4
import pytest
import psycopg
from removal_preview_activation_fixture import published_transport_fixture,commit_preview
from removal_apply_documents_fixture import save_preview
from removal_transaction_state import tenant_transaction_state
from test_v27_removal_preview_sql_roles import database, isolated_database_config


def inspect(db,args,run,actor=None,session=None):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row=db.execute('SELECT * FROM photo.inspect_semantic_removal_preview(%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2] if actor is None else actor,args[3] if session is None else session,args[5],args[6])).fetchone()
        db.execute('RESET ROLE')
        return row


def test_removal_saved_request_recovers_exact_committed_task_read_only(database):
    args,run,scope=published_transport_fixture(database)
    assert inspect(database,args,run)==('draft','',False,False)
    task=commit_preview(database,args,run,scope)
    before=tenant_transaction_state(database,args[0])
    # Simulate a caller that did not receive the commit response: persisted
    # inspection, not the original response, resolves the same saved identity.
    assert save_preview(database,args)==run
    assert inspect(database,args,run)==('queued','ready',True,True)
    assert commit_preview(database,args,run,scope)==task
    assert tenant_transaction_state(database,args[0])==before


@pytest.mark.parametrize('mutation',['actor','session','missing_run','revoked_session'])
def test_removal_unavailable_inspection_never_reports_rollback(database,mutation):
    args,run,scope=published_transport_fixture(database)
    commit_preview(database,args,run,scope)
    actor=session=None
    if mutation=='actor':actor=uuid4()
    elif mutation=='session':session=uuid4()
    elif mutation=='missing_run':run=uuid4()
    else:database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.Error):inspect(database,args,run,actor,session)
    assert tenant_transaction_state(database,args[0])==before


def test_cancel_requested_sealed_removal_recovers_identity_without_restart(database):
    args,run,scope=published_transport_fixture(database)
    commit_preview(database,args,run,scope)
    database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    before=tenant_transaction_state(database,args[0])
    assert save_preview(database,args)==run
    assert inspect(database,args,run)==('cancelling','ready',True,True)
    assert tenant_transaction_state(database,args[0])==before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0]==1


def test_cancel_requested_unsealed_removal_draft_cannot_resume(database):
    args,run,scope=published_transport_fixture(database)
    database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.Error):save_preview(database,args)
    assert inspect(database,args,run)==('cancelling','',False,False)
    assert tenant_transaction_state(database,args[0])==before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0]==0
