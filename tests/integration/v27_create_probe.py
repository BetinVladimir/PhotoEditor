"""Independent physical verification of the committed public create probe."""
from hashlib import sha256
import json
from uuid import uuid5


def verify_created_transfer(db, client, bucket, case, fixture, before):
    tenant = case['Tenant']
    runs = db.execute("SELECT r.id,r.status,r.graph_sealed,t.id,t.operation,t.state FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE w.tenant_id=%s AND w.workflow_type='semantic'", (tenant,)).fetchall()
    measured_person = fixture.get('CreatePerson', False)
    assert len(runs) == 1 + int(measured_person), [(str(row[0]), row[4]) for row in runs]
    parents = [r for r in runs if r[4] == 'semantic.transfer']
    assert len(parents) == 1
    run, status, sealed, task, operation, state = parents[0]
    assert (status, sealed, operation, state) == ('queued', True, 'semantic.transfer', 'ready')
    transports = db.execute("SELECT b.purpose,a.bytes,a.object_key,a.object_version,a.sha256,a.id FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id WHERE b.tenant_id=%s AND b.run_id=%s AND b.state='ready' ORDER BY b.purpose", (tenant, run)).fetchall()
    assert len(transports) == 2 and [r[0] for r in transports] == ['v2_input_chunk_0', 'v2_input_manifest']

    def read(key, version, size, digest):
        assert version not in ('', 'null', None)
        response = client.get_object(bucket, key, version_id=version)
        try:
            payload = response.read()
        finally:
            response.close()
            response.release_conn()
        assert len(payload) == size and sha256(payload).hexdigest() == digest
        return payload

    bodies = {purpose: json.loads(read(key, version, size, digest)) for purpose, size, key, version, digest, aid in transports}
    manifest, chunk = bodies['v2_input_manifest'], bodies['v2_input_chunk_0']
    assert manifest['entry_count'] == len(chunk['entries']) > 0
    assert manifest['chunks'] == [dict(artifact_id=str(transports[0][5]), sha256=transports[0][4])]
    assert manifest['entries_sha'] == sha256(json.dumps(chunk['entries'], sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    entries = {(e['ref']['artifact_id'], e['ref']['sha256'], e['contract']) for e in chunk['entries']}
    assert (fixture['RequiredProtection']['artifact_id'], fixture['RequiredProtection']['sha256'], 'MaskRef') in entries
    registered = db.execute("SELECT artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s", (tenant, task)).fetchall()
    assert {(str(aid), digest, contract) for aid, digest, contract in registered if contract not in ('InputClosureChunk', 'InputClosureManifest')} == entries
    assert {(str(aid), digest, contract) for aid, digest, contract in registered if contract in ('InputClosureChunk', 'InputClosureManifest')} == {(str(transports[0][5]), transports[0][4], 'InputClosureChunk'), (str(transports[1][5]), transports[1][4], 'InputClosureManifest')}
    for aid, digest, contract in entries:
        row = db.execute("SELECT object_key,object_version,bytes,sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND state='ready' AND verified_at IS NOT NULL", (tenant, aid)).fetchone()
        assert row and row[3] == digest
        read(*row)
    documents = db.execute("SELECT a.object_key,a.object_version,a.bytes,a.sha256 FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.bucket=%s AND u.state='ready' AND u.upload_contract='Document' AND u.artifact_id<>%s", (tenant, bucket, fixture['Intent']['reference_mask']['artifact_id'])).fetchall()
    assert len(documents) == 3 + int(measured_person), [(row[2], row[3]) for row in documents]
    for row in documents:
        json.loads(read(*row))
    person_bytes = 0
    if measured_person:
        children = [r for r in runs if r[4] == 'mask.person']
        assert len(children) == 1
        child, child_status, child_sealed, child_task, _, child_state = children[0]
        assert child == uuid5(run, 'semantic.mask.person:reference')
        assert (child_status, child_sealed, child_state) == ('queued', True, 'ready')
        child_config = db.execute('SELECT config_snapshot FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone()[0]['v27_person_prepare']
        assert child_config['parent_run_id'] == str(run)
        assert child_config['recovery']['request_key'] == 'proposal-publication'
        assert child_config['recovery']['qualified_costs']['VRAMBytes'] == 128 << 20
        assert child_config['recovery']['continuation'] == 'ordinary_parent'
        policy = child_config['recovery']['ordinary_policy']
        assert policy['project_id'] == str(case['Project']) and policy['reserved_bytes'] == 268435456
        assert policy['expected']['operation'] == 'semantic.transfer'
        assert policy['person']['report'] == fixture['PersonQualification']['report']
        assert db.execute('SELECT v27_parent_recovery_enabled,v27_parent_recovery_done,v27_parent_recovery_token,v27_parent_recovery_until,v27_parent_recovery_after FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == (True, True, None, None, None)
        child_transports = db.execute("SELECT b.purpose,a.bytes,a.object_key,a.object_version,a.sha256,a.id FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id WHERE b.tenant_id=%s AND b.run_id=%s AND b.state='ready' ORDER BY b.purpose", (tenant, child)).fetchall()
        assert len(child_transports) == 2
        child_bodies = {purpose: json.loads(read(key, version, size, digest)) for purpose, size, key, version, digest, aid in child_transports}
        cm, cc = child_bodies['v2_input_manifest'], child_bodies['v2_input_chunk_0']
        assert cm['entry_count'] == len(cc['entries']) > 0
        assert cm['chunks'] == [dict(artifact_id=str(child_transports[0][5]), sha256=child_transports[0][4])]
        assert cm['entries_sha'] == sha256(json.dumps(cc['entries'], sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        child_entries = {(e['ref']['artifact_id'], e['ref']['sha256'], e['contract']) for e in cc['entries']}
        child_registered = db.execute('SELECT artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (tenant, child_task)).fetchall()
        assert {(str(aid), digest, contract) for aid, digest, contract in child_registered if contract not in ('InputClosureChunk', 'InputClosureManifest')} == child_entries
        assert {(str(aid), digest, contract) for aid, digest, contract in child_registered if contract in ('InputClosureChunk', 'InputClosureManifest')} == {(str(child_transports[0][5]), child_transports[0][4], 'InputClosureChunk'), (str(child_transports[1][5]), child_transports[1][4], 'InputClosureManifest')}
        for aid, digest, contract in child_entries:
            row = db.execute("SELECT object_key,object_version,bytes,sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND state='ready' AND verified_at IS NOT NULL", (tenant, aid)).fetchone()
            assert row and row[3] == digest
            read(*row)
        person_bytes = sum(row[1] for row in child_transports)
        assert db.execute("SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'", (tenant, child)).fetchall() == [(268435456,)]
        assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'", (tenant, child)).fetchone() == (1,)
        assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'", (tenant, str(child))).fetchone() == (1,)
    added_bytes = sum(row[2] for row in documents) + sum(row[1] for row in transports) + person_bytes
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == (before[0] + added_bytes, before[1] + 268435456 * (1 + int(measured_person)))
    assert db.execute("SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'", (tenant, run)).fetchall() == [(268435456,)]
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'", (tenant, run)).fetchone() == (1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'", (tenant, str(run))).fetchone() == (1,)
    assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND resource_id=%s AND route='V2.Admit' AND response_status=202", (tenant, run)).fetchone() == (1,)


def verify_uncovered_person(db, client, bucket, case):
    """New physical mask exists, but missing protection must leave parent pending."""
    from io import BytesIO
    import numpy as np
    from photo_worker.contracts.images import MaskRef
    from photo_worker.contracts.person import PersonReport
    rows = db.execute("SELECT r.id,r.status,r.graph_sealed,t.id,t.state,t.canonical_result_id FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE w.tenant_id=%s AND w.workflow_type='semantic'", (case['Tenant'],)).fetchall()
    assert len(rows) == 1
    run, status, sealed, task, state, result = rows[0]
    assert (status, sealed, state) == ('running', True, 'succeeded') and result is not None
    config = db.execute('SELECT config_snapshot,v27_parent_recovery_done FROM photo.runs WHERE tenant_id=%s AND id=%s', (case['Tenant'],run)).fetchone()
    parent = config[0]['v27_person_prepare']['parent_run_id']
    assert config[0]['v27_person_prepare']['recovery']['person_profile_version'] == 1 and config[1] is False
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (case['Tenant'],parent)).fetchone() == (0,)
    assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND resource_id=%s AND route='V2.Admit'", (case['Tenant'],parent)).fetchone() == (0,)
    outputs = db.execute('SELECT a.id,a.sha256,a.object_key,a.object_version,a.bytes,a.output_role FROM photo.artifacts a WHERE a.tenant_id=%s AND a.created_by_task_id=%s', (case['Tenant'],task)).fetchall()
    assert len(outputs) == 4
    bodies = {}
    for aid, digest, key, version, size, role in outputs:
        assert version not in ('', 'null', None)
        response = client.get_object(bucket,key,version_id=version)
        try: body = response.read()
        finally: response.close(); response.release_conn()
        assert len(body) == size and sha256(body).hexdigest() == digest
        bodies[role] = body
    pixels = np.load(BytesIO(bodies['person_mask']),allow_pickle=False)
    assert pixels.shape == (300,400) and pixels.dtype == np.float32 and np.all(pixels>.99)
    mask = MaskRef.model_validate_json(bodies['person_mask_ref']); report = PersonReport.model_validate_json(bodies['person_report'])
    assert mask.artifact == report.mask and mask.selector == 'person' and report.person_fraction == 1
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s', (case['Tenant'],task)).fetchone() == (1,)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s', (task,)).fetchone() == (0,)
    assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'", (task,)).fetchone() == (0,)
