"""Authored upgrade scenario for 02534; never uses a production DSN."""
import os
from pathlib import Path
from uuid import uuid4

import pytest


def test_02534_preserves_acceptance_and_reopens_ambiguous_group():
    psycopg = pytest.importorskip("psycopg")
    dsn = os.environ.get("PHOTO_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated PHOTO_TEST_DATABASE_URL required")
    repo = Path(__file__).resolve().parents[2]
    migration = (repo / "backend/db/migrations/02534_export_admission_failure_recovery.sql").read_text().split("-- +goose Down")[0]
    with psycopg.connect(dsn) as connection:
        try:
            with connection.cursor() as cursor:
                # Simulate the pre-upgrade constraint state inside a rollback-only
                # isolated fixture, retaining the current table/ACL definitions.
                cursor.execute("ALTER TABLE photo.export_delivery_tickets DROP CONSTRAINT IF EXISTS export_delivery_admission_requires_ticket")
                cursor.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",))
                actor = cursor.fetchone()[0]
                cursor.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s", (actor,))
                tenant = cursor.fetchone()[0]
                project, batch = uuid4(), uuid4()
                cursor.execute("INSERT INTO photo.projects(tenant_id,id,name) VALUES(%s,%s,'receipt upgrade')", (tenant, project))
                cursor.execute("INSERT INTO photo.export_delivery_batches(tenant_id,id,actor_id,project_id,request_key,request_sha,plan) VALUES(%s,%s,%s,%s,%s,%s,'[{}]'::jsonb)", (tenant,batch,actor,project,str(uuid4()),"a"*64))
                cursor.execute("INSERT INTO photo.export_delivery_tickets(tenant_id,batch_id,group_key,failure_code) VALUES(%s,%s,'ambiguous','NOT_FOUND')", (tenant,batch))
                cursor.execute("INSERT INTO photo.export_delivery_tickets(tenant_id,batch_id,group_key,ticket) VALUES(%s,%s,'accepted','{\"state\":\"queued\"}'::jsonb)", (tenant,batch))
                cursor.execute(migration)
                cursor.execute("SELECT group_key,ticket,failure_code FROM photo.export_delivery_tickets WHERE tenant_id=%s AND batch_id=%s", (tenant,batch))
                assert cursor.fetchall() == [("accepted", {"state":"queued"}, None)]
                cursor.execute("SELECT details->>'previous_failure_code' FROM photo.audit_log WHERE tenant_id=%s AND resource_id=%s AND action='export.admission_refusal_retracted'", (tenant,batch))
                assert cursor.fetchall() == [("NOT_FOUND",)]
                cursor.execute("SELECT request_sha,plan FROM photo.export_delivery_batches WHERE tenant_id=%s AND id=%s", (tenant,batch))
                assert cursor.fetchone() == ("a"*64,[{}])
                with pytest.raises(psycopg.errors.CheckViolation):
                    with connection.transaction():
                        cursor.execute("INSERT INTO photo.export_delivery_tickets(tenant_id,batch_id,group_key,failure_code) VALUES(%s,%s,'old-binary','VALIDATION_FAILED')", (tenant,batch))
        finally:
            connection.rollback()
