from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_report import persist_recovery_report
from photo_worker.runtime.restore_replay_receipt import verify_restore_replay_files
from test_restore_report import evidence


@pytest.mark.parametrize('mode',['valid','foreign_run','foreign_database','foreign_build',
    'wrong_recovery_pin','bad_replay_pin','partial','bool_count','approval','noncanonical',
    'early_completion','public_file','wrong_filename'])
def test_actual_recovery_and_replay_files_are_required(tmp_path,mode):
    now=datetime.now(timezone.utc);result=evidence(tmp_path,now)
    report=persist_recovery_report(tmp_path,result,build_sha256='d'*64,
        backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,check_active=lambda:None)
    replay={'schema_version':'restore-task-replay-receipt/v1','run_id':str(result.preflight.run_id),
        'recovery_sha256':report.sha256,'build_sha256':'d'*64,'database':'isolated',
        'initially_pending':0,'confirmed':0,'completed_at':now.isoformat(),'writers_authorized':False}
    if mode=='foreign_run':replay['run_id']=str(uuid4())
    if mode=='foreign_database':replay['database']='production'
    if mode=='foreign_build':replay['build_sha256']='a'*64
    if mode=='wrong_recovery_pin':replay['recovery_sha256']='b'*64
    if mode=='partial':replay['initially_pending']=1
    if mode=='bool_count':replay['confirmed']=False
    if mode=='approval':replay['writers_authorized']=True
    if mode=='early_completion':replay['completed_at']='2020-01-01T00:00:00Z'
    raw=canonical_bytes(replay)
    if mode=='noncanonical':raw+=b'\n'
    path=tmp_path/('replay.json' if mode=='wrong_filename' else
                   str(result.preflight.run_id)+'.replay.json')
    path.write_bytes(raw);path.chmod(0o644 if mode=='public_file' else 0o600)
    pin='0'*64 if mode=='bad_replay_pin' else sha256(raw).hexdigest()
    def run():return verify_restore_replay_files(report.path,report.sha256,path,pin,
        evidence=result,build_sha256='d'*64,check_active=lambda:None)
    if mode=='valid':
        verified=run();assert verified.run_id==result.preflight.run_id and verified.confirmed==0
        assert verified.replay_sha256==pin
    else:
        with pytest.raises(ValueError):run()
