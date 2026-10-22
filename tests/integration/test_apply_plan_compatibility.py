"""Accepted Apply plan compatibility: authored, not executed."""
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def test_old_pending_culling_run_requests_normal_cancellation_without_replan(db):
    tenant,run,token,_=_build(db)
    config={'style_config':{'mode':'fixed','extensions':{'culling':{'enabled':True,'never_delete':True,'eye_policy':'review'}}}}
    db.execute("UPDATE photo.apply_builds SET plan_contract_version=1,phase='quality',lease_token=NULL,lease_until=NULL WHERE run_id=%s",(run,))
    db.execute("UPDATE photo.runs SET status='waiting_review',config_snapshot=%s WHERE id=%s",(Jsonb(config),run))
    before=db.execute('SELECT input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(run,)).fetchone()
    assert db.execute('SELECT photo.cancel_unsupported_apply_plans()').fetchone()[0]==1
    assert db.execute('SELECT input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(run,)).fetchone()==before
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(run,)).fetchone()==('cancelling',True)
    assert db.execute('SELECT phase,last_error FROM photo.apply_builds WHERE run_id=%s',(run,)).fetchone()==('failed','APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING')
    assert db.execute('SELECT count(*) FROM photo.apply_v4_stages WHERE run_id=%s',(run,)).fetchone()[0]==0
    assert db.execute('SELECT photo.cancel_unsupported_apply_plans()').fetchone()[0]==0
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0]==1
    assert db.execute('SELECT status FROM photo.runs WHERE id=%s',(run,)).fetchone()==('cancelled',)


def test_current_plan_version_and_runnable_old_series_are_unchanged(db):
    tenant,run,token,_=_build(db)
    assert db.execute('SELECT plan_contract_version FROM photo.apply_builds WHERE run_id=%s',(run,)).fetchone()==(2,)
    config={'style_config':{'mode':'fixed','extensions':{'series':{'enabled':True}}}}
    db.execute('UPDATE photo.runs SET config_snapshot=%s WHERE id=%s',(Jsonb(config),run))
    db.execute('UPDATE photo.apply_builds SET plan_contract_version=1 WHERE run_id=%s',(run,))
    assert db.execute('SELECT photo.cancel_unsupported_apply_plans()').fetchone()[0]==0
    assert not db.execute('SELECT photo.request_apply_plan_restart(%s,%s,%s,%s)',(tenant,run,token,'APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT')).fetchone()[0]
    frozen=db.execute('SELECT photo.apply_v4_frozen_plan(%s,%s,%s)',(tenant,run,token)).fetchone()[0]
    assert frozen==dict(version=1,stages=[],config=config)
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(run,)).fetchone()==('queued',False)


def test_compatibility_uses_disabled_override_without_falling_back_to_style(db):
    tenant,run,token,_=_build(db)
    config={'style_config':{'mode':'fixed','extensions':{'culling':{'enabled':True}}},'job_overrides':{'modules':{'extensions':{'culling':None}}}}
    db.execute('UPDATE photo.runs SET config_snapshot=%s WHERE id=%s',(Jsonb(config),run))
    db.execute('UPDATE photo.apply_builds SET plan_contract_version=1 WHERE run_id=%s',(run,))
    assert db.execute('SELECT photo.cancel_unsupported_apply_plans()').fetchone()[0]==0
    assert not db.execute('SELECT photo.request_apply_plan_restart(%s,%s,%s,%s)',(tenant,run,token,'APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING')).fetchone()[0]


def test_legacy_top_level_culling_is_detected_without_rewriting_frozen_plan(db):
    tenant, run, token, _ = _build(db)
    config = {'style_config': {'mode': 'fixed', 'culling': {'enabled': True, 'never_delete': True, 'eye_policy': 'review'}}}
    db.execute("UPDATE photo.runs SET config_snapshot=%s,status='waiting_review' WHERE id=%s", (Jsonb(config), run))
    db.execute("UPDATE photo.apply_builds SET plan_contract_version=1,lease_token=NULL,lease_until=NULL WHERE run_id=%s", (run,))
    assert db.execute('SELECT photo.cancel_unsupported_apply_plans()').fetchone()[0] == 1
    status, frozen = db.execute('SELECT status,config_snapshot FROM photo.runs WHERE id=%s', (run,)).fetchone()
    assert status == 'cancelling'
    assert frozen == config
    assert db.execute('SELECT photo.apply_plan_restart_diagnostic(%s,%s)', (tenant, run)).fetchone()[0] == 'APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING'
