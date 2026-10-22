"""Independent persisted target fixture checks; no authority bypass or writes.

These inspect actual canonical rows and version-pinned object bytes after the
nested worker processes exit. Model doubles remain explicit in the caller.
"""
from hashlib import sha256
from io import BytesIO
import json
import os
from uuid import UUID
import numpy as np
from PIL import Image
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.face import FaceBoxes, FaceLandmarks
from photo_worker.contracts.face_parse import FaceLabels19
from photo_worker.contracts.person import PersonReport

_ROLES = {
    'mask.person': ('person_mask', 'person_mask_png', 'person_mask_ref', 'person_report'),
    'face.detect': ('face_boxes',),
    'face.landmarks': ('face_landmarks',),
    'face.parse': ('face_labels', 'face_labels_ref'),
    'mask.face_skin': ('skin_mask', 'skin_mask_png', 'skin_mask_ref'),
    'defects.propose': ('defect_labels', 'defect_protection', 'defect_labels_png', 'defect_candidates', 'defect_propose_report'),
}


def _read(db, objects, tenant, ref, *, producer=None, role=None, attempt=None, fence=None):
    row = db.execute("""SELECT bucket,object_key,object_version,sha256,bytes,
        output_role,created_by_task_id,produced_attempt,produced_fence,media_type
        FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s
        AND state='ready' AND verified_at IS NOT NULL
        AND (expires_at IS NULL OR expires_at>clock_timestamp())""",
        (tenant, ref['artifact_id'], ref['sha256'])).fetchone()
    assert row is not None, 'missing verified target artifact'
    bucket, key, version, digest, size, actual_role, task, a, f, media = row
    assert key and version not in ('', 'null', None) and size > 0
    if producer is not None:
        assert (task, actual_role, a, f) == (producer, role, attempt, fence)
        receipts = db.execute("""SELECT count(*) FROM photo.output_uploads WHERE tenant_id=%s
            AND artifact_id=%s AND task_id=%s AND attempt=%s AND fence=%s
            AND state='ready' AND completed_at IS NOT NULL AND role=%s
            AND bucket=%s AND object_key=%s AND object_version=%s AND sha256=%s
            AND bytes=%s AND media_type=%s""", (tenant, ref['artifact_id'], producer,
            attempt, fence, role, bucket, key, version, digest, size, media)).fetchone()[0]
        assert receipts == 1, 'missing exact ready output receipt' 
    response = objects.get_object(bucket, key, version_id=version)
    try:
        body = response.read()
    finally:
        response.close()
        response.release_conn()
    assert len(body) == size and sha256(body).hexdigest() == digest == ref['sha256']
    return body


