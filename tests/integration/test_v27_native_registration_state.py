"""Positive native SQL registration gates; metadata-only, no GPU admission."""
from copy import deepcopy
from uuid import uuid4, uuid5

from psycopg import errors

import pytest
from psycopg.types.json import Jsonb
from test_database import tenant


@pytest.mark.parametrize('operation,stage', [('face.detect', 'detector'), ('mask.person', 'person')])
def test_native_registration_missing_then_exact_replay(db, operation, stage):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tid,))
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Native registration') RETURNING id", (tid,)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,0) RETURNING id", (tid, actor, 'a'*64)).fetchone()[0]
    parent, task = uuid4(), uuid4()
    run = uuid5(parent, 'semantic.' + operation + ':reference')
    refs = []
    for ordinal in range(3):
        digest = str(ordinal+1)*64
        artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,128,'application/json','v1',clock_timestamp()) RETURNING id", (tid, str(uuid4()), digest)).fetchone()[0]
        refs.append(dict(artifact_id=str(artifact), sha256=digest))
    profile = dict(backend='cuda', renderer_version='v2.2', resource_estimate=dict(ram_bytes=1073741824, vram_bytes=1073741824, scratch_bytes=1048576, output_bytes=1048576, pixel_count=64))
    config = dict(project_id=str(project))
    config['v27_'+stage+'_prepare'] = dict(schema_version=1, parent_run_id=str(parent), task_id=str(task), input_digest='d'*64, params=refs[0])
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'fail_fast')", (run,tid,snapshot,Jsonb(config),'b'*64,actor))
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'semantic','v2.2',%s) RETURNING id", (tid,project,run,snapshot,'c'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,%s,'1','gpu','[]',%s,%s,%s)", (task,tid,run,operation+'/0',operation,refs[0]['artifact_id'],'d'*64,Jsonb(profile)))
    rows = sorted([dict(ref=ref, contract=contract) for ref,contract in zip(refs,['Params','WorkingImage','NpyArray'])], key=lambda item:item['ref']['artifact_id'])
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1024,1,clock_timestamp()+interval '1 hour')", (tid,actor,run))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_scheduler')

    def publish(purpose, digest):
        result = db.execute("SELECT * FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,128,'test')", (tid,run,task,purpose,digest)).fetchone()
        assert result[0] == 'created'
        assert db.execute("SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,128,'test','v1')", (tid,run,task,purpose,result[1],result[2],digest)).fetchone()[0]
        return dict(artifact_id=str(result[1]),sha256=digest)

    chunk, manifest = publish('v2_input_chunk_0','e'*64), publish('v2_input_manifest','f'*64)
    query = 'SELECT photo.semantic_'+stage+'_preparation_registration_state(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'

    def state(entries=rows, count=3, requested_tenant=tid, requested_task=task):
        return db.execute(query, (requested_tenant,run,requested_task,refs[0]['artifact_id'],refs[0]['sha256'],'d'*64,Jsonb(profile),Jsonb(entries),Jsonb(manifest),Jsonb([chunk]),Jsonb(refs),count,'9'*64)).fetchone()[0]

    packet = dict(schema_version=1, task_id=str(task), params=refs[0], input_digest='d'*64, profile=profile,
                  transport=dict(schema_version=1, rows=rows, manifest=manifest, chunks=[chunk], direct=refs, count=3, entries_sha='9'*64))
    def binding_state(candidate=packet, requested_operation=operation):
        return db.execute('SELECT photo.semantic_native_registration_binding_state(%s,%s,%s,%s)',
                          (tid,run,requested_operation,Jsonb(candidate))).fetchone()[0]
    source_binding = dict(asset_version_id=str(uuid4()),source=refs[1],canvas_hash='a'*64)
    source_pins = []
    for ref,contract in zip(refs[1:],['WorkingImage','WorkingPixels']):
        source_pins.append(dict(contract=contract,artifact_id=ref['artifact_id'],sha256=ref['sha256'],bucket='test',key=ref['artifact_id'],version='v1',bytes=128,media_type='application/json',parent_id=None if contract=='WorkingImage' else refs[1]['artifact_id'],parent_sha=None if contract=='WorkingImage' else refs[1]['sha256']))
    params_pin = dict(bucket='test',key=refs[0]['artifact_id'],version='v1',sha256=refs[0]['sha256'],bytes=128,media_type='application/json')
    files = [dict(Ref=ref,Contract=contract,Object=dict(Key=ref['artifact_id'],VersionID='v1',SHA256=ref['sha256'],Bytes=128,ContentType='application/json')) for ref,contract in zip(refs,['Params','WorkingImage','WorkingPixels'])]
    def closure_matches(candidate_files=files,candidate_source=source_pins,candidate_packet=packet):
        return db.execute('SELECT photo.semantic_native_file_closure_matches(%s,%s,%s,%s,%s,%s)',tuple(Jsonb(value) for value in [source_binding,candidate_source,refs[0],params_pin,candidate_files,candidate_packet])).fetchone()[0]
    assert closure_matches()
    assert closure_matches(list(reversed(files)))
    for mutation in ['version','bytes','extra','missing','duplicate','source_parent','registration_contract']:
        changed_files,changed_source,changed_packet = deepcopy(files),deepcopy(source_pins),deepcopy(packet)
        if mutation == 'version': changed_files[1]['Object']['VersionID'] = 'v2'
        elif mutation == 'bytes': changed_files[2]['Object']['Bytes'] += 1
        elif mutation == 'extra': changed_files[1]['unexpected'] = True
        elif mutation == 'missing': changed_files.pop()
        elif mutation == 'duplicate': changed_files[2] = changed_files[1]
        elif mutation == 'source_parent': changed_source[1]['parent_id'] = str(uuid4())
        elif mutation == 'registration_contract': changed_packet['transport']['rows'][0]['contract'] = 'FaceBoxes'
        assert not closure_matches(changed_files,changed_source,changed_packet), mutation
    assert binding_state() == 'missing'
    for mutation in ['extra','schema','overflow','null_rows','row_contract','row_duplicate','manifest_extra','chunks_null','direct_duplicate','params_extra','bad_uuid']:
        changed = deepcopy(packet)
        if mutation == 'extra': changed['unexpected'] = True
        elif mutation == 'schema': changed['schema_version'] = 2
        elif mutation == 'overflow': changed['transport']['count'] = 999999999999999999999
        elif mutation == 'null_rows': changed['transport']['rows'] = None
        elif mutation == 'row_contract': changed['transport']['rows'][0]['contract'] = 'FaceBoxes'
        elif mutation == 'row_duplicate': changed['transport']['rows'][1] = changed['transport']['rows'][0]
        elif mutation == 'manifest_extra': changed['transport']['manifest']['unexpected'] = True
        elif mutation == 'chunks_null': changed['transport']['chunks'] = None
        elif mutation == 'direct_duplicate': changed['transport']['direct'][1] = changed['transport']['direct'][0]
        elif mutation == 'params_extra': changed['params']['unexpected'] = True
        elif mutation == 'bad_uuid': changed['task_id'] = 'invalid'
        assert binding_state(changed) == 'conflict', mutation
    assert binding_state(requested_operation='face.parse') == 'conflict'
    other_operation = 'mask.person' if operation == 'face.detect' else 'face.detect'
    assert binding_state(requested_operation=other_operation) == 'conflict'
    assert not db.execute('SELECT photo.lock_semantic_native_admission_anchor(%s,%s,%s)',(tid,run,Jsonb({}))).fetchone()[0]
    assert not db.execute('SELECT photo.lock_semantic_native_admission_evidence(%s,%s,%s)',(tid,run,Jsonb({}))).fetchone()[0]
    assert state() == 'missing'
    assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)', (tid,run,task,Jsonb(rows))).fetchone()[0] == 3
    assert db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,3,%s,%s)', (tid,run,task,Jsonb(manifest),Jsonb([chunk]),'9'*64,Jsonb(refs))).fetchone()[0]
    assert state() == 'replayed'
    assert binding_state() == 'replayed'
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.quota_reservations SET bytes=16777216,files=0 WHERE tenant_id=%s AND holder_id=%s",(tid,run))
    db.execute('UPDATE photo.tenant_usage SET reserved_bytes=16777216 WHERE tenant_id=%s',(tid,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    budget_evidence = dict(operation=operation,actor_id=str(actor),registration=packet,anchor=dict(intent_text='{"target_snapshot_id":"'+str(snapshot)+'"}'))
    def budget(candidate=budget_evidence):
        return db.execute('SELECT photo.lock_semantic_native_graph_budget(%s,%s,%s)',(tid,run,Jsonb(candidate))).fetchone()[0]
    assert budget()
    assert not budget(dict(budget_evidence,operation='face.parse'))
    assert not budget(dict(budget_evidence,actor_id=str(uuid4())))
    db.execute('RESET ROLE')
    for amount,files_count in [(9437183,0),(16777216,1)]:
        db.execute('UPDATE photo.quota_reservations SET bytes=%s,files=%s WHERE tenant_id=%s AND holder_id=%s',(amount,files_count,tid,run))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert not budget()
        db.execute('RESET ROLE')
    db.execute('UPDATE photo.quota_reservations SET bytes=16777216,files=0 WHERE tenant_id=%s AND holder_id=%s',(tid,run))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert budget()
    for mutation in ['run_cancel','task_cancel','expired','usage']:
        db.execute('RESET ROLE')
        if mutation == 'run_cancel': db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
        elif mutation == 'task_cancel': db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
        elif mutation == 'expired': db.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_id=%s",(tid,run))
        elif mutation == 'usage': db.execute('UPDATE photo.tenant_usage SET reserved_bytes=0 WHERE tenant_id=%s',(tid,))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert not budget(), mutation
        db.execute('RESET ROLE')
        db.execute('UPDATE photo.runs SET cancel_requested=false WHERE id=%s',(run,))
        db.execute('UPDATE photo.tasks SET cancel_requested=false WHERE id=%s',(task,))
        db.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '1 hour' WHERE tenant_id=%s AND holder_id=%s",(tid,run))
        db.execute('UPDATE photo.tenant_usage SET reserved_bytes=16777216 WHERE tenant_id=%s',(tid,))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert budget()
    # Restore the original shell reservation used by the replay assertions below.
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.quota_reservations SET bytes=1024,files=1 WHERE tenant_id=%s AND holder_id=%s',(tid,run))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert state(requested_tenant=uuid4()) == 'conflict'
    assert state(requested_task=uuid4()) == 'conflict'
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(uuid4()),))
    assert state() == 'conflict'
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    assert state() == 'replayed'
    assert state(rows[:-1],2) == 'conflict'
    changed = [dict(item) for item in rows]
    changed[0] = dict(ref=changed[0]['ref'],contract='FaceBoxes')
    assert state(changed) == 'conflict'
    # The envelope is usable only by the scheduler, including before SQL casts.
    for role in ['photo_api','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance','photo_semantic_admission']:
        db.execute('RESET ROLE')
        db.execute('SET LOCAL ROLE ' + role)
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                binding_state()
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                closure_matches()
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                budget()
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SELECT photo.lock_semantic_native_admission_anchor(%s,%s,%s)',(tid,run,Jsonb({})))
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SELECT photo.lock_semantic_native_admission_evidence(%s,%s,%s)',(tid,run,Jsonb({})))
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SELECT photo.lock_semantic_native_source_authority(%s,%s,%s,%s,%s)',(tid,project,actor,uuid4(),Jsonb({})))
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert binding_state() == 'replayed'
    db.execute('RESET ROLE')
    assert db.execute('SELECT graph_sealed,status FROM photo.runs WHERE id=%s',(run,)).fetchone() == (False,'draft')

    draft = dict(id=str(task),node_key=operation+'/0',operation=operation,operation_version='1',resource_class='gpu',params_artifact_id=refs[0]['artifact_id'],params_sha=refs[0]['sha256'],input_digest='d'*64,execution_profile=profile)
    def shell_matches(candidate=draft, candidate_config=config, amount=1024):
        return db.execute('SELECT photo.v2_pending_shell_matches(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,run,workflow,project,snapshot,actor,'semantic','c'*64,'b'*64,Jsonb(candidate_config),Jsonb([candidate]),Jsonb([]),amount)).fetchone()[0]
    assert shell_matches()
    for field, value in [('input_digest','8'*64),('params_sha','8'*64),('operation','face.parse'),('resource_class','cpu')]:
        changed = dict(draft)
        changed[field] = value
        assert not shell_matches(changed)
    changed_profile = dict(profile, backend='cpu')
    assert not shell_matches(dict(draft, execution_profile=changed_profile))
    assert not shell_matches(amount=1023)
    foreign_config = dict(config)
    foreign_config['v27_'+stage+'_prepare'] = dict(config['v27_'+stage+'_prepare'], parent_run_id=str(uuid4()))
    assert not shell_matches(candidate_config=foreign_config)
    # State readers never repair bad requests or alter the registered rows.
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s',(tid,task)).fetchone()[0] == 5
    stored_inputs, stored_profile = db.execute('SELECT inputs,execution_profile FROM photo.tasks WHERE tenant_id=%s AND id=%s',(tid,task)).fetchone()
    assert stored_inputs == [manifest] + refs
    assert stored_profile['v2_entry_count'] == 3
    assert stored_profile['v2_entries_sha'] == '9'*64
    assert stored_profile['v2_input_manifest'] == manifest
    assert db.execute('SELECT graph_sealed,status FROM photo.runs WHERE id=%s',(run,)).fetchone() == (False,'draft')
