"""Real saved review authority + qualification metadata + quota boundary.

No SQL authority doubles. Source/report bytes are synthetic metadata, not physical
quality evidence; transport/activation is outside this fixture. Never auto-enable
operations or execute a worker. Database fixture rolls all changes back.
"""
import hashlib
import json
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture, save
from semantic_qualification_fixture import frozen_qualification

CHECKS = [
    'background_cleanup.real_photo_review','background_cleanup.outside_mask_exact',
    'background_cleanup.protected_pixels_exact','background_cleanup.alpha_preserved',
    'background_cleanup.strength_zero_exact','background_cleanup.working_color_delta',
    'background_cleanup.global_pass','background_cleanup.model_runtime_pins',
    'background_cleanup.cancellation','background_cleanup.resource_admission',
]


def quota_fixture(db, width=100, height=80):
    args, _, scope, _ = saved_authority_fixture(db)
    # Match normal tenant bootstrap without overwriting existing counters.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT (tenant_id) DO NOTHING',
               (args[0],))
    pin = frozen_qualification(db,args[0],scope['project'],scope['actor'],
        operation='semantic.preview_background_cleanup',backend='cpu',required_checks=CHECKS)
    source = dict(dtype='float32',color=dict(profile_id='work_rec2020_gamma24_v1',encoding='gamma24'),
                  canvas=dict(width=width,height=height))
    raw_source = json.dumps(source,separators=(',', ':'))
    source_id = uuid4()
    source_sha = hashlib.sha256(raw_source.encode()).hexdigest()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json','v1',clock_timestamp())",
               (source_id,args[0],str(source_id),source_sha,len(raw_source.encode())))
    body = json.loads(args[5])
    body['source']['source'] = dict(artifact_id=str(source_id),sha256=source_sha)
    for field in ('model_lock_sha','runtime_lock_sha','device_profile_sha'):
        body[field] = pin[field]
    raw = json.dumps(body,sort_keys=True,separators=(',', ':'))
    args = (*args[:5],raw,hashlib.sha256(raw.encode()).hexdigest())
    run = save(db,args)
    pixels = width*height
    display = min(pixels,6553600)
    estimate = dict(ram_bytes=768*pixels+536870912+1,vram_bytes=0,
        scratch_bytes=32*pixels+4*16384+16777216+5*display+1048576+65536,
        output_bytes=12*pixels+16384+20*display+4*65536+8*1048576,pixel_count=pixels)
    profile = dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate,
        model_lock_id=pin['model_lock_sha'],qualification=pin,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    assert db.execute('SELECT photo.v27_downstream_profile_ready(%s,%s,%s,%s,%s,%s)',
        (args[0],scope['project'],'semantic.preview_background_cleanup','1','gpu',Jsonb(profile))).fetchone()[0] is True
    return args,run,raw_source,profile


def reserve(db,args,run,source,profile):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT photo.reserve_semantic_background_cleanup_preview_quota(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (*args[:5],run,args[5],args[6],source,Jsonb(profile))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def test_background_cleanup_quota_reserves_once_and_replays_frozen_profile(database):
    args,run,source,profile = quota_fixture(database)
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    assert reserve(database,args,run,source,profile) is True
    budget = profile['resource_estimate']['output_bytes']
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before+budget
    assert reserve(database,args,run,source,profile) is True
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before+budget
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s',(run,)).fetchone()[0] == profile
    changed = dict(profile,resource_estimate=dict(profile['resource_estimate'],output_bytes=budget+1))
    assert reserve(database,args,run,source,changed) is False
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


@pytest.mark.parametrize('field', ['ram_bytes','scratch_bytes','output_bytes','pixel_count','vram_bytes'])
def test_background_cleanup_quota_rejects_each_resource_mismatch_without_reservation(database,field):
    args,run,source,profile = quota_fixture(database)
    estimate = dict(profile['resource_estimate'])
    estimate[field] = 1 if field == 'vram_bytes' else estimate[field]-1
    changed = dict(profile,resource_estimate=estimate)
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    assert reserve(database,args,run,source,changed) is False
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s',(run,)).fetchone()[0] is None
    assert database.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone()[0] == 0


@pytest.mark.parametrize('mutation', ['source_sha','source_unverified','report_unverified','policy_disabled','expired_reservation'])
def test_background_cleanup_quota_rechecks_live_source_and_qualification(database,mutation):
    args,run,source,profile = quota_fixture(database)
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    if mutation == 'source_sha':
        source += ' '
    elif mutation == 'source_unverified':
        aid = json.loads(args[5])['source']['source']['artifact_id']
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(aid,))
    elif mutation == 'report_unverified':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(profile['qualification']['report']['artifact_id'],))
    elif mutation == 'policy_disabled':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(profile['qualification']['policy_id'],))
    else:
        assert reserve(database,args,run,source,profile) is True
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run))
        before += profile['resource_estimate']['output_bytes']
    assert reserve(database,args,run,source,profile) is False
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def test_background_cleanup_quota_capacity_rejects_without_frozen_profile(database):
    args,run,source,profile = quota_fixture(database)
    budget = profile['resource_estimate']['output_bytes']
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb(%s::bigint)) WHERE id=%s",
                     (usage[0]+usage[1]+budget-1,args[0]))
    assert reserve(database,args,run,source,profile) is False
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone() == usage
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s',(run,)).fetchone()[0] is None


