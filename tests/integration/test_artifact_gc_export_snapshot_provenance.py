"""Authored real recipe + export snapshot publication and accounting scenarios.
No object IO, migrations or scenarios were executed during authoring.
"""
import json
from hashlib import sha256
from uuid import uuid4
import pytest
from psycopg import errors
from layer_original_fixture import seed_original_composition
from test_artifact_gc_draft_provenance import charged, seed_corrupt_artifact


def wire(value):return json.dumps(value,sort_keys=True,separators=(',',':'))


def completed_snapshot(db):
    case=seed_original_composition(db);tenant=case['Tenant'];binding=json.loads(case['Params'])['base']
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tenant,))
    body=wire(dict(schema_version=1,recipe=dict(schema_version=3,source=binding),recipe_sha256='c'*64,scalars=dict(source=binding),learning=None))
    digest=sha256(body.encode()).hexdigest();document=uuid4();key=str(uuid4())
    snapshot=db.execute("SELECT photo.v30_uuid5(%s,'interchange:export-snapshot:'||%s||':'||%s||':'||%s)",(tenant,case['Project'],case['Actor'],key)).fetchone()[0]
    artifact=db.execute("SELECT photo.v30_uuid5(%s,'interchange:export-snapshot:artifact:1')",(snapshot,)).fetchone()[0]
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,));db.execute('SET LOCAL ROLE photo_api')
        args=(tenant,binding['asset_version_id'],document,case['Actor'],case['Session'],digest,len(body.encode()),'test')
        ticket=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
        assert ticket[0]=='created'
        assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*args[:5],ticket[1],ticket[2],*args[5:],'recipe-v1')).fetchone()==(True,)
        assert db.execute('SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')',(*args[:5],ticket[1],digest,binding['source']['artifact_id'],binding['source']['sha256'],body)).fetchone()==(True,)
        item=dict(ordinal=0,binding=binding,working_image=binding['source'],recipe=dict(artifact_id=str(ticket[1]),sha256=digest))
        request=wire(dict(items=[item]));snapshot_body=wire(dict(schema_version=1,snapshot_id=str(snapshot),snapshot_digest='f'*64,items=[item]))
        snapshot_sha=sha256(snapshot_body.encode()).hexdigest()
        args=(tenant,case['Actor'],case['Session'],case['Project'],key,snapshot,artifact,request,snapshot_body,snapshot_sha,'test')
        reserved=db.execute('SELECT * FROM photo.reserve_v30_export_snapshot('+','.join(['%s']*11)+')',args).fetchone()
        assert reserved[0]=='created' and reserved[1]==artifact
        completion=(*args[:7],reserved[2],sha256(request.encode()).hexdigest(),snapshot_sha,'test','snapshot-v1')
        assert db.execute('SELECT photo.complete_v30_export_snapshot('+','.join(['%s']*12)+')',completion).fetchone()==(True,)
        db.execute('RESET ROLE')
    return tenant,artifact,len(snapshot_body.encode())


def test_real_export_snapshot_is_charged_and_shared_gc_settles_once(db):
    tenant,artifact,size=completed_snapshot(db)
    assert charged(db,tenant,artifact)==(size,)
    before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()
    token=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.artifact_has_references(%s,%s)',(tenant,artifact)).fetchone()==(False,)
        db.execute("UPDATE photo.artifacts SET state='deleting',gc_token=%s,gc_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s",(token,artifact))
        db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_maintenance')
        # Caller enters only after physical removal is independently confirmed.
        assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(artifact,token)).fetchone()==(True,)
        assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(artifact,token)).fetchone()==(True,)
        db.execute('RESET ROLE')
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(before[0]-size,before[1])
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleted',True)


@pytest.mark.parametrize('field,value',[('object_key','tenants/other/snapshot'),('media_type','image/jpeg'),('output_role','wrong_role')])
def test_seeded_corrupt_snapshot_catalog_never_becomes_zero_charge(db,field,value):
    tenant,artifact,size=completed_snapshot(db)
    assert charged(db,tenant,artifact)==(size,)
    # Negative metadata fixture only; positive reserve/complete remains above.
    original=artifact
    artifact=seed_corrupt_artifact(db,original,field,value)
    db.execute('UPDATE photo.v30_export_snapshot_documents SET artifact_id=%s WHERE tenant_id=%s AND artifact_id=%s',(artifact,tenant,original))
    with pytest.raises(errors.CheckViolation,match='ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH'):
        with db.transaction():charged(db,tenant,artifact)
    assert db.execute('SELECT gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==(False,)
