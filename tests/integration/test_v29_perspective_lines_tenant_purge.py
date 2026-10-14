"""V29 line evidence metadata purge; object-store cleanup is tested separately."""
import json
from uuid import uuid4
import pytest

from layer_original_fixture import seed_original_composition


def _line_metadata(db):
    case=seed_original_composition(db)
    tenant=case['Tenant'];base=json.loads(case['Params'])['base'];line=uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,2,'application/json','v1',clock_timestamp(),'document')",
        (line,tenant,f'tenants/{tenant}/perspective-lines/{line}','a'*64))
    db.execute("INSERT INTO photo.v29_perspective_lines(tenant_id,project_id,snapshot_id,asset_version_id,source_id,source_sha,canvas_hash,artifact_id,sha256) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)",
        (tenant,case['Project'],case['Snapshot'],base['asset_version_id'],
         base['source']['artifact_id'],base['source']['sha256'],base['canvas_hash'],line,'a'*64))
    return case,line


def test_perspective_line_purge_is_owner_only_and_explicitly_allowlisted(db):
    definition=db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert "'v29_perspective_lines'" in definition
    assert definition.index("'v29_perspective_lines'")<definition.index("'projects'")
    assert db.execute("SELECT has_table_privilege('photo_fn_owner','photo.v29_perspective_lines','DELETE')").fetchone()[0]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
        assert not db.execute('SELECT has_table_privilege(%s,%s,%s)',
            (role,'photo.v29_perspective_lines','DELETE')).fetchone()[0]


def test_perspective_line_metadata_survives_bad_token_and_only_target_tenant_is_purged(db):
    case,line=_line_metadata(db);neighbor,neighbor_line=_line_metadata(db)
    tenant=case['Tenant']
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tenant,))
    job=db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s) RETURNING id",
        (tenant,tenant,case['Actor'])).fetchone()[0]
    claimed=db.execute('SELECT job_id,cleanup_token FROM photo.claim_tenant_deletion()').fetchone()
    assert claimed and claimed[0]==job
    assert not db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,uuid4())).fetchone()[0]
    assert db.execute('SELECT 1 FROM photo.v29_perspective_lines WHERE tenant_id=%s AND artifact_id=%s',
        (tenant,line)).fetchone()
    assert db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,claimed[1])).fetchone()[0]
    assert not db.execute('SELECT 1 FROM photo.v29_perspective_lines WHERE tenant_id=%s',(tenant,)).fetchone()
    assert db.execute('SELECT 1 FROM photo.v29_perspective_lines WHERE tenant_id=%s AND artifact_id=%s',
        (neighbor['Tenant'],neighbor_line)).fetchone()


def test_perspective_line_reference_blocks_gc_until_catalogue_row_is_removed(db):
    case,line=_line_metadata(db);tenant=case['Tenant']
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(tenant,line)).fetchone()[0]
    assert not db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(uuid4(),line)).fetchone()[0]
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=%s AND id=%s",
        (tenant,line))
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_maintenance')
        assert db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchone() is None
    db.execute('DELETE FROM photo.v29_perspective_lines WHERE tenant_id=%s AND artifact_id=%s',
        (tenant,line))
    assert not db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(tenant,line)).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_maintenance')
        assert db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchone()[0]==line


def test_perspective_line_binding_refuses_expired_artifact(db):
    import psycopg
    case,line=_line_metadata(db);tenant=case['Tenant']
    row=db.execute('SELECT project_id,snapshot_id,asset_version_id,source_id,source_sha,canvas_hash,sha256 FROM photo.v29_perspective_lines WHERE tenant_id=%s AND artifact_id=%s',
        (tenant,line)).fetchone()
    db.execute('DELETE FROM photo.v29_perspective_lines WHERE tenant_id=%s AND artifact_id=%s',(tenant,line))
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=%s AND id=%s",
        (tenant,line))
    with db.transaction(force_rollback=True):
        with pytest.raises(psycopg.errors.CheckViolation):
            db.execute('INSERT INTO photo.v29_perspective_lines(tenant_id,project_id,snapshot_id,asset_version_id,source_id,source_sha,canvas_hash,artifact_id,sha256) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (tenant,*row[:6],line,row[6]))
