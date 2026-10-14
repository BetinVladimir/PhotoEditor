"""Authenticated freeze HTTP boundary with a controlled service, not GPU qualification."""
from pathlib import Path
import os
import subprocess
from psycopg.conninfo import make_conninfo


def test_render_freeze_http_authenticated_contract(database):
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/httpapi','-run','^TestIntegrationRenderFreezeHTTP$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr


def test_concrete_freeze_saved_replay_without_object_reads(database):
    import json
    import psycopg
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from test_v26_render_draft_freeze_fence import _seed_registration
    key='freeze-saved-replay'
    with psycopg.connect(**database) as conn:
        args,register,tid,stream,artifact,document,draft,revision,*_=_seed_registration(conn,freeze_key=key)
        assert conn.execute(register,args).fetchone()==(True,)
        conn.execute('RESET ROLE')
        asset=conn.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tid,args[1])).fetchone()[0]
        request=dict(document_id=str(draft),expected_revision=str(revision),assume_profile=None)
        response=dict(document_id=str(document),draft_document_id=str(draft),draft_revision=str(revision),source=json.loads(args[9])['recipe']['source'],recipe=dict(artifact_id=str(args[5]),sha256=args[6]))
        conn.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,resource_id,expires_at) VALUES(%s,'user',%s,%s,%s,%s,200,%s,%s,clock_timestamp()+interval '1 hour')",(tid,args[3],'Render.Freeze.'+str(asset),key,sha256(canonical_bytes(request)).hexdigest(),json.dumps(response),document))
        # A later draft edit must not change the original completed response.
        conn.execute('UPDATE photo.v2_document_streams SET revision=revision+1 WHERE tenant_id=%s AND id=%s',(tid,stream))
        fixture=dict(Tenant=str(tid),Actor=str(args[3]),Session=str(args[4]),Asset=str(asset),Key=key,Request=request)
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_RENDER_FREEZE_REPLAY=json.dumps(fixture),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationRenderFreezeSavedReplay$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr
