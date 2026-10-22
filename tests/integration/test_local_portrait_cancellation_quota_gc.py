"""Authored local-operation coverage of the shared V2 terminal accounting path."""
from uuid import UUID
import pytest
from test_local_portrait_result_authority import adopted_local_result


@pytest.mark.parametrize('operation', ['portrait.hair', 'portrait.clothing'])
def test_local_cancelled_adopted_task_releases_reservation_once_and_retains_gc_roots(db, operation):
    item = adopted_local_result(db, operation)
    tenant, run = item['tenant'], item['run']
    # The common fixture starts with its terminal read state. Model the real
    # pre-finalizer boundary: canonical adoption exists, run is cancelling.
    db.execute("UPDATE photo.runs SET status='cancelling',cancel_requested=true WHERE tenant_id=%s AND id=%s", (tenant, run))
    # This relational tenant constructor does not invoke tenant Create, which
    # normally supplies its usage ledger. Provision the real default row before
    # adding a run reservation; never replace or reset an existing ledger.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING', (tenant,))
    usage_before = db.execute('SELECT storage_bytes,reserved_bytes,source_files,reserved_files FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()
    assert usage_before is not None
    amount = 4096
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,'user',%s,'run',%s,%s,0,'reserved',clock_timestamp()+interval '1 hour')", (tenant, item['actor'], run, amount))
    assert db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+%s WHERE tenant_id=%s', (amount, tenant)).rowcount == 1
    reserved = db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    assert reserved == usage_before[1] + amount
    candidate = UUID(item['candidate']['artifact_id'])
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)', (tenant, candidate)).fetchone() == (True,)
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tenant, run)).fetchone() == ('cancelled',)
    assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, run)).fetchone() == ('released', 0)
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == (reserved-amount,)
    revision = db.execute('SELECT revision FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, run)).fetchone()
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tenant, run)).fetchone() == ('cancelled',)
    assert db.execute('SELECT revision FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, run)).fetchone() == revision
    assert db.execute('SELECT storage_bytes,reserved_bytes,source_files,reserved_files FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == usage_before
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)', (tenant, candidate)).fetchone() == (True,)
    assert db.execute('SELECT run_id,workflow_id FROM photo.discover_v28_local_portrait_result(%s,%s,%s,%s)', (tenant, run, item['actor'], item['session'])).fetchall() == [(run, None)]
    assert db.execute('SELECT params_id FROM photo.v28_local_portrait_adoption_params(%s,%s,1,1,%s)', (tenant, item['task'], item['result'])).fetchall() == []
