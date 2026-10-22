"""Work-only relational Apply-v4 detail producer, not real image/model execution.

Construct before inserting the correction event. All IDs refer to inserted rows;
no triggers/constraints are disabled. Bind through the real API after event INSERT
in the same transaction. The linked training builder uses this controlled receipt graph.
"""
from hashlib import sha256
from uuid import UUID, uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.resolved import ResolvedStyle,ResolveParams,ResolvedModules
from photo_worker.contracts.portrait_detail import PortraitParams,PortraitDetailReport
from photo_worker.contracts.style import Disabled,PortraitDetail


def detail_producer(db, tenant, project, actor, version, collection, item, material):
    asset, source, source_sha = db.execute(
        'SELECT asset_id,master_artifact_id,content_hash FROM photo.asset_versions '
        'WHERE tenant_id=%s AND id=%s', (tenant, version)).fetchone()
    snapshot, run = uuid4(), uuid4()
    revision=db.execute('SELECT revision FROM photo.collections WHERE tenant_id=%s AND id=%s AND project_id=%s',
        (tenant,collection,project)).fetchone()[0]
    position=db.execute('SELECT position FROM photo.collection_items WHERE tenant_id=%s AND id=%s AND collection_id=%s',
        (tenant,item,collection)).fetchone()[0]
    selection=[dict(item_id=str(item),asset_id=str(asset),asset_version_id=str(version),artifact_id=str(source),sha256=source_sha,position=position)]
    snapshot_digest=sha256(canonical_bytes(selection)).hexdigest()
    db.execute("INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,"
               "principal_type,principal_id,digest,item_count) VALUES(%s,%s,'selection',%s,%s,'user',%s,%s,1)",
               (snapshot, tenant, collection, revision, actor, snapshot_digest))
    db.execute('INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,'
               'asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,%s,%s)',
               (tenant, snapshot, item, version, source, source_sha, Jsonb(dict(position=position,asset_id=str(asset)))))
    config = dict(schema_version='1',pipeline='apply',pipeline_version=4,renderer_version='photo_renderer_v2',
        source_collection_id=str(collection),source_revision=revision,input_snapshot_id=str(snapshot),
        input_snapshot_digest=snapshot_digest,failure_policy='continue_items',portrait_detail_enabled=True)

    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,"
               "config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'continue_items',false)",
               (run, tenant, snapshot, Jsonb(config), sha256(canonical_bytes(config)).hexdigest(), actor))
    db.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,item_count,"
               "phase,graph_version) VALUES(%s,%s,%s,1,'portrait_detail',4)",
               (tenant, run, collection))
    db.execute("INSERT INTO photo.apply_v4_stages(tenant_id,run_id,stage_index,phase,operation,"
               "output_role,cardinality,plan_hash) VALUES(%s,%s,0,'portrait_detail','portrait.detail',"
               "'portrait_detail_report','item',%s)", (tenant, run, sha256(canonical_bytes(dict(phase='portrait_detail',operation='portrait.detail',output_role='portrait_detail_report',cardinality='item'))).hexdigest()))

    def artifact(role, *, producer=None, body=None, media='application/json'):
        raw = canonical_bytes(body or {'schema_version': 1, 'fixture_role': role})
        aid, digest = uuid4(), sha256(raw).hexdigest()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,"
                   "sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,"
                   "produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,"
                   "'ready',%s,clock_timestamp(),%s,%s,%s)",
                   (aid, tenant, str(aid), digest, len(raw), media, role, producer,
                    1 if producer else None, 1 if producer else None))
        return {'artifact_id': str(aid), 'sha256': digest}, raw

    def stage(phase, operation, role, inputs, params_body, prerequisites=(),output_body=None):
        task, result, worker = uuid4(), uuid4(), uuid4()
        params, raw = artifact('task_params', body=params_body)
        db.execute('INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,'
                   'source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,'
                   'params_sha256,params) VALUES(%s,%s,%s,%s,0,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                   (task, tenant, run, phase, item, asset, version, operation+':0', operation,
                    Jsonb(inputs), list(prerequisites), params['sha256'], Jsonb(params_body)))
        # Same classical Apply profile shape as workflow.applyProfile; no V2 root profile.
        allocated=4*12+2*1048576
        profile = dict(backend='cpu',precision='float32',tile_size=512,
            resource_estimate=dict(ram_bytes=3<<30,scratch_bytes=len(raw)+allocated,output_bytes=allocated,pixel_count=4))

        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,"
                   "resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,"
                   "attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1','cpu','blocked',%s,%s,%s,%s,1,1)",
                   (task, tenant, run, operation+':0', operation, Jsonb(inputs), params['artifact_id'],
                    sha256(canonical_bytes(inputs)).hexdigest(), Jsonb(profile)))
        attempt = db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,"
                             "worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,"
                             "clock_timestamp(),clock_timestamp(),'cpu',%s) RETURNING id",
                             (tenant, task, worker, Jsonb(profile))).fetchone()[0]
        output, output_raw = artifact(role, producer=task,body=output_body)
        # Controlled finished gateway receipt metadata, never a physical upload claim.
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,"
                   "idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) "
                   "VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,'application/json','ready','v1',clock_timestamp())",
                   (tenant,task,worker,sha256(b'controlled fixture certificate').hexdigest(),uuid4(),output['artifact_id'],role,
                    output['artifact_id'],output['sha256'],len(output_raw)))

        db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,"
                   "metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)",
                   (result, tenant, task, Jsonb([output]), sha256(canonical_bytes([output])).hexdigest()))
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",
                   (result, task))
        db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,"
                   "token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,"
                   "'task_params',%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp())",
                   (tenant, run, task, params['artifact_id'], uuid4(), params['sha256'], len(raw)))
        return task, result, attempt, output, params

    source_ref = {'artifact_id': str(source), 'sha256': source_sha}
    image_material=material.images[str(source)]
    image_body=image_material['image'].model_dump(mode='json')
    image = stage('local', 'image.local', 'working_image', [source_ref], {'image':source_ref},output_body=image_body)
    style=material.style
    extensions=style.extensions.model_copy(update={'portrait_detail':PortraitDetail(enabled=True,teeth_strength=0.,shine_strength=0.,under_eye_strength=0.,glasses_policy="review")})
    resolved_body=ResolvedStyle.create(params=ResolveParams(style=ArtifactRef.model_validate_json(canonical_bytes(material.style_ref)),image=ArtifactRef.model_validate_json(canonical_bytes(image[3]))),mode='fixed',fixed=style.global_,absolute_lut=None,local=Disabled(),strength=1.,modules=ResolvedModules(beauty=style.beauty,defects=style.defects,geometry=style.geometry,extensions=extensions,models=style.models),neighbors=[],distance=None,uncovered_fraction=None,reasons=[],needs_review=False)
    resolved = stage('resolve','style.resolve','resolved_style',[image[3],material.style_ref],resolved_body.params.model_dump(mode='json'),output_body=resolved_body.model_dump(mode='json'))
    import numpy as np
    mask=material.array(np.ones((2,2),dtype=np.float32))
    mask_body=MaskRef(artifact=ArtifactRef.model_validate_json(canonical_bytes(mask)),canvas=image_material['image'].canvas,transform_chain=image_material['image'].transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=False)
    params_body=PortraitParams(image=ArtifactRef.model_validate_json(canonical_bytes(image[3])),resolved=ArtifactRef.model_validate_json(canonical_bytes(resolved[3])),regions=[dict(region_id='teeth-1',kind='teeth',mask=mask_body,confirmed=False,strength=0.)],protected=[])
    params=params_body.model_dump(mode='json')
    report=PortraitDetailReport(image=params_body.image,resolved=params_body.resolved,regions=[dict(region_id='teeth-1',kind='teeth',applied=False,changed_pixels=0,max_ev=0.,chroma_reduction=0.,reason='zero_strength')],alpha_preserved=True,warnings=[])
    detail = stage('portrait_detail','portrait.detail','portrait_detail_report',[image[3],resolved[3],mask],params,[image[0],resolved[0]],output_body=report.model_dump(mode='json'))
    return {'run': run, 'task': detail[0], 'result': detail[1], 'attempt': detail[2],
            'image': image[3], 'resolved': resolved[3], 'params': detail[4], 'mask': mask,
            'source': source_ref, 'asset_version': version}


