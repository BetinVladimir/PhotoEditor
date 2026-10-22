"""Work-only2591→2592 external identity/drift matrix; no execution yet."""
from pathlib import Path
import psycopg
import pytest
from conftest import database

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02592_v23_apply_detail_producer_domain.sql').read_text().split('-- +goose Down')[0]
PUBLIC='photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)'
PRIVATE='photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)'


def snapshot(db, signature):
    return db.execute('SELECT to_jsonb(p)-\'prosrc\',pg_get_functiondef(p.oid),p.prosrc FROM pg_proc p WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()


@pytest.mark.migration_head(2591)
@pytest.mark.parametrize('drift',[None,'first_source','last_source','last_acl','last_owner','last_config','last_volatility'])
def test_detail_producer_upgrade_identity_and_atomic_refusal(database,drift):
    with psycopg.connect(**database) as db:
        if drift in ('first_source','last_source'):
            signature=PUBLIC if drift=='first_source' else PRIVATE
            before=snapshot(db,signature)
            # Nonneedle body drift, including last ordinal after first replacement.
            assert before[2].count('DECLARE e photo.correction_events%ROWTYPE;')==1
            revised=before[2].replace('DECLARE e photo.correction_events%ROWTYPE;',
                'DECLARE e photo.correction_events%ROWTYPE; -- external source drift')
            db.execute(before[1].replace(before[2],revised))
        elif drift=='last_acl':db.execute('GRANT EXECUTE ON FUNCTION '+PRIVATE+' TO PUBLIC')
        elif drift=='last_owner':db.execute('ALTER FUNCTION '+PRIVATE+' OWNER TO photo_scheduler')
        elif drift=='last_config':db.execute('ALTER FUNCTION '+PRIVATE+' SET search_path=pg_catalog')
        elif drift=='last_volatility':db.execute('ALTER FUNCTION '+PRIVATE+' STABLE')
        db.commit()
        before={signature:snapshot(db,signature) for signature in (PUBLIC,PRIVATE)}
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='V23 detail producer'):
                with db.transaction():db.execute(PATCH)
            assert {s:snapshot(db,s) for s in before}==before
        else:
            db.execute(PATCH)
            for signature in before:
                after=snapshot(db,signature)
                assert after[0]==before[signature][0]
                assert after[2]!=before[signature][2]
                assert 'JOIN photo.v2_workflows w' not in after[2]
                assert "r.kind='apply'" in after[2]
                assert after[2].count('uploaded.media_type=')==3
                assert after[2].count('receipt_attempt.worker_id=uploaded.worker_id')==3
                for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway','photo_portal','photo_maintenance'):
                    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(signature==PUBLIC and role=='photo_scheduler',)


