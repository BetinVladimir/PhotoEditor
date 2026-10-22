"""Forward Delivery upgrades preserve function authority and unrelated policies."""
from pathlib import Path

import pytest
import psycopg

ROOT=Path(__file__).resolve().parents[2]
SIGNATURE='photo.v30_delivery_stage_result(uuid,uuid,integer,text)'


def function_catalog(db):
    return db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid=%s::regprocedure',(SIGNATURE,)).fetchone()


def upgrade(db, filename):
    db.execute((ROOT/'backend/db/migrations'/filename).read_text().split('-- +goose Down',1)[0])


@pytest.mark.migration_head(2577)
def test_delivery_upgrade_preserves_existing_authority_and_unrelated_output_policies(db):
    before=function_catalog(db)
    policies=db.execute('SELECT * FROM photo.operation_output_policies ORDER BY operation,version,role').fetchall()
    table_authority=db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.operation_output_policies'::regclass").fetchone()
    assert db.execute("SELECT count(*) FROM photo.operation_output_policies WHERE operation='export.encode'").fetchone()==(0,)
    upgrade(db,'02578_delivery_stage_result_aliases.sql')
    after=function_catalog(db)
    assert after[:5]==before[:5]
    old='SELECT * INTO z FROM photo.task_results WHERE tenant_id=e.tenant_id AND task_id=t.id AND id=t.canonical_result_id;'
    new='SELECT result.* INTO z FROM photo.task_results result WHERE result.tenant_id=e.tenant_id AND result.task_id=t.id AND result.id=t.canonical_result_id;'
    assert before[5].count(old)==1
    assert after[5]==before[5].replace(old,new)
    upgrade(db,'02579_export_encode_output_policies.sql')
    assert function_catalog(db)==after
    assert db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.operation_output_policies'::regclass").fetchone()==table_authority
    assert db.execute("SELECT * FROM photo.operation_output_policies WHERE operation<>'export.encode' ORDER BY operation,version,role").fetchall()==policies
    assert db.execute("SELECT version,role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='export.encode' ORDER BY role").fetchall()==[
        ('1','encoded_image',['image/jpeg','image/png','image/tiff'],1073741824,1),
        ('1','encoded_manifest',['application/json'],1048576,1),
    ]


@pytest.mark.migration_head(2578)
def test_policy_upgrade_refuses_unexpected_preexisting_encode_policy_without_partial_writes(db):
    db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES('export.encode','1','encoded_image','[\"image/jpeg\"]',1024,1)")
    before=db.execute('SELECT * FROM photo.operation_output_policies ORDER BY operation,version,role').fetchall()
    authority=function_catalog(db)
    with pytest.raises(psycopg.errors.RaiseException,match='EXPORT_ENCODE_OUTPUT_POLICY_SOURCE_SHAPE'):
        with db.transaction():
            upgrade(db,'02579_export_encode_output_policies.sql')
    assert db.execute('SELECT * FROM photo.operation_output_policies ORDER BY operation,version,role').fetchall()==before
    assert function_catalog(db)==authority