@pytest.mark.parametrize('width,height', [(250000,8),(8,250000)])
def test_background_cleanup_quota_accepts_long_canvas_within_pixel_contract(database,width,height):
    args,run,source,profile = quota_fixture(database,width=width,height=height)
    # Metadata admission only: no large arrays or model inference are allocated.
    database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb(1000000000000::bigint)) WHERE id=%s",(args[0],))
    assert profile['resource_estimate']['pixel_count'] == width*height
    assert reserve(database,args,run,source,profile) is True


@pytest.mark.parametrize('width,height', [(7,80),(80,7),(25000001,8),(200000001,8),(0,80)])
def test_background_cleanup_quota_rejects_canvas_outside_source_contract(database,width,height):
    args,run,source,profile = quota_fixture(database,width=width,height=height)
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    assert reserve(database,args,run,source,profile) is False
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before


@pytest.mark.parametrize('mutation', ['principal_id', 'principal_type'])
def test_background_cleanup_quota_replay_rejects_foreign_principal(database, mutation):
    args,run,source,profile = quota_fixture(database)
    assert reserve(database,args,run,source,profile) is True
    if mutation == 'principal_id':
        database.execute("UPDATE photo.quota_reservations SET principal_id=%s WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (uuid4(),args[0],run))
    else:
        database.execute("UPDATE photo.quota_reservations SET principal_type='share' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (args[0],run))
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    reservation = database.execute("SELECT id,principal_type,principal_id,bytes,files,state,expires_at FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone()
    assert reserve(database,args,run,source,profile) is False
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before
    assert database.execute("SELECT id,principal_type,principal_id,bytes,files,state,expires_at FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone() == reservation
    assert database.execute('SELECT v27_preapply_preview_profile FROM photo.runs WHERE id=%s',(run,)).fetchone()[0] == profile
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def attach_share_scope(db,args,run):
    """Create an actual same-tenant share; retain user principal to isolate share_id guard."""
    project = db.execute('SELECT project_id FROM photo.semantic_intents WHERE tenant_id=%s AND id=%s',args[:2]).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Quota scope fixture','published') RETURNING id",(args[0],project)).fetchone()[0]
    share = db.execute("INSERT INTO photo.share_links(tenant_id,collection_id,secret_hash,permissions,limits,allowed_presets,created_by) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id",
        (args[0],collection,b's'*32,Jsonb(dict(view=True,download=False,upload=False)),Jsonb({}),Jsonb([]),args[2])).fetchone()[0]
    db.execute("UPDATE photo.quota_reservations SET share_id=%s WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(share,args[0],run))
    return share


def test_background_cleanup_quota_replay_rejects_existing_share_scope(database):
    args,run,source,profile = quota_fixture(database)
    assert reserve(database,args,run,source,profile) is True
    share = attach_share_scope(database,args,run)
    before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0]
    assert reserve(database,args,run,source,profile) is False
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == before
    assert database.execute("SELECT principal_type,principal_id,share_id,bytes,state FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone() == ('user',args[2],share,profile['resource_estimate']['output_bytes'],'reserved')
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0
