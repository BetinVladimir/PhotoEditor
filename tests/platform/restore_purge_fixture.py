"""Canonical report-only purge observation fixture; no live deletion claim."""
from hashlib import sha256
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes


def purge_probe_observations(replay):
    tenant=str(uuid4());job=str(uuid4());content=b'restore fixture'
    buckets=['fixture-objects'];digest=sha256(content).hexdigest()
    references=sorted([{'bucket':buckets[0], 'key':prefix+'tenants/'+tenant+'/item',
                 'version':'v1','sha256':digest,'size':len(content)}
                for prefix in ('','staging/','originals/')],key=lambda ref:(ref['bucket'],ref['key'],ref['version']))
    inventory=sha256(b'photoeditor.restore-object-inventory/v1\n')
    for ref in references:
        inventory.update(canonical_bytes(ref));inventory.update(b'\n')
    fixture={'schema_version':'restore-purge-fixture/v1','restore_run_id':str(replay.run_id),
        'database':replay.database,'build_sha256':replay.build_sha256,
        'recovery_sha256':replay.recovery_sha256,'replay_sha256':replay.replay_sha256,
        'tenant_id':tenant,'revision':1,'buckets':buckets,'references':references,
        'api_origin':'https://fixture.invalid','delete_origin':'https://fixture.invalid',
        'api_path_sha256':'a'*64,'api_control_path_sha256':'b'*64,
        'api_sha256':'c'*64,'api_control_sha256':'d'*64,
        'api_denial':[403,'FORBIDDEN'],'s3_origin':'https://fixture.invalid',
        's3_bucket':buckets[0],'s3_key':references[0]['key'],'s3_version':'v1',
        's3_control_key':'control','s3_control_version':'v1','s3_control_sha256':'e'*64}
    observations={'fixture':fixture,
        'baseline':{'schema_version':'restore-purge-baseline/v1','tenant_id':tenant,
            'inventory':{'schema_version':'restore-object-inventory/v1','objects':3,
                'bytes_verified':3*len(content),'inventory_sha256':inventory.hexdigest()},
            'api':{'schema_version':'restore-access-readable/v1','response_sha256':'c'*64,
                'observed_at':'2026-10-12T00:00:00+00:00'},
            's3':{'schema_version':'restore-s3-readable/v1','tenant_id':tenant,
                'bucket':buckets[0],'key':references[0]['key'],'version':'v1',
                'sha256':digest,'size':len(content),'observed_at':'2026-10-12T00:00:00+00:00'}},
        'completion':{'database':replay.database,'tenant_id':tenant,
            'deletion_job_id':job,'polls':1},
        'purge':{'schema_version':'restore-purge-observation/v1',
            'metadata':{'schema_version':'restore-purge-metadata/v1','database':replay.database,
                'tenant_id':tenant,'deletion_job_id':job,
                'checked_tables':['artifacts','consent_grants','outbox','tasks']},
            'storage':{'schema_version':'restore-purge-storage/v1','tenant_id':tenant,
                'buckets':buckets,'prefixes':[prefix+'tenants/'+tenant+'/'
                    for prefix in ('','staging/','originals/')],'passes':2},
            'api_access':{'schema_version':'restore-access-denial/v1','status':403,
                'code':'FORBIDDEN','response_sha256':'f'*64,
                'observed_at':'2026-10-12T00:00:00+00:00'},
            's3_access':{'schema_version':'restore-s3-denial/v1','tenant_id':tenant,
                'bucket':buckets[0],'key':references[0]['key'],'version':'v1',
                'code':'NoSuchVersion','response_sha256':'1'*64,
                'observed_at':'2026-10-12T00:00:00+00:00'}}}
    return sha256(canonical_bytes(fixture)).hexdigest(),observations
