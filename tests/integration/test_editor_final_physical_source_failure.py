"""Physical failure transport boundary; authored only, never a runtime PASS.

Uses exact descriptor refusal/ACL and current withdrawal integration checks.
The seeded build intentionally lacks admitted final-origin authority; it must
not be possible to terminalize it by claiming missing bytes.
"""
from uuid import uuid4
import pytest
from test_editor_intermediate_catalog_withdrawal import sealed_catalog
from editor_admitted_final_fixture import admitted_final


@pytest.mark.parametrize('code',['EDITOR_SOURCE_OBJECT_MISSING','EDITOR_SOURCE_OBJECT_INTEGRITY'])
def test_admitted_editor_physical_failure_drains_before_exact_quota_release(db,code):
    tid,build,final,pending,descriptor,version,source,source_sha=admitted_final(db)
    initial_reserved=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,build,*descriptor,code)).fetchone()[0] is True
    db.execute('RESET ROLE')
    # The failure withdraws publication immediately, without pretending a live
    # task is terminal or releasing capacity that its cancellation still owns.
    assert db.execute('SELECT photo.editor_effect_final_origin_ready(%s,%s)',(tid,build)).fetchone()[0] is False
    witness=db.execute('SELECT final_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()[0]
    assert witness['code']==code
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.withdraw_editor_effect_build()').fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(final,)).fetchone()==('cancelling',True)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(pending,)).fetchone()[0]=='ready'
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]==initial_reserved
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.cancel_idle_task()').fetchone()[0]==1
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0]==1
        assert db.execute('SELECT photo.withdraw_editor_effect_build()').fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(pending,)).fetchone()[0]=='cancelled'
    assert db.execute('SELECT status FROM photo.runs WHERE id=%s',(final,)).fetchone()[0]=='cancelled'
    assert db.execute('SELECT state FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()[0]=='failed'
    assert db.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_id IN(%s,%s,(SELECT seal_run_id FROM photo.editor_effect_builds WHERE run_id=%s)) AND state='released' AND bytes=0",(tid,build,final,build)).fetchone()[0]==3
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]==initial_reserved-896
    # Replays cannot overwrite the append-once witness or decrement twice.
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,build,*descriptor,code)).fetchone()[0] is False
        assert db.execute('SELECT photo.withdraw_editor_effect_build()').fetchone()[0] is False
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]==initial_reserved-896
    assert db.execute('SELECT final_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()[0]==witness
    assert db.execute('SELECT master_artifact_id,content_hash FROM photo.asset_versions WHERE id=%s',(version,)).fetchone()==(source,source_sha)
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')


def test_editor_physical_failure_is_scheduler_only_and_unknown_origin_safe(db):
    signature='photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)'
    for role in ['photo_api','photo_worker','photo_portal','photo_gateway','photo_maintenance','public','photo_scheduler']:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),'fixture','input','v1','a'*64,2,'application/json','EDITOR_SOURCE_OBJECT_MISSING')).fetchone()[0] is False


@pytest.mark.parametrize('case',['unadmitted','foreign_build','foreign_tenant','wrong_key','wrong_version','wrong_sha','wrong_size','wrong_media','untyped_error'])
def test_unbound_or_unadmitted_physical_witness_never_fails_editor(db,case):
    tenant,build,refs=sealed_catalog(db)
    artifact=refs[0][0]
    bucket,key,version,digest,size,media=db.execute('SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()
    code='EDITOR_SOURCE_OBJECT_MISSING'
    if case=='foreign_build':build=uuid4()
    elif case=='foreign_tenant':tenant=uuid4()
    elif case=='wrong_key':key='foreign/key'
    elif case=='wrong_version':version='foreign-v2'
    elif case=='wrong_sha':digest='e'*64
    elif case=='wrong_size':size+=1
    elif case=='wrong_media':media='application/x-npy'
    elif case=='untyped_error':code='NETWORK_TIMEOUT'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant,build,bucket,key,version,digest,size,media,code)).fetchone()[0] is False
    assert db.execute('SELECT count(*) FROM photo.editor_effect_builds WHERE final_source_failure IS NOT NULL').fetchone()[0]==0


def test_editor_physical_failure_withdraws_authority_before_normal_drain(db):
    origin=db.execute("SELECT pg_get_functiondef('photo.editor_effect_final_origin_ready(uuid,uuid)'::regprocedure)").fetchone()[0]
    withdrawal=db.execute("SELECT pg_get_functiondef('photo.withdraw_editor_effect_build()'::regprocedure)").fetchone()[0]
    finalizer=db.execute("SELECT pg_get_functiondef('photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)'::regprocedure)").fetchone()[0]
    assert 'b.final_source_failure IS NULL' in origin
    assert 'o.final_source_failure IS NOT NULL' in withdrawal
    assert 'IF b.final_source_failure IS NULL AND' in withdrawal
    assert "status='cancelling'" in withdrawal and "state='released',bytes=0" in withdrawal
    assert 'photo.editor_effect_final_artifact(p_tenant,p_build,a.id,a.sha256)' in finalizer
    for field in ['bucket','object_key','object_version','sha256','bytes','media_type']:
        assert 'a.'+field in finalizer