@pytest.mark.migration_head(2592)
@pytest.mark.parametrize('drift',[None,'trigger_disabled','guard_body','other_column_acl','public_source','private_acl'])
def test_detail_event_row_lock_privilege_upgrade(database,drift):
    from test_v23_record_correction import _fixture, _record
    from uuid import UUID
    lock_patch=(ROOT/'backend/db/migrations/02593_v23_detail_event_row_lock_privilege.sql').read_text().split('-- +goose Down')[0]
    with psycopg.connect(**database) as db:
        tenant,actor,session,project,request,consent=_fixture(db)
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        event=UUID(_record(db,tenant,actor,session,request)['id'])
        db.execute('RESET ROLE')
        db.commit()
        if drift=='trigger_disabled':db.execute('ALTER TABLE photo.correction_events DISABLE TRIGGER correction_events_immutable')
        elif drift=='guard_body':
            definition=db.execute("SELECT pg_get_functiondef('photo.guard_v2_immutable()'::regprocedure)").fetchone()[0]
            db.execute(definition.replace('V2_IMMUTABLE','EXTERNAL_GUARD_DRIFT'))
        elif drift=='other_column_acl':db.execute('GRANT UPDATE(reason) ON photo.correction_events TO photo_fn_owner')
        elif drift=='public_source':
            before=snapshot(db,PUBLIC)
            db.execute(before[1].replace(before[2],before[2]+'\n-- external nonneedle drift\n'))
        elif drift=='private_acl':db.execute('GRANT EXECUTE ON FUNCTION '+PRIVATE+' TO photo_api')
        db.commit()
        def state():
            return dict(functions={sig:snapshot(db,sig) for sig in (PUBLIC,PRIVATE)},
                rows=db.execute('SELECT to_jsonb(x) FROM photo.correction_events x ORDER BY id').fetchall(),
                table=db.execute("SELECT to_jsonb(c) FROM pg_class c WHERE oid='photo.correction_events'::regclass").fetchone(),
                columns=db.execute("SELECT attnum,attacl::text FROM pg_attribute WHERE attrelid='photo.correction_events'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum").fetchall(),
                triggers=db.execute("SELECT to_jsonb(t) FROM pg_trigger t WHERE tgrelid='photo.correction_events'::regclass ORDER BY oid").fetchall())
        before=state()
        assert db.execute("SELECT has_table_privilege('photo_fn_owner','photo.correction_events','UPDATE')").fetchone()==(False,)
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='V23_DETAIL_EVENT_ROW_LOCK'):
                with db.transaction():db.execute(lock_patch)
            assert state()==before
            return
        assert db.execute("SELECT has_column_privilege('photo_fn_owner','photo.correction_events','id','UPDATE')").fetchone()==(False,)
        with pytest.raises(psycopg.errors.InsufficientPrivilege),db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('SELECT id FROM photo.correction_events WHERE tenant_id=%s AND id=%s FOR SHARE',(tenant,event)).fetchone()
        db.execute(lock_patch)
        after=state()
        assert db.execute("SELECT rolcanlogin FROM pg_roles WHERE rolname='photo_fn_owner'").fetchone()==(False,)
        assert after['functions']==before['functions'] and after['rows']==before['rows']
        assert after['table']==before['table'] and after['triggers']==before['triggers']
        id_attribute=db.execute("SELECT attnum FROM pg_attribute WHERE attrelid='photo.correction_events'::regclass AND attname='id'").fetchone()[0]
        assert [row for row in after['columns'] if row[0]!=id_attribute]==[row for row in before['columns'] if row[0]!=id_attribute]
        assert dict(after['columns'])[id_attribute]!=dict(before['columns'])[id_attribute]
        attributes=db.execute("SELECT attname FROM pg_attribute WHERE attrelid='photo.correction_events'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum").fetchall()
        for role in ('photo_fn_owner','photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_maintenance'):
            assert db.execute("SELECT has_table_privilege(%s,'photo.correction_events','UPDATE')",(role,)).fetchone()==(False,)
            for (column,) in attributes:
                assert db.execute("SELECT has_column_privilege(%s,'photo.correction_events',%s,'UPDATE')",(role,column)).fetchone()==(role=='photo_fn_owner' and column=='id',)
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_fn_owner')
            assert db.execute('SELECT id FROM photo.correction_events WHERE tenant_id=%s AND id=%s FOR SHARE',(tenant,event)).fetchone()==(event,)
            with psycopg.connect(**database) as other:
                other.execute("SET LOCAL lock_timeout='150ms'")
                other.execute('SET LOCAL ROLE photo_fn_owner')
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    other.execute('DELETE FROM photo.correction_events WHERE tenant_id=%s AND id=%s',(tenant,event))
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState,match='V2_IMMUTABLE'),db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('UPDATE photo.correction_events SET id=id WHERE tenant_id=%s AND id=%s',(tenant,event))
        for role in ('photo_api','photo_worker','photo_scheduler'):
            with pytest.raises(psycopg.errors.InsufficientPrivilege),db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                db.execute('UPDATE photo.correction_events SET id=id WHERE tenant_id=%s AND id=%s',(tenant,event))
        assert state()==after


