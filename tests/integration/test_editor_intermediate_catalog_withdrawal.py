"""Exact immutable intermediate catalog-loss regression; authored, not executed.

This owner-seeded relational fixture exercises the private catalog witness, not
full admitted Editor origin/physical quality. The existing withdrawal lifecycle
is retained rather than replaced with a permissive test predicate.
"""
from uuid import uuid4
from psycopg import sql
import pytest
from test_apply_v4_plan import _build


def sealed_catalog(db):
    tid,run,_,items=_build(db,count=1)
    _,_,version,source,digest=items[0]
    project=db.execute('SELECT project_id FROM photo.collections WHERE id=(SELECT source_collection_id FROM photo.apply_builds WHERE run_id=%s)',(run,)).fetchone()[0]
    actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    refs=[]
    for ordinal in range(5):
        sha=str(ordinal+1)*64
        aid=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'fixture',%s,'v1',%s,2,'application/json','ready',clock_timestamp()) RETURNING id",(tid,str(uuid4()),sha)).fetchone()[0]
        refs.append((aid,sha))
    db.execute('UPDATE photo.apply_builds SET source_collection_id=NULL WHERE run_id=%s',(run,))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("INSERT INTO photo.editor_effect_builds(tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,state,seal_adoption_digest,seal_adoption_sql_digest,seal_receipt_artifact_id,seal_receipt_sha,result_artifact_id,result_sha,processed_source_artifact_id,processed_source_sha,executed_style_artifact_id,executed_style_sha,quality_artifact_id,quality_sha) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,1,%s,'sealed',%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)",
            (tid,run,project,actor,uuid4(),version,source,digest,'a'*64,source,digest,uuid4(),uuid4(),'b'*64,'c'*64,'d'*64,*refs[0],*refs[1],*refs[2],*refs[3],*refs[4]))
    return tid,run,refs


def lost(db,tid,run):
    return db.execute('SELECT photo.editor_effect_intermediate_catalog_lost(%s,%s)',(tid,run)).fetchone()[0]


@pytest.mark.parametrize('ordinal',range(5))
@pytest.mark.parametrize('case',['expired','cleaning','unverified','wrong_sha','wrong_version'])
def test_sealed_exact_intermediate_loss_and_recovery(db,ordinal,case):
    tid,run,refs=sealed_catalog(db)
    assert lost(db,tid,run) is False
    queries={
        'expired':"UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",
        'cleaning':"UPDATE photo.artifacts SET state='cleaning' WHERE id=%s",
        'unverified':"UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s",
    }
    with db.transaction(force_rollback=True):
        if case in queries:
            db.execute(queries[case],(refs[ordinal][0],))
        else:
            prefix=['seal_receipt','result','processed_source','executed_style','quality'][ordinal]
            if case=='wrong_sha':
                db.execute(sql.SQL('UPDATE photo.editor_effect_builds SET {}=%s WHERE run_id=%s').format(sql.Identifier(prefix+'_sha')),('f'*64,run))
            else:
                invalid=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'fixture',%s,'null',%s,2,'application/json','ready',clock_timestamp()) RETURNING id",(tid,str(uuid4()),refs[ordinal][1])).fetchone()[0]
                db.execute(sql.SQL('UPDATE photo.editor_effect_builds SET {}=%s WHERE run_id=%s').format(sql.Identifier(prefix+'_artifact_id')),(invalid,run))
        assert lost(db,tid,run) is True
        assert lost(db,uuid4(),run) is False
    assert lost(db,tid,run) is False


def test_no_loss_in_unadopted_build_and_no_qualification_readiness_in_witness(db):
    tid,run,refs=sealed_catalog(db)
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.editor_effect_builds SET state='sealing' WHERE run_id=%s",(run,))
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(refs[0][0],))
        assert lost(db,tid,run) is False
    definition=db.execute("SELECT pg_get_functiondef('photo.editor_effect_intermediate_catalog_lost(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert 'qualification_ready' not in definition and 'final_origin_ready' not in definition
    withdrawal=db.execute("SELECT pg_get_functiondef('photo.withdraw_editor_effect_build()'::regprocedure)").fetchone()[0]
    assert 'editor_effect_intermediate_catalog_lost(o.tenant_id,o.run_id)' in withdrawal
    assert 'NOT photo.editor_effect_intermediate_catalog_lost(b.tenant_id,b.run_id)' in withdrawal
    assert 'EDITOR_INTERMEDIATE_UNAVAILABLE' in withdrawal
    assert "state='released',bytes=0" in withdrawal
    assert "status='cancelling'" in withdrawal
    signature='photo.editor_effect_intermediate_catalog_lost(uuid,uuid)'
    for role in ['photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_maintenance','public']:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
