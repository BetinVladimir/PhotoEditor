"""Real API draft publication and scheduler scope; synthetic parent metadata."""
from pathlib import Path as _PhotoRepositoryPath
from pathlib import Path
import json
from hashlib import sha256
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from layer_original_fixture import seed_original_composition
from test_v25_lens_objectstore import lens_objects

ROOT = Path(__file__).resolve().parents[2]


def test_composition_params_publication_replay(database, lens_objects):
    client, bucket = lens_objects
    with psycopg.connect(**database, autocommit=True) as db:
        with db.transaction():
            case = seed_original_composition(db)
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING', (case['Tenant'],))
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s",(case['Tenant'],))
        case['Objects'] = client._photo_test_config
        before = db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['Tenant'],)).fetchone()
        env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database), PHOTO_COMPOSITION_PUBLISH_CASE=json.dumps(case), GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
        result = subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationCompositionParamsPublication$','-count=1'], cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode == 0, result.stdout + result.stderr
        rows = db.execute('SELECT a.bytes,a.object_version,u.object_version FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.bucket=%s AND u.state=\'ready\'', (case['Tenant'],bucket)).fetchall()
        assert len(rows) == 1
        size, artifact_version, upload_version = rows[0]
        assert artifact_version == upload_version and artifact_version not in ('','null',None)
        after = db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['Tenant'],)).fetchone()
        reservations=db.execute("SELECT q.bytes,r.status,r.graph_sealed FROM photo.quota_reservations q JOIN photo.runs r ON r.tenant_id=q.tenant_id AND r.id=q.holder_id WHERE q.tenant_id=%s AND q.holder_type='run' AND r.config_snapshot ? 'v25_composition'",(case['Tenant'],)).fetchall()
        assert reservations == [(24*25+32*1024*1024,'draft',False)]
        transport=db.execute("SELECT d.purpose,d.bytes,d.object_version,a.object_version,a.object_key,a.sha256 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=%s AND d.bucket=%s AND d.state='ready' ORDER BY d.purpose",(case['Tenant'],bucket)).fetchall()
        assert [row[0] for row in transport] == ['v2_input_chunk_0','v2_input_manifest']
        assert all(row[2]==row[3] and row[2] not in ('','null',None) for row in transport)
        documents=[]
        for _,length,version,_,key,digest in transport:
            response=client.get_object(bucket,key,version_id=version)
            try:payload=response.read()
            finally:response.close();response.release_conn()
            assert len(payload)==length and sha256(payload).hexdigest()==digest
            documents.append(json.loads(payload))
        chunk,manifest=documents
        assert manifest['entry_count']==len(chunk['entries'])
        entries=json.dumps(chunk['entries'],sort_keys=True,separators=(',',':')).encode()
        assert manifest['entries_sha']==sha256(entries).hexdigest()
        assert after == (before[0] + size + sum(row[1] for row in transport), before[1]+reservations[0][0])
        tasks=db.execute("SELECT t.operation,t.state,t.attempt,t.inputs FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v25_composition'",(case['Tenant'],)).fetchall()
        assert tasks == [('layer.compose','blocked',0,[])]
        run,task=db.execute("SELECT r.id,t.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v25_composition'",(case['Tenant'],)).fetchone()
        digest=sha256(b'{}').hexdigest()
        db.execute('SET ROLE photo_scheduler')
        try:
            reservation=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],run,task,'v2_input_chunk_1',digest,2,bucket)).fetchone()
        finally:db.execute('RESET ROLE')
        assert reservation[0]=='created' and reservation[3] is False
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
        db.execute('SET ROLE photo_scheduler')
        try:
            completed=db.execute('SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],run,task,'v2_input_chunk_1',reservation[1],reservation[2],digest,2,bucket,'synthetic-unpublished')).fetchone()[0]
        finally:db.execute('RESET ROLE')
        assert completed is False
        assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(reservation[1],)).fetchone()[0]==0
        manifest_row=transport[1]
        response=client.get_object(bucket,manifest_row[4],version_id=manifest_row[2])
        try:manifest_body=response.read().decode()
        finally:response.close();response.release_conn()
        case.update(ExistingRun=str(run),RevokedTransport=True,ManifestBody=manifest_body)
        env['PHOTO_COMPOSITION_PUBLISH_CASE']=json.dumps(case)
        result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationCompositionParamsPublication$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode==0,result.stdout+result.stderr
        final=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()
        assert final==(after[0],after[1]+2)
