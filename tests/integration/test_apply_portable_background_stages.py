"""Apply portable-asset stage admission. Authored only; not executed."""
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def test_portable_background_stages_register_under_scheduler_allowlist(db):
    tenant,run,token,_=_build(db)
    plan=[
        dict(phase='resolve',operation='style.resolve',output_role='resolved_style',cardinality='item'),
        dict(phase='color',operation='image.color',output_role='working_image',cardinality='item'),
        dict(phase='local',operation='image.local',output_role='working_image',cardinality='item'),
        dict(phase='background_inspect',operation='asset.inspect',output_role='metadata',cardinality='item'),
        dict(phase='background_decode',operation='asset.decode',output_role='working_image',cardinality='item'),
        dict(phase='background',operation='background.compose',output_role='working_image',cardinality='item'),
        dict(phase='quality',operation='quality.check',output_role='quality_report',cardinality='item'),
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    stages=db.execute('SELECT phase,operation FROM photo.apply_v4_stages WHERE tenant_id=%s AND run_id=%s ORDER BY stage_index',(tenant,run)).fetchall()
    assert stages[3:6]==[('background_inspect','asset.inspect'),('background_decode','asset.decode'),('background','background.compose')]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    assert not db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'f'*64,Jsonb(plan))).fetchone()[0]
    assert not db.execute("SELECT EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE p.oid='photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure AND acl.grantee=0 AND acl.privilege_type='EXECUTE')").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)','EXECUTE')").fetchone()[0]
