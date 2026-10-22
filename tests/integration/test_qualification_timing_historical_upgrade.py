"""Historical timing compatibility reproduction; direct rows are relational fixtures."""
from pathlib import Path
from uuid import uuid4
import pytest
import psycopg
ROOT=Path(__file__).resolve().parents[2]
def seed(db,trusted,status):
 actor=db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(str(uuid4())+'@example.test',)).fetchone()[0]
 tenant=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Timing',%s,'{}','{}') RETURNING id",(actor,)).fetchone()[0]
 db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'owner')",(tenant,actor))
 project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Timing') RETURNING id",(tenant,)).fetchone()[0]
 report=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'fixture',%s,'v1',%s,2,'application/json',clock_timestamp()) RETURNING id",(tenant,str(uuid4()),'a'*64)).fetchone()[0]
 q=db.execute("INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,status,trusted_import) VALUES(%s,%s,'session.describe','1',%s,'cuda',%s,%s,%s,%s) RETURNING id",(tenant,project,'b'*64,'c'*64,report,status,trusted)).fetchone()[0]
 db.execute('SET CONSTRAINTS ALL IMMEDIATE')
 db.execute('SET CONSTRAINTS ALL DEFERRED')
 return q
def upgrade(db):
 db.execute((ROOT/'backend/db/migrations/01675_v32_qualification_timing.sql').read_text().split('-- +goose Down',1)[0])
@pytest.mark.migration_head(1674)
@pytest.mark.parametrize('trusted,status',[(False,'passed'),(False,'failed'),(True,'failed')])
def test_historical_null_timing_upgrade_preserves_rows(db,trusted,status):
 q=seed(db,trusted,status)
 before=db.execute('SELECT * FROM photo.model_qualifications WHERE id=%s',(q,)).fetchone()
 upgrade(db)
 after=db.execute('SELECT * FROM photo.model_qualifications WHERE id=%s',(q,)).fetchone()
 assert after[:-1]==before and after[-1] is None

@pytest.mark.migration_head(1674)
def test_trusted_passed_unknown_timing_upgrade_control(db):
 q=seed(db,True,'passed')
 before=db.execute('SELECT * FROM photo.model_qualifications WHERE id=%s',(q,)).fetchone()
 upgrade(db)
 after=db.execute('SELECT * FROM photo.model_qualifications WHERE id=%s',(q,)).fetchone()
 assert after[:-1]==before and after[-1] is None

@pytest.mark.parametrize('trusted,status',[(False,'passed'),(False,'failed'),(True,'failed'),(True,'passed')])
def test_current_unknown_timing_preserves_all_historical_statuses(db,trusted,status):
 q=seed(db,trusted,status)
 assert db.execute('SELECT measured_p95_ms FROM photo.model_qualifications WHERE id=%s',(q,)).fetchone()==(None,)

@pytest.mark.migration_head(1674)
def test_existing_old_constraint_forward_repair_and_drift_refusal(db):
 upgrade(db)
 seed(db,True,'passed')
 name=db.execute("SELECT c.conname FROM pg_constraint c WHERE c.conrelid='photo.model_qualifications'::regclass AND c.contype='c' AND (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='measured_p95_ms')=ANY(c.conkey)").fetchone()[0]
 from psycopg import sql
 db.execute(sql.SQL('ALTER TABLE photo.model_qualifications DROP CONSTRAINT {}').format(sql.Identifier(name)))
 db.execute(sql.SQL("ALTER TABLE photo.model_qualifications ADD CONSTRAINT {} CHECK(measured_p95_ms BETWEEN 1 AND 9007199254740991 AND trusted_import AND status='passed')").format(sql.Identifier(name)))
 migration=(ROOT/'backend/db/migrations/02581_qualification_unknown_timing_compatibility.sql').read_text().split('-- +goose Down',1)[0]
 before_rows=db.execute('SELECT to_jsonb(q) FROM photo.model_qualifications q ORDER BY id').fetchall()
 authority=db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.model_qualifications'::regclass").fetchone()
 other=db.execute("SELECT conname,pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='photo.model_qualifications'::regclass AND conname<>%s ORDER BY conname",(name,)).fetchall()
 db.execute(migration)
 assert db.execute('SELECT to_jsonb(q) FROM photo.model_qualifications q ORDER BY id').fetchall()==before_rows
 assert db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.model_qualifications'::regclass").fetchone()==authority
 assert db.execute("SELECT conname,pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='photo.model_qualifications'::regclass AND conname<>%s ORDER BY conname",(name,)).fetchall()==other
 for trusted,status in [(False,'passed'),(False,'failed'),(True,'failed')]: seed(db,trusted,status)
 with db.transaction(): db.execute(migration)
 # Unknown timings do not authorize measured timing for failed/untrusted records.
 for trusted,status in [(False,'passed'),(False,'failed'),(True,'failed')]:
  with pytest.raises(psycopg.errors.CheckViolation):
   with db.transaction():
    db.execute("INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,status,trusted_import,measured_p95_ms) SELECT tenant_id,project_id,operation,operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,%s,%s,20 FROM photo.model_qualifications LIMIT 1",(status,trusted))
 with db.transaction():
  db.execute(sql.SQL('ALTER TABLE photo.model_qualifications DROP CONSTRAINT {}').format(sql.Identifier(name)))
  db.execute(sql.SQL('ALTER TABLE photo.model_qualifications ADD CONSTRAINT {} CHECK(measured_p95_ms IS NULL OR measured_p95_ms>0)').format(sql.Identifier(name)))
  with pytest.raises(psycopg.errors.RaiseException,match='QUALIFICATION_TIMING_CONSTRAINT_SOURCE_SHAPE'):
   with db.transaction(): db.execute(migration)

@pytest.mark.parametrize('timing',[0,-1,9007199254740992])
def test_current_trusted_passed_invalid_measured_timing(db,timing):
 q=seed(db,True,'passed')
 with pytest.raises(psycopg.errors.CheckViolation):
  with db.transaction():
   db.execute("INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,status,trusted_import,measured_p95_ms) SELECT tenant_id,project_id,operation,operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,status,trusted_import,%s FROM photo.model_qualifications WHERE id=%s",(timing,q))
