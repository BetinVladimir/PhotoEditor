"""Structural upgrade and refusal tests; actual photographic GET is separate."""
from pathlib import Path
import psycopg
import pytest
from conftest import database

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02590_semantic_shown_preview_physical_receipt.sql').read_text().split('-- +goose Down')[0]
SIGNATURE='photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)'
CHILD='photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid)'
NEEDLE="IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(preview.id,result.id)) OR NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND id=preview.run_id AND status='completed' AND graph_sealed AND NOT cancel_requested) THEN RETURN;END IF;"

@pytest.mark.migration_head(2589)
@pytest.mark.parametrize('drift',[None,'source','nonneedle_source','authority','child_source','child_nonneedle_source','child_authority'])
def test_upgrade_exact_identity_and_drift_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot(signature):
            return db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,prorettype,proargtypes,proargnames,proargmodes,proallargtypes,prokind,proretset,prolang,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
        if drift=='source':
            row=snapshot(SIGNATURE);assert row[-1].count(NEEDLE)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(NEEDLE,NEEDLE.replace('IF NOT FOUND','IF  NOT FOUND'))))
        elif drift=='nonneedle_source':
            row=snapshot(SIGNATURE)
            guard="AND state='succeeded' AND operation_version='1' AND NOT cancel_requested"
            assert row[-1].count(guard)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(guard,"AND state='succeeded' AND operation_version='1'")))
        elif drift=='authority':
            db.execute('GRANT EXECUTE ON FUNCTION '+SIGNATURE+' TO PUBLIC')
        elif drift=='child_source':
            row=snapshot(CHILD)
            needle='photo.v2_adoption_event_id(t.id,z.id)'
            assert row[-1].count(needle)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(needle,'photo.v2_adoption_event_id(t.id, z.id)')))
        elif drift=='child_nonneedle_source':
            row=snapshot(CHILD);guard="AND photo.v2_editor_authorized(p_tenant,target.project_id,p_actor,p_session)"
            assert row[-1].count(guard)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(guard,'AND true')))
        elif drift=='child_authority':
            db.execute('GRANT EXECUTE ON FUNCTION '+CHILD+' TO PUBLIC')
        db.commit()
        before=snapshot(SIGNATURE);child=snapshot(CHILD)
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='SHOWN_PREVIEW_'):
                with db.transaction():db.execute(PATCH)
            assert snapshot(SIGNATURE)==before
            assert snapshot(CHILD)==child
        else:
            db.execute(PATCH)
            after=snapshot(SIGNATURE)
            assert after[:-2]==before[:-2]
            assert after[-1]!=before[-1] and NEEDLE not in after[-1]
            for column in ('v27_preapply_preview_receipt','v27_removal_preview_receipt','v27_background_cleanup_preview_receipt','v27_retouch_preview_receipt'):
                assert after[-1].count(column)==1
            assert snapshot(CHILD)==child
            for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_semantic_admission'):
                assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,SIGNATURE)).fetchone()==(role=='photo_api',)

# Physical negatives must run on the real full shown graph, not this empty
# upgrade database. Do not seed artificial acceptance/inbox rows here.
