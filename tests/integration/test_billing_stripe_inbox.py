"""Signed Stripe raw HTTP reaches encrypted durable inbox under maintenance role."""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from test_billing_outbox import outbox
ROOT=Path(__file__).resolve().parents[2]
pytestmark=[pytest.mark.integration,pytest.mark.migration_head(17)]
VERSION='2026-10-28.endive'

@pytest.fixture
def stripe_inbox(outbox):
    c,config=outbox
    for name in ('02679_billing_stripe_inbox_schema.sql','02680_billing_stripe_inbox_functions.sql'):
        with c.transaction():c.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
    account=c.execute("INSERT INTO photo.billing_provider_accounts(external_account_id,api_version,webhook_format) VALUES('acct_fixture',%s,'snapshot') RETURNING id",(VERSION,)).fetchone()[0]
    yield c,config,account

def test_signed_http_durable_inbox_and_conflict_evidence(stripe_inbox):
    import json,os,shutil,subprocess
    from psycopg.conninfo import make_conninfo
    _,database,account=stripe_inbox
    fixture={'account_id':str(account),'external_account':'acct_fixture','api_version':VERSION}
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BILLING_STRIPE_INBOX_FIXTURE=json.dumps(fixture))
    result=subprocess.run([shutil.which('go') or 'go','test','./internal/httpapi','-run','^TestIntegrationSignedStripeDurableInbox$','-count=1','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationSignedStripeDurableInbox' in result.stdout

@pytest.mark.parametrize('role',['photo_api','photo_auth','photo_worker','photo_scheduler'])
def test_only_maintenance_can_ingest(stripe_inbox,role):
    c,_,account=stripe_inbox
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with c.transaction():
            c.execute('SET LOCAL ROLE '+role)
            c.execute("SELECT * FROM photo.billing_ingest_stripe_webhook(%s,%s,'acct_fixture',%s,'evt_fixture','invoice.paid','in_fixture',clock_timestamp(),%s,%s)",(uuid4(),account,VERSION,b'ciphertext-fixture',b'x'*32))

@pytest.mark.parametrize('version,account_override',[(VERSION,True),('wrong-version',False)])
def test_account_and_version_validated_before_record(stripe_inbox,version,account_override):
    c,_,account=stripe_inbox
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_WEBHOOK_INPUT_INCOMPLETE'):
        with c.transaction():
            c.execute('SET LOCAL ROLE photo_maintenance')
            c.execute("SELECT * FROM photo.billing_ingest_stripe_webhook(%s,%s,'acct_fixture',%s,'evt_fixture','invoice.paid','in_fixture',clock_timestamp(),%s,%s)",(uuid4(),uuid4() if account_override else account,version,b'ciphertext-fixture',b'x'*32))
    assert c.execute('SELECT count(*) FROM photo.billing_webhook_inbox').fetchone()[0]==0


def test_runtime_installer_with_dedicated_login(stripe_inbox):
    import json,os,shutil,subprocess
    from psycopg.conninfo import make_conninfo
    _,database,account=stripe_inbox
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BILLING_STRIPE_INBOX_FIXTURE=json.dumps({'account_id':str(account),'external_account':'acct_fixture','api_version':VERSION}))
    result=subprocess.run([shutil.which('go') or 'go','test','./cmd/photo','-run','^TestIntegrationStripeWebhookInstallerLeastPrivilege$','-count=1','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationStripeWebhookInstallerLeastPrivilege' in result.stdout


def test_readiness_requires_exact_account_mapping(stripe_inbox):
    c,_,account=stripe_inbox
    with c.transaction():
        c.execute('SET LOCAL ROLE photo_maintenance')
        assert c.execute('SELECT photo.billing_stripe_ingress_ready(%s,%s,%s)',(account,'acct_fixture',VERSION)).fetchone()[0] is True
        assert c.execute('SELECT photo.billing_stripe_ingress_ready(%s,%s,%s)',(account,'acct_other',VERSION)).fetchone()[0] is False
