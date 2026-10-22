"""Advisory close authority agrees with incrementable request boundary."""
import pytest
from semantic_close_fixture import close_fixture
from removal_transaction_state import tenant_transaction_state


@pytest.mark.parametrize('revision,allowed',[(9007199254740990,True),(9007199254740991,False)])
def test_close_status_disables_nonincrementable_revision(db,revision,allowed):
    f=close_fixture(db,parent_revision=revision)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row=db.execute('SELECT * FROM photo.semantic_review_status(%s,%s,%s,%s)',f['args'][:4]).fetchone()
        db.execute('RESET ROLE')
    assert row[2]==str(revision) and row[3]=='waiting_review' and row[5]==0 and row[6] is allowed


@pytest.mark.migration_head(2414)
def test_close_status_upgrade_preserves_rows_and_function_privileges(db):
    from pathlib import Path
    f=close_fixture(db,parent_revision=9007199254740991)
    signature='photo.semantic_review_status(uuid,uuid,uuid,uuid)'
    identity=db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    before=tenant_transaction_state(db,f['args'][0])
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/02415_semantic_close_status_safe_revision.sql'
    db.execute(migration.read_text().split('-- +goose Down',1)[0])
    assert db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()==identity
    assert tenant_transaction_state(db,f['args'][0])==before
    assert db.execute('SELECT can_close FROM photo.semantic_review_status(%s,%s,%s,%s)',f['args'][:4]).fetchone()==(False,)
