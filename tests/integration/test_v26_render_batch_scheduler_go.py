"""Run actual Go scheduler adapter with two committed, isolated metadata batches."""
from pathlib import Path as _PhotoRepositoryPath
import os
import json
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from conftest import ROOT
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def test_go_batch_pass_updates_terminal_and_intermediate(database):
    with psycopg.connect(**database) as db:
        _,terminal=seed_failed_batch(db)
        tenant,queued=seed_failed_batch(db)
        db.execute("UPDATE photo.runs SET status='queued' WHERE tenant_id=%s AND kind='v2'",(tenant,))
        db.execute("UPDATE photo.tasks SET state='blocked',terminal_reason=NULL WHERE tenant_id=%s",(tenant,))
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BATCH_TERMINAL=str(terminal),PHOTO_BATCH_QUEUED=str(queued),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result=subprocess.run(['go','test','-json','./internal/workflow','-run','^TestIntegrationRenderBatchPassFinalizesAndSynchronizes$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    _capture_named_go(result, 'TestIntegrationRenderBatchPassFinalizesAndSynchronizes')
    assert result.returncode==0,result.stdout+result.stderr


def _capture_named_go(result, name, *, require_pass=True):
    """Persist only closed Go event metadata: never child output, DSN or object config."""
    from uuid import uuid4
    from hashlib import sha256
    events = []
    for line in result.stdout.splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        events.append({key: event[key] for key in ('Action', 'Package', 'Test', 'Elapsed') if key in event})
    evidence = ROOT/'work/specialized-v26-go-evidence'
    evidence.mkdir(parents=True, exist_ok=True)
    record = dict(root=name, exit_code=result.returncode, events=events,
                  stdout_sha256=sha256(result.stdout.encode()).hexdigest(),
                  stderr_sha256=sha256(result.stderr.encode()).hexdigest())
    path = evidence/(name+'-'+uuid4().hex+'.json')
    path.write_text(json.dumps(record, indent=2)+'\n')
    package = ('photoeditor/backend/internal/workflow' if name in ('TestIntegrationRenderBatchPassFinalizesAndSynchronizes', 'TestIntegrationRenderRecoveryRuntimeLoop') else 'photoeditor/backend/internal/v2draft' if name == 'TestIntegrationRenderCatalogFrameAuthority' else 'photoeditor/backend/internal/v2render')
    exact = [event for event in events if event.get('Test') == name and event.get('Package') == package]
    assert sum(event.get('Action') == 'run' for event in exact) == 1, str(path)
    assert not any(event.get('Test') == name and event.get('Package') != package for event in events), str(path)
    assert not any(event.get('Action') == 'skip' for event in events), str(path)
    if require_pass:
        assert result.returncode == 0, str(path)
        assert [event['Action'] for event in exact if event.get('Action') in ('pass', 'fail', 'skip')] == ['pass'], str(path)
        assert not any(event.get('Action') == 'fail' for event in events), str(path)
    else:
        assert result.returncode != 0, str(path)
        assert not any(event.get('Action') == 'pass' for event in exact), str(path)