def bind_current_event(db, tenant, project, actor, session, event, producer):
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute('SELECT photo.bind_v23_portrait_detail_event('+','.join(['%s']*9)+')',
                      (tenant, project, actor, session, event, producer['run'], producer['task'],
                       producer['result'], producer['attempt'])).fetchone() == (True,)
    db.execute('RESET ROLE')


def freeze_linked_detail(db, tenant, project, actor, session, event, producer, protection):
    """Build typed body only from the production selected event-linked source."""
    from photo_worker.operations.learning.detail_evidence import FrozenPortraitDetailHoldoutEvidence
    db.execute('SET LOCAL ROLE photo_scheduler')
    row = db.execute('SELECT * FROM photo.v23_portrait_detail_event_link_source('
                     + ','.join(['%s']*5)+')', (tenant, project, actor, session, event)).fetchone()
    assert row is not None
    # Return ABI is pinned by01990: version/source/consent + exact producer refs.
    assert row[0] == producer['asset_version']
    assert tuple(map(str, row[5:9])) == tuple(map(str, (producer['run'], producer['task'],
                                                       producer['result'], producer['attempt'])))
    assert row[9:11] == (UUID(producer['image']['artifact_id']), producer['image']['sha256'])
    assert row[11:13] == (UUID(producer['params']['artifact_id']), producer['params']['sha256'])
    assert row[13:15] == (UUID(producer['resolved']['artifact_id']), producer['resolved']['sha256'])
    assert db.execute('SELECT * FROM photo.v23_portrait_detail_mask_artifact('
                      + ','.join(['%s']*7)+')',
                      (tenant, project, actor, session, event, producer['mask']['artifact_id'],
                       producer['mask']['sha256'])).fetchone() is not None
    db.execute('RESET ROLE')
    body = dict(schema_version=1, tenant_id=str(tenant), event_id=str(event),
                asset_version_id=str(row[0]), run_id=str(row[5]), task_id=str(row[6]),
                result_id=str(row[7]), attempt_id=str(row[8]),
                source=dict(artifact_id=str(row[1]), sha256=row[2]),
                image=dict(artifact_id=str(row[9]), sha256=row[10]),
                params=dict(artifact_id=str(row[11]), sha256=row[12]),
                resolved=dict(artifact_id=str(row[13]), sha256=row[14]),
                protection=protection, mask_artifacts=[producer['mask']])
    raw = canonical_bytes(body)
    FrozenPortraitDetailHoldoutEvidence.model_validate_json(raw)
    return raw
