"""SQL receipt settlement scenarios only; physical purge remains maintenance evidence."""
from uuid import uuid4
import pytest
from test_gateway_outputs import setup, begin, complete


def charged_output(db):
    case = setup(db)
    db.execute('SET LOCAL ROLE photo_gateway')
    receipt = begin(db, case, uuid4())
    assert receipt[0] == 'created'
    assert complete(db, case, receipt[1])[0] == 'committed'
    db.execute('RESET ROLE')
    token = uuid4()
    db.execute("UPDATE photo.artifacts SET state='deleting',gc_token=%s,gc_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s", (token, receipt[2]))
    return case, receipt[2], token


def test_gc_settles_exact_output_charge_once(db):
    case, artifact, token = charged_output(db)
    before = db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, token)).fetchone() == (True,)
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, token)).fetchone() == (True,)
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, uuid4())).fetchone() == (False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone() == (before-2,)
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s', (artifact,)).fetchone() == ('deleted', True)


@pytest.mark.parametrize('refusal', ['expired', 'wrong_token', 'referenced'])
def test_gc_refusal_does_not_settle_charge(db, refusal):
    case, artifact, token = charged_output(db)
    if refusal == 'expired':
        db.execute("UPDATE photo.artifacts SET gc_lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (artifact,))
    if refusal == 'referenced':
        # Existing task reference is checked independently of task attempt state.
        db.execute("UPDATE photo.artifacts SET state='ready' WHERE id=%s", (artifact,))
        db.execute('UPDATE photo.tasks SET params_artifact_id=%s WHERE id=%s', (artifact, case['task']))
        db.execute("UPDATE photo.artifacts SET state='deleting' WHERE id=%s", (artifact,))
    before = db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone()
    if refusal == 'referenced':
        with pytest.raises(Exception, match='ARTIFACT_GC_REFERENCE_RACE'), db.transaction():
            db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, token))
    else:
        assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, uuid4() if refusal == 'wrong_token' else token)).fetchone() == (False,)
    assert db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone() == before
    assert db.execute('SELECT gc_storage_settled FROM photo.artifacts WHERE id=%s', (artifact,)).fetchone() == (False,)


def test_api_cannot_forge_storage_settlement_marker(db):
    case, artifact, token = charged_output(db)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(case['tenant']),))
    db.execute('SET LOCAL ROLE photo_api')
    with pytest.raises(Exception, match='ARTIFACT_GC_SETTLEMENT_PRIVATE'), db.transaction():
        db.execute('UPDATE photo.artifacts SET gc_storage_settled=true WHERE id=%s', (artifact,))
    with pytest.raises(Exception, match='ARTIFACT_GC_SETTLEMENT_PRIVATE'), db.transaction():
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,gc_storage_settled) VALUES(%s,%s,'test',%s,'v1',%s,2,'application/json','ready',clock_timestamp(),true)", (uuid4(),case['tenant'],str(uuid4()),'b'*64))
    db.execute('RESET ROLE')
    assert db.execute('SELECT gc_storage_settled FROM photo.artifacts WHERE id=%s', (artifact,)).fetchone() == (False,)
    db.execute('SET LOCAL ROLE photo_maintenance')
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)', (artifact, token)).fetchone() == (True,)


@pytest.mark.parametrize('field,value', [('bytes',3),('object_version','wrong-version'),('sha256','c'*64),('object_key','wrong-key'),('media_type','wrong/type'),('role','wrong-role')])
def test_gc_does_not_treat_mismatched_charge_receipt_as_uncharged(db,field,value):
    from psycopg import sql
    case,artifact,token=charged_output(db)
    db.execute(sql.SQL('UPDATE photo.output_uploads SET {}=%s WHERE artifact_id=%s').format(sql.Identifier(field)),(value,artifact))
    before=db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()
    with pytest.raises(Exception,match='ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH'),db.transaction():
        db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(artifact,token))
    assert db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==before
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleting',False)


def test_contact_sheet_font_has_exact_charged_provenance(db):
    case=setup(db)
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(case['tenant']),))
    db.execute('SET LOCAL ROLE photo_api')
    receipt=db.execute("SELECT * FROM photo.reserve_contact_sheet_font(%s,%s,2,'test')",(case['tenant'],'d'*64)).fetchone()
    assert receipt[0]=='created'
    assert db.execute("SELECT photo.complete_contact_sheet_font(%s,%s,%s,%s,2,'test','font-v1')",(case['tenant'],receipt[1],receipt[2],'d'*64)).fetchone()==(True,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT photo.artifact_gc_charged_bytes(%s,%s)',(case['tenant'],receipt[1])).fetchone()==(2,)


@pytest.mark.parametrize('field,value', [('output_role','original'),('object_key','originals/tenants/legacy-object')])
def test_generic_gc_refuses_dedicated_ingest_identity_before_settlement(db,field,value):
    from test_artifact_gc_draft_provenance import seed_corrupt_artifact
    case,original,token=charged_output(db)
    # Negative persisted identity fixture: no publication or charge assertion
    # for the new artifact; dedicated identity must refuse before provenance.
    artifact=seed_corrupt_artifact(db,original,field,value)
    before=db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()
    with pytest.raises(Exception,match='ARTIFACT_ACCOUNTING_DEDICATED_INGEST'),db.transaction():
        db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(artifact,token))
    assert db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==before
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleting',False)
    db.execute("UPDATE photo.artifacts SET state='ready',expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(artifact,))
    claimed=db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchall()
    assert all(row[0]!=artifact for row in claimed)
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('ready',False)


def test_generic_gc_never_infers_zero_charge_from_missing_provenance(db):
    case,artifact,token=charged_output(db)
    db.execute('DELETE FROM photo.output_uploads WHERE artifact_id=%s',(artifact,))
    before=db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()
    with pytest.raises(Exception,match='ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN'),db.transaction():
        db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(artifact,token))
    assert db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==before
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleting',False)
    db.execute("UPDATE photo.artifacts SET state='ready',expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(artifact,))
    assert all(row[0]!=artifact for row in db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchall())
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('ready',False)