def _stages(db, objects, tenant, marker, operations):
    rows = db.execute("""SELECT r.id,r.status,r.graph_sealed,r.config_snapshot,
        t.id,t.operation,t.state,t.canonical_result_id,t.attempt,t.fencing_token,
        t.params_artifact_id,t.input_digest,t.execution_profile,t.inputs
        FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
        WHERE r.tenant_id=%s AND r.config_snapshot ? %s ORDER BY r.id""",
        (tenant, marker)).fetchall()
    assert rows and set(r[5] for r in rows) == set(operations)
    seen = set()
    result = []
    for run, status, sealed, config, task, op, state, canonical, attempt, fence, params_id, digest, profile, inputs in rows:
        stage = config[marker]
        identity = (stage['target_ordinal'], op)
        assert identity not in seen
        seen.add(identity)
        assert stage['schema_version'] == 1 and stage['task_id'] == str(task)
        assert stage.get('operation', op) == op
        assert stage['params']['artifact_id'] == str(params_id) and stage['input_digest'] == digest
        for name in ('parent_run_id', 'auth_session_id', 'target_namespace', 'target_stage_id'):
            assert UUID(stage[name]).int != 0
        assert len(stage['manifest_sha']) == len(stage['request_sha']) == len(stage['policy_sha']) == 64
        assert stage['source']['source'] != stage['params']
        params = _read(db, objects, tenant, stage['params'])
        assert isinstance(json.loads(params), dict)
        registered = 'v2_input_manifest' in profile
        family = {'v27_target_person_prepare': 'PERSON', 'v27_target_detector_prepare': 'DETECTOR',
                  'v27_target_face_prepare': 'FACE', 'v27_target_skin_prepare': 'SKIN',
                  'v27_target_defect_prepare': 'DEFECT'}[marker]
        requested_registered = any(os.environ.get('PHOTO_TEST_V27_TARGET_'+family+'_'+phase) == '1'
                                   for phase in ('REGISTER', 'SEAL', 'EXECUTE', 'FINISH', 'ADVANCE'))
        if sealed or canonical is not None or requested_registered:
            assert registered, 'requested registered phase has no manifest'
        if registered:
            assert db.execute("""SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s
                AND task_id=%s AND artifact_id=%s AND sha256=%s AND contract='Params'""",
                (tenant, task, stage['params']['artifact_id'], stage['params']['sha256'])).fetchone()[0] == 1
        else:
            assert not sealed and status == 'draft' and state == 'blocked' and canonical is None
            assert attempt == 0 and inputs == []
            assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] == 0
        bodies = []
        outputs = []
        if canonical is None:
            assert state in ('blocked', 'ready', 'queued')
            assert (sealed and status == 'queued') or (not sealed and status == 'draft' and state == 'blocked')
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] == 0
        else:
            assert sealed and state == 'succeeded' and status in ('running', 'completed') and attempt > 0 and fence > 0
            saved = db.execute('SELECT outputs,attempt,fencing_token FROM photo.task_results WHERE tenant_id=%s AND id=%s AND task_id=%s', (tenant, canonical, task)).fetchone()
            assert saved is not None
            assert saved[1:] == (attempt, fence)
            outputs = saved[0]
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] == 1
            assert len(outputs) == len(_ROLES[op])
            for ref, role in zip(outputs, _ROLES[op]):
                bodies.append(_read(db, objects, tenant, ref, producer=task, role=role, attempt=attempt, fence=fence))
            assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s', (task,)).fetchone()[0] == 0
            assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'", (task,)).fetchone()[0] == 0
            assert db.execute('SELECT profile FROM photo.task_attempts WHERE tenant_id=%s AND task_id=%s AND attempt=%s AND fencing_token=%s AND finished_at IS NOT NULL', (tenant, task, attempt, fence)).fetchone()[0] == profile
        result.append((stage, outputs, bodies))
    return result


def verify_target_person_draft(db, objects, tenant):
    for stage, outputs, bodies in _stages(db, objects, tenant, 'v27_target_person_prepare', ('mask.person',)):
        if bodies:
            pixels = np.load(BytesIO(bodies[0]), allow_pickle=False)
            root = MaskRef.model_validate_json(bodies[2])
            assert pixels.dtype == np.float32 and pixels.ndim == 2 and np.isfinite(pixels).all()
            assert np.all((pixels >= 0) & (pixels <= 1)) and np.any(pixels > 0)
            assert root.selector == 'person' and not root.protected and root.artifact.model_dump(mode='json') == outputs[0]
            with Image.open(BytesIO(bodies[1])) as png:
                assert np.array_equal(np.asarray(png), np.rint(pixels * 65535).astype(np.uint16))
            report = PersonReport.model_validate_json(bodies[3])
            assert report.mask.model_dump(mode='json') == outputs[0]
            assert report.image.model_dump(mode='json') == stage['source']['source']
            assert report.backend == 'cuda' and report.model_lock_id == 'c'*64
            assert 0 < report.person_fraction <= 1


def verify_target_detector_draft(db, objects, tenant):
    for stage, outputs, bodies in _stages(db, objects, tenant, 'v27_target_detector_prepare', ('face.detect',)):
        if bodies:
            boxes = FaceBoxes.model_validate_json(bodies[0])
            assert boxes.faces and boxes.backend == 'cuda'
            assert boxes.image.model_dump(mode='json') == stage['source']['source']


def verify_target_face_blocked_drafts(db, objects, tenant):
    for stage, outputs, bodies in _stages(db, objects, tenant, 'v27_target_face_prepare', ('face.landmarks', 'face.parse')):
        if not bodies:
            continue
        if stage['operation'] == 'face.landmarks':
            manifest = FaceLandmarks.model_validate_json(bodies[0])
            assert manifest.landmarks and manifest.image.model_dump(mode='json') == stage['source']['source']
        else:
            labels = np.load(BytesIO(bodies[0]), allow_pickle=False)
            manifest = FaceLabels19.model_validate_json(bodies[1])
            assert manifest.labels.model_dump(mode='json') == outputs[0] and manifest.parsed_faces > 0
            assert manifest.image.model_dump(mode='json') == stage['source']['source']
            assert labels.dtype == np.uint16 and labels.ndim == 2 and np.all(labels <= 18) and np.any(labels == 1)


