"""Purpose-bound OTP verifier under the actual nonprivileged auth login."""
import os
from pathlib import Path
import secrets
import shutil
import subprocess
from uuid import uuid4
import psycopg
from psycopg import sql
from psycopg.conninfo import make_conninfo
import pytest
ROOT=Path(__file__).resolve().parents[2]
pytestmark=[pytest.mark.integration,pytest.mark.migration_head(17)]

class RuntimeDSN(str):
    def __repr__(self):return '<isolated auth runtime DSN>'

@pytest.fixture
def recent_auth_db(database):
    role='recent_auth_'+uuid4().hex;password=secrets.token_urlsafe(32)
    with psycopg.connect(**database,autocommit=True) as admin:
        for name in ('02533_otp_verify_encrypted_receipt.sql','02666_billing_legal_schema.sql','02667_billing_legal_auth_tables.sql','02681_billing_recent_auth_challenges.sql','02682_billing_recent_auth_verifier.sql'):
            with admin.transaction():admin.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
        admin.execute(sql.SQL('CREATE ROLE {} LOGIN PASSWORD {}').format(sql.Identifier(role),sql.Literal(password)))
        try:
            admin.execute(sql.SQL('GRANT photo_auth TO {}').format(sql.Identifier(role)))
            runtime=dict(database,user=role,password=password)
            yield database,RuntimeDSN(make_conninfo(**runtime))
        finally:
            admin.execute(sql.SQL('DROP ROLE {}').format(sql.Identifier(role)))

def test_recent_auth_verifier_and_login_regression(recent_auth_db):
    database,runtime=recent_auth_db
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_RUNTIME_DATABASE_URL=str(runtime))
    result=subprocess.run([shutil.which('go') or 'go','test','./internal/auth','-run','^TestIntegrationRecentAuth','-count=1','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationRecentAuthPurposeRecoveryAndOneUse' in result.stdout


def test_recent_auth_actual_http_and_web_csrf(recent_auth_db):
    database,runtime=recent_auth_db
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_RUNTIME_DATABASE_URL=str(runtime))
    result=subprocess.run([shutil.which('go') or 'go','test','./internal/httpapi','-run','^TestIntegrationRecentAuth','-count=1','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationRecentAuthActualHTTP' in result.stdout
