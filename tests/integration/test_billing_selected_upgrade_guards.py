"""Real metadata guards on owned databases; no native authority or pixel claim."""
from pathlib import Path
import re
import pytest
import psycopg
from psycopg import sql

ROOT=Path(__file__).resolve().parents[2]
pytestmark=pytest.mark.migration_head(2609)
NAMES=('billing_prepare_operations','billing_bind_operations','seal_v2_graph',
       'billing_guard_operation_item','billing_settle_operations','billing_settle_next_operations')
ARGS={name:('' if name in ('billing_guard_operation_item','billing_settle_next_operations') else 'uuid,uuid') for name in NAMES}

def predecessor_definitions():
    original=(ROOT/'docs/billing/contracts/operations-workflow.sql').read_text()
    paid=(ROOT/'docs/billing/contracts/paid-operation-funding.sql').read_text()
    result={}
    for name in NAMES:
        source=paid if name=='billing_bind_operations' else original
        match=re.search(r'CREATE (?:OR REPLACE )?FUNCTION photo\.'+re.escape(name)+r'\([^)]*\)[\s\S]*?\nEND \$\$;',source)
        assert match is not None,name
        result[name]=match.group().replace('CREATE FUNCTION','CREATE OR REPLACE FUNCTION',1)
    return result

def restore_predecessors(db):
    definitions=predecessor_definitions()
    for name in NAMES:db.execute(definitions[name])
    return definitions

def guards():
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    prefix=source.split('ALTER TABLE photo.billing_operation_items ADD COLUMN success_contract',1)[0]
    assert prefix.count("RAISE EXCEPTION 'BILLING_SELECTED_PREDECESSOR_DRIFT:")==6
    assert 'CREATE FUNCTION' not in prefix and 'CREATE OR REPLACE FUNCTION' not in prefix
    return prefix

def test_selected_all_predecessor_guards_accept_original_metadata(db):
    restore_predecessors(db)
    db.execute(guards())

@pytest.mark.parametrize('name',NAMES)
@pytest.mark.parametrize('damage',['body','owner','raw_acl','config','cost','security'])
def test_selected_predecessor_metadata_drift_is_refused(db,name,damage):
    definitions=restore_predecessors(db)
    signature='photo.'+name+'('+ARGS[name]+')'
    if damage=='body':
        assert definitions[name].count('AS $$')==1
        db.execute(definitions[name].replace('AS $$','AS $$\n-- test-only predecessor drift\n',1))
    elif damage=='owner':
        admin=db.execute('SELECT session_user').fetchone()[0]
        db.execute(sql.SQL('ALTER FUNCTION '+signature+' OWNER TO {}').format(sql.Identifier(admin)))
    elif damage=='raw_acl':db.execute('GRANT EXECUTE ON FUNCTION '+signature+' TO photo_worker')
    elif damage=='config':db.execute('ALTER FUNCTION '+signature+' SET search_path=pg_catalog')
    elif damage=='cost':db.execute('ALTER FUNCTION '+signature+' COST 101')
    else:db.execute('ALTER FUNCTION '+signature+' SECURITY INVOKER')
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_SELECTED_PREDECESSOR_DRIFT: '+re.escape(name+'('+ARGS[name]+')')):
        with db.transaction():db.execute(guards())

def test_selected_predecessor_owner_login_is_refused(db):
    restore_predecessors(db)
    with db.transaction(force_rollback=True):
        db.execute('ALTER ROLE photo_fn_owner LOGIN')
        with pytest.raises(psycopg.errors.RaiseException,match='BILLING_SELECTED_PREDECESSOR_DRIFT: billing_prepare_operations'):
            with db.transaction():db.execute(guards())

def test_selected_predecessor_effective_acl_drift_is_refused(db):
    restore_predecessors(db)
    raw=db.execute("SELECT proacl::text FROM pg_catalog.pg_proc WHERE oid='photo.billing_prepare_operations(uuid,uuid)'::regprocedure").fetchone()
    with db.transaction(force_rollback=True):
        db.execute('GRANT photo_api TO photo_worker WITH INHERIT TRUE')
        assert db.execute("SELECT has_function_privilege('photo_worker','photo.billing_prepare_operations(uuid,uuid)','EXECUTE')").fetchone()==(True,)
        assert db.execute("SELECT proacl::text FROM pg_catalog.pg_proc WHERE oid='photo.billing_prepare_operations(uuid,uuid)'::regprocedure").fetchone()==raw
        with pytest.raises(psycopg.errors.RaiseException,match='BILLING_SELECTED_CALLER_DRIFT: billing_prepare_operations'):
            with db.transaction():db.execute(guards())
