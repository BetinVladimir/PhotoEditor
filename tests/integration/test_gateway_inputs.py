"""Gateway cannot use a task lease as broad tenant artifact access."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_task_leases import queued_task,claim


def setup(db):
    x=queued_task(db)
    db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",('f'*64,x['worker']))
    # Fixtures model already versioned storage, as production ingest must do.
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'version-1',%s,3,'image/png') RETURNING id",(x['tenant'],str(uuid4()),'a'*64)).fetchone()[0]
    command={**x['command'],'inputs':[dict(artifact_id=str(artifact),sha256='a'*64)]}
    db.execute('UPDATE photo.tasks SET inputs=%s,dispatch_command=%s WHERE id=%s',(Jsonb(command['inputs']),Jsonb(command),x['task']))
    x['command']=command
    assert claim(db,x)[0]=='claimed'
    return x,artifact


def read(db,x,artifact,**changes):
    return db.execute('SELECT * FROM photo.gateway_input(%s,%s,%s,%s,%s,%s)',
        (x['task'],changes.get('attempt',1),changes.get('fence',1),changes.get('worker',x['worker']),changes.get('certificate','f'*64),artifact)).fetchone()


def test_gateway_exact_input_scope(db):
    x,artifact=setup(db)
    unrelated=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'version-2',%s,3,'image/png') RETURNING id",(x['tenant'],str(uuid4()),'a'*64)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_gateway')
    row=read(db,x,artifact)
    assert row[0]==artifact and row[3]=='version-1' and row[4]=='a'*64
    assert read(db,x,unrelated) is None
    assert read(db,x,uuid4()) is None
    assert read(db,x,artifact,attempt=2) is None
    assert read(db,x,artifact,fence=2) is None
    assert read(db,x,artifact,worker=uuid4()) is None
    assert read(db,x,artifact,certificate='e'*64) is None
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():db.execute('SELECT * FROM photo.artifacts')
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert read(db,x,artifact) is None


def test_gateway_pinned_digest_and_artifact_expiry(db):
    x,artifact=setup(db)
    db.execute("UPDATE photo.tasks SET dispatch_command=jsonb_set(dispatch_command,'{inputs,0,sha256}',%s) WHERE id=%s",(Jsonb('b'*64),x['task']))
    db.execute('SET LOCAL ROLE photo_gateway');assert read(db,x,artifact) is None;db.execute('RESET ROLE')
    db.execute('UPDATE photo.tasks SET dispatch_command=%s WHERE id=%s',(Jsonb(x['command']),x['task']))
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,))
    db.execute('SET LOCAL ROLE photo_gateway');assert read(db,x,artifact) is None;db.execute('RESET ROLE')
    db.execute("UPDATE photo.artifacts SET expires_at=NULL,state='deleting' WHERE id=%s",(artifact,))
    db.execute('SET LOCAL ROLE photo_gateway');assert read(db,x,artifact) is None
