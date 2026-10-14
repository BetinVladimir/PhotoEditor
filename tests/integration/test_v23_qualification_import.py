"""Trusted qualification import must not be callable by ordinary roles."""
import hashlib
import json
from uuid import uuid4

import pytest
from psycopg import errors


@pytest.mark.parametrize("p95,stored", [(20,20),(20.25,21),(0,None)])
def test_qualification_import_is_maintenance_only_and_operator_fenced(db,p95,stored):
    operator = db.execute("INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id",
                          ('qualification-operator@example.test',)).fetchone()[0]
    tenant = db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) "
                        "VALUES('Qualification',%s,'{}','{}') RETURNING id",
                        (operator,)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) "
               "VALUES(%s,%s,'owner')", (tenant, operator))
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Qualification') RETURNING id",
                         (tenant,)).fetchone()[0]
    model, runtime, device, dataset = ('a'*64, 'b'*64, 'c'*64, 'd'*64)
    fixture, evidence = 'e'*64, 'f'*64
    db.execute("INSERT INTO photo.v2_qualification_operators(operator_id,enabled) "
               "VALUES(%s,true)", (operator,))
    db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,"
               "model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,"
               "issuer_id,required_checks,test_fixture_shas,evidence_shas,"
               "license_cleared,enabled) VALUES('identity.compare','1',%s,%s,'cuda',"
               "%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],true,true)",
               (model, runtime, device, dataset, operator, fixture, evidence))
    report = dict(schema_version=1, operation='identity.compare', version='1',
                  model_lock_sha=model, runtime_lock_sha=runtime,
                  device_profile_sha=device, dataset_sha=dataset,
                  input_fixture_shas=[fixture],
                  checks=[dict(id='quality', passed=True, evidence_sha=evidence)],
                  metrics=dict(p50_ms=min(10,p95), p95_ms=p95, peak_vram_bytes=0,
                               peak_rss_bytes=0, scratch_bytes=0), status='passed',
                  issuer_id=str(operator), created_at='2026-09-01T00:00:00Z')
    body = json.dumps(report, sort_keys=True, separators=(',', ':')).encode()
    sha = hashlib.sha256(body).hexdigest()
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
                          "object_version,sha256,bytes,media_type,state,verified_at) "
                          "VALUES(%s,'test','qualify/report','v1',%s,%s,"
                          "'application/json','ready',now()) RETURNING id",
                          (tenant, sha, len(body))).fetchone()[0]
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')
    statement = "SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)"
    args = (tenant, project, artifact, sha, operator, 'cuda', body.decode())
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute(statement, args)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        qualification = db.execute(statement, args).fetchone()[0]
        replay = db.execute(statement, args).fetchone()[0]
        assert replay == qualification
    db.execute('RESET ROLE')
    row = db.execute("SELECT trusted_import,backend,report_sha,measured_p95_ms FROM "
                     "photo.model_qualifications WHERE id=%s", (qualification,)).fetchone()
    assert row == (True, 'cuda', sha, stored)
    assert db.execute('SELECT count(*) FROM photo.model_qualifications WHERE tenant_id=%s AND report_artifact_id=%s',(tenant,artifact)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND action='v2_qualification.imported' AND resource_id=%s",(tenant,qualification)).fetchone()==(1,)
    # Imported timing remains part of immutable report identity, including NULL.
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('UPDATE photo.model_qualifications SET measured_p95_ms=42 WHERE id=%s',(qualification,))
    assert db.execute('SELECT measured_p95_ms FROM photo.model_qualifications WHERE id=%s',(qualification,)).fetchone()==(stored,)
    db.execute("UPDATE photo.v2_qualification_policies SET admission_enabled=true "
               "WHERE operation='identity.compare' AND operation_version='1'")
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        assert db.execute("SELECT model_lock_sha FROM "
                          "photo.resolve_v2_qualification(%s,%s,'identity.compare','1')",
                          (tenant, project)).fetchone()[0] == model
        assert db.execute("SELECT model_lock_sha FROM "
                          "photo.resolve_v2_qualification(%s,%s,'identity.compare','1')",
                          (uuid4(), project)).fetchone() is None
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.v2_qualification_policies SET runtime_lock_sha=%s "
               "WHERE operation='identity.compare'", ('0'*64,))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        assert db.execute("SELECT model_lock_sha FROM "
                          "photo.resolve_v2_qualification(%s,%s,'identity.compare','1')",
                          (tenant, project)).fetchone() is None
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.v2_qualification_policies SET runtime_lock_sha=%s "
               "WHERE operation='identity.compare'", (runtime,))
    forged = dict(report, status='failed')
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        db.execute(statement, (tenant, project, artifact, sha, operator, 'cuda',
                               json.dumps(forged, sort_keys=True, separators=(',', ':'))))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        db.execute(statement, (tenant, project, artifact, sha, operator,
                               'cpu', body.decode()))
    db.execute('UPDATE photo.v2_qualification_operators SET enabled=false '
               'WHERE operator_id=%s', (operator,))
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        db.execute(statement, (tenant, project, uuid4(), sha, operator, 'cuda', body.decode()))