def verify_target_skin_executed_draft(db, objects, tenant):
    for stage, outputs, bodies in _stages(db, objects, tenant, 'v27_target_skin_prepare', ('mask.face_skin',)):
        assert bodies
        pixels = np.load(BytesIO(bodies[0]), allow_pickle=False)
        root = MaskRef.model_validate_json(bodies[2])
        assert pixels.dtype == np.float32 and np.isfinite(pixels).all() and np.all((pixels >= 0) & (pixels <= 1))
        assert root.selector == 'skin' and not root.protected and root.artifact.model_dump(mode='json') == outputs[0]
        parser_labels = np.load(BytesIO(_read(db, objects, tenant, stage['parser_outputs'][0])), allow_pickle=False)
        assert np.array_equal(pixels, parser_labels == 1)
        with Image.open(BytesIO(bodies[1])) as png:
            assert np.array_equal(np.asarray(png), np.rint(pixels * 65535).astype(np.uint16))


def verify_target_defect_prepared_params(db, objects, tenant):
    for stage, _, _ in _stages(db, objects, tenant, 'v27_target_defect_prepare', ('defects.propose',)):
        params = json.loads(_read(db, objects, tenant, stage['params']))
        assert len(stage['diameter_face_fraction']) == 2 and params
        for name in ('landmarks_outputs', 'skin_outputs', 'person_outputs'):
            assert stage[name]
            for ref in stage[name]:
                _read(db, objects, tenant, ref)
        assert stage['person_pixels'] in stage['person_outputs']
        assert stage['person_root'] in stage['person_outputs']


def verify_target_defect_transport(db, objects, tenant):
    from photo_worker.contracts.defects import DefectCandidates, DefectProposeReport
    verify_target_defect_prepared_params(db, objects, tenant)
    for stage, outputs, bodies in _stages(db, objects, tenant, 'v27_target_defect_prepare', ('defects.propose',)):
        if not bodies:
            continue
        labels = np.load(BytesIO(bodies[0]), allow_pickle=False)
        protection = np.load(BytesIO(bodies[1]), allow_pickle=False)
        assert labels.shape == protection.shape and labels.ndim == 2 and np.any(protection > 0)
        assert np.isfinite(protection).all() and np.all((protection >= 0) & (protection <= 1))
        assert not np.any(labels[protection > 0])
        with Image.open(BytesIO(bodies[2])) as png:
            assert np.array_equal(np.asarray(png), labels)
        proposal = DefectCandidates.model_validate_json(bodies[3])
        report = DefectProposeReport.model_validate_json(bodies[4])
        assert proposal.label_map.model_dump(mode='json') == outputs[0]
        assert report.protection.artifact.model_dump(mode='json') == outputs[1] and report.protection.protected


def verify_native_parent_admission(db, objects, tenant, required_protection):
    rows = db.execute("SELECT id,graph_sealed,status,config_snapshot FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'", (tenant,)).fetchall()
    assert len(rows) == 1
    parent, sealed, status, config = rows[0]
    assert sealed and status == 'queued'
    nodes = config['v27_semantic']['nodes']
    tasks = db.execute('SELECT id,state,input_digest,params_artifact_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s', (tenant, parent)).fetchall()
    assert len(tasks) == len(nodes) and tasks
    by_id = {str(row[0]): row for row in tasks}
    for node in nodes:
        task, state, digest, params_id = by_id[node['task_id']]
        assert state == 'ready' and digest and str(params_id) == node['params_artifact_id']
        assert node['target_protection'] == required_protection
        for ref in (node['source_protection'], node['target_protection']):
            root = MaskRef.model_validate_json(_read(db, objects, tenant, ref))
            assert root.protected
            _read(db, objects, tenant, root.artifact.model_dump(mode='json'))
        _read(db, objects, tenant, dict(artifact_id=node['params_artifact_id'], sha256=node['params_sha']))