@pytest.mark.migration_head(2593)
@pytest.mark.parametrize('table,column,trigger,other_column',[
    ('v23_portrait_detail_event_links','event_id','v23_portrait_detail_event_links_immutable','actor_id'),
    ('apply_nodes','id','apply_nodes_identity_immutable','phase'),
])
@pytest.mark.parametrize('drift',[None,'trigger_shape','guard_body','other_column_acl','source','policy'])
def test_detail_link_node_row_lock_upgrade(database,tmp_path,table,column,trigger,other_column,drift):
    """Whole real detail construction after narrowly protected row-lock grant."""
    from detail_training_fixture import detail_training
    from detail_closure_compiler import ClosureCompiler
    patch=(ROOT/'backend/db/migrations/02594_v23_detail_link_node_row_lock_privilege.sql').read_text().split('-- +goose Down')[0]
    with psycopg.connect(**database) as db:
        def state():
            return dict(functions=db.execute("SELECT to_jsonb(p) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND (p.proname LIKE '%detail%' OR p.proname='guard_v2_immutable') ORDER BY p.oid").fetchall(),
                tables=db.execute("SELECT to_jsonb(c) FROM pg_class c WHERE c.oid IN('photo.apply_nodes'::regclass,'photo.v23_portrait_detail_event_links'::regclass) ORDER BY c.oid").fetchall(),
                columns=db.execute("SELECT attrelid,attnum,attacl::text FROM pg_attribute WHERE attrelid IN('photo.apply_nodes'::regclass,'photo.v23_portrait_detail_event_links'::regclass) AND attnum>0 AND NOT attisdropped ORDER BY attrelid,attnum").fetchall(),
                triggers=db.execute("SELECT to_jsonb(t) FROM pg_trigger t WHERE tgrelid IN('photo.apply_nodes'::regclass,'photo.v23_portrait_detail_event_links'::regclass) ORDER BY oid").fetchall(),
                policies=db.execute("SELECT to_jsonb(p) FROM pg_policy p WHERE polrelid IN('photo.apply_nodes'::regclass,'photo.v23_portrait_detail_event_links'::regclass) ORDER BY oid").fetchall(),
                rows={t:db.execute('SELECT to_jsonb(x) FROM photo.'+t+' x ORDER BY to_jsonb(x)::text').fetchall() for t in ('apply_nodes','v23_portrait_detail_event_links')})
        if drift=='trigger_shape':
            if table=='apply_nodes':
                db.execute('CREATE TRIGGER apply_nodes_identity_immutable AFTER UPDATE OF id ON photo.apply_nodes FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable()')
            else:db.execute('ALTER TABLE photo.'+table+' DISABLE TRIGGER '+trigger)
        elif drift=='guard_body':
            definition=db.execute("SELECT pg_get_functiondef('photo.guard_v2_immutable()'::regprocedure)").fetchone()[0]
            db.execute(definition.replace('V2_IMMUTABLE','EXTERNAL_IDENTITY_DRIFT'))
        elif drift=='other_column_acl':db.execute('GRANT UPDATE('+other_column+') ON photo.'+table+' TO photo_fn_owner')
        elif drift=='source':
            definition=db.execute("SELECT pg_get_functiondef('photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'::regprocedure)").fetchone()[0]
            db.execute(definition.replace('DECLARE item record;','DECLARE item record; -- external row-lock consumer drift\n'))
        elif drift=='policy':db.execute('ALTER POLICY trusted_functions ON photo.'+table+' USING(false)')
        db.commit();before=state()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='V23_DETAIL_(LINK|NODE)_ROW_LOCK'):
                with db.transaction():db.execute(patch)
            assert state()==before  # Includes rollback of first-table grant on last-table drift.
            return
        with pytest.raises(psycopg.errors.InsufficientPrivilege),db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('SELECT '+column+' FROM photo.'+table+' FOR SHARE').fetchall()
        db.execute(patch);after=state()
        assert after['functions']==before['functions'] and after['policies']==before['policies']
        assert after['tables']==before['tables'] and after['rows']==before['rows']
        old_triggers={row[0]['tgname']:row[0] for row in before['triggers']}
        new_triggers={row[0]['tgname']:row[0] for row in after['triggers']}
        assert all(new_triggers[name]==row for name,row in old_triggers.items())
        assert set(new_triggers)-set(old_triggers)=={'apply_nodes_identity_immutable'}
        assert db.execute("SELECT rolcanlogin FROM pg_roles WHERE rolname='photo_fn_owner'").fetchone()==(False,)
        for target,allowed in [('v23_portrait_detail_event_links','event_id'),('apply_nodes','id')]:
            attributes=db.execute('SELECT attname FROM pg_attribute WHERE attrelid=%s::regclass AND attnum>0 AND NOT attisdropped',('photo.'+target,)).fetchall()
            for role in ('photo_fn_owner','photo_api','photo_scheduler','photo_worker','photo_gateway','photo_portal','photo_maintenance'):
                assert db.execute('SELECT has_table_privilege(%s,%s,\'UPDATE\')',(role,'photo.'+target)).fetchone()==(False,)
                for (attribute,) in attributes:
                    assert db.execute('SELECT has_column_privilege(%s,%s,%s,\'UPDATE\')',(role,'photo.'+target,attribute)).fetchone()==(role=='photo_fn_owner' and attribute==allowed,)
        db.execute(patch);assert state()==after  # Exact reapply is idempotent.
        compiler=ClosureCompiler(tmp_path/'compiler')
        ids=detail_training(db,qualification={},compiler=compiler)
        db.execute('RESET ROLE')
        tenant=ids[0]
        row=db.execute('SELECT '+column+' FROM photo.'+table+' WHERE tenant_id=%s ORDER BY '+column+' LIMIT 1',(tenant,)).fetchone()
        assert row is not None
        db.commit();identity=row[0]
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_fn_owner')
            assert db.execute('SELECT '+column+' FROM photo.'+table+' WHERE tenant_id=%s AND '+column+'=%s FOR SHARE',(tenant,identity)).fetchone()==(identity,)
            with psycopg.connect(**database) as other:
                other.execute("SET LOCAL lock_timeout='150ms'")
                other.execute("SET LOCAL statement_timeout='5000ms'")
                other.execute('SET LOCAL ROLE photo_fn_owner')
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    other.execute('DELETE FROM photo.'+table+' WHERE tenant_id=%s AND '+column+'=%s',(tenant,identity))
        preserved=state()
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState,match='V2_IMMUTABLE'),db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('UPDATE photo.'+table+' SET '+column+'='+column+' WHERE tenant_id=%s AND '+column+'=%s',(tenant,identity))
        for role in ('photo_api','photo_worker','photo_scheduler'):
            with pytest.raises(psycopg.errors.InsufficientPrivilege),db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                db.execute('UPDATE photo.'+table+' SET '+column+'='+column+' WHERE tenant_id=%s AND '+column+'=%s',(tenant,identity))
        assert state()==preserved
