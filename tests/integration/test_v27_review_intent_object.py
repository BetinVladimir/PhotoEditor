"""Authorized immutable intent metadata; synthetic bytes, not objectstore proof."""
import hashlib
from uuid import uuid4
import pytest
import psycopg
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from background_cleanup_apply_documents_fixture import prepared_review_fixture
from removal_transaction_state import tenant_transaction_state


def read(db,args):
    return db.execute('SELECT * FROM photo.semantic_review_intent_object(%s,%s,%s,%s)',args[:4]).fetchall()


def test_review_kind_source_returns_exact_authorized_intent_descriptor(database):
    args,_,scope,_=prepared_review_fixture(database)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        rows=read(database,args)
        database.execute('RESET ROLE')
    assert len(rows)==1
    aid,sha,bucket,key,version,size,media=rows[0]
    assert sha==hashlib.sha256(scope['intent_body']).hexdigest()
    assert size==len(scope['intent_body']) and media=='application/json'
    assert bucket and key and version not in (None,'','null')


@pytest.mark.parametrize('mutation',['unverified','expired','wrong_media'])
def test_review_kind_source_does_not_project_unusable_object(database,mutation):
    args,_,_,_=prepared_review_fixture(database)
    aid=read(database,args)[0][0]
    if mutation=='unverified':database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(aid,))
    elif mutation=='expired':database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(aid,))
    else:
        healthy=read(database,args)
        tenant_before=tenant_transaction_state(database,args[0])
        with pytest.raises(psycopg.errors.CheckViolation) as refused:
            with database.transaction():
                database.execute("UPDATE photo.artifacts SET media_type='application/octet-stream' WHERE id=%s",(aid,))
        assert refused.value.sqlstate=='23514'
        assert refused.value.diag.message_primary=='immutable field artifacts.media_type'
        assert read(database,args)==healthy
        assert tenant_transaction_state(database,args[0])==tenant_before
        return
    assert read(database,args)==[]


def test_review_kind_source_rechecks_actor_authority(database):
    args,_,_,_=prepared_review_fixture(database)
    changed=(*args[:2],uuid4(),args[3])
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with database.transaction():read(database,changed)
