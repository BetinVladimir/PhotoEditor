"""Authored receipt provenance from real reserve/complete SQL; no object bytes implied.
The seed composition contains synthetic source metadata. The artifact under review is
always produced by the real publication function and its actual quota transaction.
"""
import json
from uuid import uuid4
import pytest
from psycopg import errors
from layer_original_fixture import seed_original_composition


def completed_draft(db,kind):
    case=seed_original_composition(db)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
    scope=json.loads(case['Params'])['base']['asset_version_id']
    if kind=='shot':
        collection=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'GC shot provenance') RETURNING id",(case['Tenant'],case['Project'])).fetchone()[0]
        scope=db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch) VALUES(%s,%s,%s,'GC provenance','open','{}',1) RETURNING id",(case['Tenant'],case['Project'],collection)).fetchone()[0]
    size=228 if kind=='mask' else 32
    args=(case['Tenant'],scope,uuid4(),case['Actor'],case['Session'],'a'*64,size,'test')
    reserve='reserve_v25_user_mask_pixels' if kind=='mask' else 'reserve_v24_shot_upload' if kind=='shot' else 'reserve_v2_draft_upload'
    complete='complete_v25_user_mask_pixels' if kind=='mask' else 'complete_v24_shot_upload' if kind=='shot' else 'complete_v2_draft_upload'
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        db.execute('SET LOCAL ROLE photo_api')
        reserve_args=(*args,5,5) if kind=='mask' else args
        ticket=db.execute('SELECT * FROM photo.'+reserve+'('+','.join(['%s']*len(reserve_args))+')',reserve_args).fetchone()
        assert ticket[0]=='created'
        completion=(*args[:5],ticket[1],ticket[2],*args[5:],'provenance-v1')
        if kind=='mask':completion=(*completion,5,5)
        assert db.execute('SELECT photo.'+complete+'('+','.join(['%s']*len(completion))+')',completion).fetchone()==(True,)
        db.execute('RESET ROLE')
    return case['Tenant'],ticket[1],size


def charged(db,tenant,artifact):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result=db.execute('SELECT photo.artifact_gc_charged_bytes(%s,%s)',(tenant,artifact)).fetchone()
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('kind',['document','mask','shot'])
def test_real_completed_draft_receipt_matches_exact_provenance(db,kind):
    tenant,artifact,size=completed_draft(db,kind)
    before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()
    assert charged(db,tenant,artifact)==(size,)
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==before
    assert db.execute('SELECT object_key FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('tenants/'+tenant+'/v2-drafts/'+str(artifact),)


def seed_corrupt_artifact(db, original, field, value):
    """Hostile persisted metadata fixture, not a successful publication path.

    Insert immutable identity/content once, retaining the completed fixture's
    tuple except the selected discriminator. No trigger is disabled or bypassed.
    Receipt redirection by the caller models catalog corruption for the private
    accounting guard; it does not assert production permission to do so.
    """
    assert field in ('object_key','media_type','output_role')
    artifact=uuid4()
    row=db.execute('SELECT object_key,media_type,output_role FROM photo.artifacts WHERE id=%s',(original,)).fetchone()
    values=dict(zip(('object_key','media_type','output_role'),row))
    values['object_key']=values['object_key'].replace(str(original),str(artifact))
    values[field]=value
    db.execute("""INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,
      sha256,bytes,media_type,output_role,state,verified_at,gc_token,gc_lease_until,
      created_by_task_id,produced_attempt,produced_fence)
      SELECT %s,tenant_id,bucket,%s,object_version,sha256,bytes,%s,%s,state,
      verified_at,gc_token,gc_lease_until,created_by_task_id,produced_attempt,produced_fence
      FROM photo.artifacts WHERE id=%s""",
      (artifact,values['object_key'],values['media_type'],values['output_role'],original))
    return artifact


@pytest.mark.parametrize('kind',['document','mask','shot'])
@pytest.mark.parametrize('field,value',[('object_key','tenants/foreign/unknown'),('media_type','image/jpeg'),('output_role','wrong_role')])
def test_seeded_corrupt_draft_catalog_refuses_key_media_or_role(db,kind,field,value):
    tenant,artifact,size=completed_draft(db,kind)
    assert charged(db,tenant,artifact)==(size,)
    original=artifact
    artifact=seed_corrupt_artifact(db,original,field,value)
    db.execute('UPDATE photo.v2_draft_uploads SET artifact_id=%s WHERE tenant_id=%s AND artifact_id=%s',(artifact,tenant,original))
    before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()
    with pytest.raises(errors.CheckViolation,match='ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH'):
        with db.transaction():charged(db,tenant,artifact)
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==before
    assert db.execute('SELECT gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==(False,)
