"""Own retouch quota replay/refusal atomicity; synthetic SQL pins, no GPU QA."""
from copy import deepcopy
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb

from retouch_preview_admission_fixture import prepared_retouch_preview_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mutation', [
    None, 'ram_floor', 'scratch_floor', 'output_floor', 'pixel_count', 'vram',
    'unsafe_integer', 'boolean', 'fraction', 'negative', 'missing', 'extra_estimate',
    'extra_profile', 'renderer', 'backend', 'model', 'runtime', 'device', 'context',
    'frozen_conflict', 'expired', 'principal', 'bytes', 'files', 'released', 'policy',
    'source_hash',
])
def test_retouch_quota_replay_or_refusal_preserves_frozen_state(database, mutation):
    data = prepared_retouch_preview_fixture(database)
    args = data['args']
    tenant, run = args[0], data['run']
    profile = deepcopy(data['node']['execution_profile'])
    estimate = profile['resource_estimate']
    image = data['scope']['source_body'].decode()
    pixels = estimate['pixel_count']
    if mutation == 'ram_floor':
        estimate['ram_bytes'] = 960*pixels + 805306368 + 256*4194304
    elif mutation == 'scratch_floor':
        estimate['scratch_bytes'] = 64*pixels + 64*(16*min(pixels,4194304)+16384) + 67108864 + 4*16384 + 16777216 + 5*min(pixels,6553600) + 1048576 + 65536 - 1
    elif mutation == 'output_floor':
        estimate['output_bytes'] = 12*pixels + 16384 + 20*min(pixels,6553600) + 4*65536 + 8*1048576 - 1
    elif mutation == 'pixel_count': estimate['pixel_count'] += 1
    elif mutation == 'vram': estimate['vram_bytes'] = 1
    elif mutation == 'unsafe_integer': estimate['ram_bytes'] = 9007199254740992
    elif mutation == 'boolean': estimate['ram_bytes'] = True
    elif mutation == 'fraction': estimate['ram_bytes'] = 1.5
    elif mutation == 'negative': estimate['scratch_bytes'] = -1
    elif mutation == 'missing': del estimate['output_bytes']
    elif mutation == 'extra_estimate': estimate['client_budget'] = 1
    elif mutation == 'extra_profile': profile['client_budget'] = 1
    elif mutation == 'renderer': profile['renderer_version'] = 'v2.1'
    elif mutation == 'backend': profile['backend'] = 'cuda'
    elif mutation in ('model', 'runtime', 'device'):
        key = dict(model='model_lock_sha', runtime='runtime_lock_sha', device='device_profile_sha')[mutation]
        profile[key] = '0'*64
    elif mutation == 'context': profile['retouch_context']['artifact_id'] = str(uuid4())
    elif mutation == 'frozen_conflict': estimate['ram_bytes'] += 1
    elif mutation == 'source_hash': image += ' '
    elif mutation == 'policy':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s', (data['pin']['policy_id'],))
    elif mutation in ('expired', 'principal', 'bytes', 'files', 'released'):
        assignments = dict(expired="expires_at=clock_timestamp()-interval '1 second'",
                           principal='principal_id=%s', bytes='bytes=bytes+1',
                           files='files=1', released="state='released'")
        values = (uuid4(), tenant, run) if mutation == 'principal' else (tenant, run)
        database.execute('UPDATE photo.quota_reservations SET '+assignments[mutation]+
                         " WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", values)

    def snapshot():
        return (
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone(),
            database.execute("SELECT id,state,principal_type,principal_id,share_id,bytes,files,expires_at FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant,run)).fetchall(),
            database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant,run)).fetchone(),
            database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (run,)).fetchone(),
        )

    before = snapshot()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        base = (tenant, args[1], args[2], args[3], data['target'],run,args[5],args[6])
        result = database.execute('SELECT photo.reserve_semantic_retouch_preview_quota('+','.join(['%s']*10)+')',
                                  (*base,image,Jsonb(profile))).fetchone()[0]
        database.execute('RESET ROLE')
    assert result is (mutation is None)
    assert snapshot() == before


@pytest.mark.parametrize('headroom', [-1, 0, 1])
def test_initial_retouch_quota_capacity_boundary_is_atomic(database, headroom):
    data = prepared_retouch_preview_fixture(database, reserve_quota=False)
    args, run = data['args'], data['run']
    tenant = args[0]
    profile = data['node']['execution_profile']
    budget = profile['resource_estimate']['output_bytes']
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()
    assert database.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant,run)).fetchone() == (0,)
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s', (run,)).fetchone() == (None,)
    capacity = usage[0] + usage[1] + budget + headroom
    database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',%s::jsonb) WHERE id=%s", (str(capacity),tenant))
    base = (tenant,args[1],args[2],args[3],data['target'],run,args[5],args[6])
    def reserve():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
            value = database.execute('SELECT photo.reserve_semantic_retouch_preview_quota('+','.join(['%s']*10)+')',
                                     (*base,data['scope']['source_body'].decode(),Jsonb(profile))).fetchone()[0]
            database.execute('RESET ROLE')
            return value
    accepted = headroom >= 0
    assert reserve() is accepted
    expected_usage = (usage[0], usage[1]+budget) if accepted else usage
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == expected_usage
    reservations = database.execute("SELECT bytes,files,state,principal_id FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant,run)).fetchall()
    assert reservations == ([(budget,0,'reserved',args[2])] if accepted else [])
    expected_profile = profile if accepted else None
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s', (run,)).fetchone() == (expected_profile,)
    # Exact successful replay and failed retries both leave accounting unchanged.
    assert reserve() is accepted
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == expected_usage
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (run,)).fetchone() == (0,)
