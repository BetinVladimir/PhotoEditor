"""Lease-owned recovery of genuinely accepted legacy Editor prestages.

No immutable run config or accepted response is edited to manufacture a legacy
command. Supply two isolated pre02549 accepted fixtures (phase editor_precompose,
missing calibrated profile): one without tasks, one with a queued idle task.
The actual claim/terminal/cancellation/Editor withdrawal functions are exercised.
This authored scenario has not been executed or credited as qualification.
"""
import json
import os
from uuid import UUID,uuid4
import pytest


def test_accepted_legacy_profile_failure_uses_existing_settlement_exactly_once(db):
    raw=os.environ.get('PHOTO_TEST_LEGACY_EDITOR_PROFILE_FIXTURES','')
    if not raw:pytest.skip('two actual accepted isolated pre02549 Editor fixtures required')
    fixtures=json.loads(raw)
    assert isinstance(fixtures,list) and len(fixtures)==2
    with db.transaction(force_rollback=True):
        for fixture in fixtures:
            tenant,run,source=(UUID(fixture[k]) for k in ('tenant_id','run_id','source_artifact_id'))
            live=fixture['has_queued_task'];assert type(live) is bool
            before=db.execute('SELECT state,sha256,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,source)).fetchone();assert before is not None
            config=db.execute('SELECT config_snapshot,config_hash FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone();assert config is not None
            assert config[0].get('editor_precompose') and not config[0]['editor_precompose'].get('execution_profile')
            reserved=db.execute("SELECT coalesce(sum(bytes),0) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'",(tenant,run)).fetchone()[0]
            usage=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
            tasks=db.execute('SELECT id,state FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchall()
            assert (len(tasks)>0)==live
            if live:assert all(t[1]=='queued' for t in tasks)
            outputs=db.execute('SELECT id,sha256,object_version,state FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id IN(SELECT id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s) ORDER BY id',(tenant,tenant,run)).fetchall()
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                claims=db.execute('SELECT * FROM photo.claim_apply_build_versioned()').fetchall()
                assert len(claims)==1 and claims[0][1]==run,'fixture must be the next isolated lease candidate'
                # Avoid assuming composite return column order for the lease token.
                db.execute('RESET ROLE')
                token=db.execute('SELECT lease_token FROM photo.apply_builds WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchone()[0]
                with db.transaction(force_rollback=True):
                    db.execute("UPDATE photo.apply_builds SET lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND run_id=%s",(tenant,run))
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    assert db.execute('SELECT photo.fail_editor_precompose_profile(%s,%s)',(run,token)).fetchone()==(False,)
                    db.execute('RESET ROLE')
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT photo.fail_editor_precompose_profile(%s,%s)',(run,uuid4())).fetchone()==(False,)
                assert db.execute('SELECT photo.fail_editor_precompose_profile(%s,%s)',(run,token)).fetchone()==(True,)
                assert db.execute('SELECT photo.fail_editor_precompose_profile(%s,%s)',(run,token)).fetchone()==(False,)
                db.execute('RESET ROLE')
                state=db.execute('SELECT status,cancel_requested FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone()
                assert state==(('cancelling',True) if live else ('failed',False))
                interim=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
                assert interim==(usage if live else usage-reserved)
                db.execute('SET LOCAL ROLE photo_scheduler')
                if live:
                    for _ in range(len(tasks)+1):db.execute('SELECT photo.cancel_idle_task()')
                    db.execute('SELECT photo.finalize_run_cancellation()')
                db.execute('SELECT photo.withdraw_editor_effect_build()')
                db.execute('RESET ROLE')
                assert db.execute('SELECT state FROM photo.editor_effect_builds WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchone()==('failed',)
                assert db.execute("SELECT last_error FROM photo.apply_builds WHERE tenant_id=%s AND run_id=%s",(tenant,run)).fetchone()==('EDITOR_PRECOMPOSE_PROFILE_UNAVAILABLE',)
                assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]==usage-reserved
                assert db.execute('SELECT config_snapshot,config_hash FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone()==config
                assert db.execute('SELECT state,sha256,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,source)).fetchone()==before
                assert db.execute('SELECT id,sha256,object_version,state FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id IN(SELECT id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s) ORDER BY id',(tenant,tenant,run)).fetchall()==outputs
