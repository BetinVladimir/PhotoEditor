"""Installed V2 graph accepts canonical prerequisite UUIDs, preserving ABI."""
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_database import tenant

SIGNATURE='photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)'

def test_real_two_node_draft_accepts_canonical_dependency(db):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tid,))
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'dependency') RETURNING id",(tid,)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,0) RETURNING id",(tid,actor,'a'*64)).fetchone()[0]
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,'application/json','version',clock_timestamp()) RETURNING id",(tid,str(uuid4()),'b'*64)).fetchone()[0]
    first,second,run=uuid4(),uuid4(),uuid4()
    tasks=[dict(id=str(task),node_key='verify/'+str(index),operation='asset.verify',operation_version='1',resource_class='media',params_artifact_id=str(params),params_sha='b'*64,input_digest='c'*64,execution_profile={'backend':'cpu'}) for index,task in enumerate((first,second))]
    edges=[dict(prerequisite_id=str(first),dependent_id=str(second))]
    args=(tid,run,uuid4(),project,snapshot,actor,'preview','d'*64,'e'*64,Jsonb({'project_id':str(project)}),Jsonb(tasks),Jsonb(edges),1024)
    query='SELECT photo.create_v2_draft_shell('+','.join(['%s']*13)+')'
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(query,args).fetchone()==(True,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT prerequisite_id,dependent_id FROM photo.task_edges WHERE tenant_id=%s AND run_id=%s',(tid,run)).fetchall()==[(first,second)]
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(1024,)

@pytest.mark.migration_head(2543)
def test_upgrade_preserves_function_identity_and_all_other_source(db):
    query='SELECT oid,proowner,proacl::text,prosecdef,prosrc FROM pg_proc WHERE oid=%s::regprocedure'
    before=db.execute(query,(SIGNATURE,)).fetchone()
    migration=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02544_v2_draft_edge_uuid_validation.sql').read_text().split('-- +goose Down')[0]
    db.execute(migration)
    after=db.execute(query,(SIGNATURE,)).fetchone()
    bad="coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'"
    good="coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'"
    assert before[:4]==after[:4]
    assert before[4].count(bad)==1 and after[4]==before[4].replace(bad,good)
    with pytest.raises(Exception,match='V2_DRAFT_EDGE_UUID_BASE_SOURCE_SHAPE'):
        with db.transaction():db.execute(migration)
