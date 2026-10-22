"""Actual Go Use/SQL/post-wait replay schedule in a disposable private DB.

Authored scenarios only; execution is deferred by the coding-first plan.
"""
from physical_go_child_evidence import run_evidenced_go
import json
import os
from pathlib import Path
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
import pytest
from semantic_close_fixture import close_fixture


@pytest.mark.parametrize('mode',['success','revoked','competing','changed_request','rollback','rollback_competing'])
def test_close_same_key_wait_reauthorizes_after_real_receipt_contention(database,tmp_path,mode):
    with psycopg.connect(**database) as seed:
        f=close_fixture(seed)
        tenant,intent,actor,session=f['args'][:4]
        wire=dict(Tenant=str(tenant),Intent=str(intent),Actor=str(actor),Session=str(session),
            Parent=str(f['parent']),Artifact=str(f['artifact']),Revision=f['revision'],
            Key=f['key'],Request=f['request'],Body=f['body'],Documents=f['documents'])
    fixture_path=tmp_path/'close-same-key.json'
    fixture_path.write_text(json.dumps(wire))
    root=Path(__file__).resolve().parents[2]
    environment=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
        PHOTO_TEST_DISPOSABLE='1',PHOTO_TEST_CLOSE_FIXTURE=str(fixture_path),PHOTO_TEST_CLOSE_MODE=mode)
    result=run_evidenced_go(['go','test','./internal/semanticreview','-run','^TestIntegrationCloseSameKeyWait$',
        '-count=1','-timeout=45s','-v'],cwd=root/'backend',env=environment,capture_output=True,text=True,timeout=75)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'V27_CLOSE_SAME_KEY_WAIT_OK' in result.stdout
    assert '--- PASS: TestIntegrationCloseSameKeyWait' in result.stdout
