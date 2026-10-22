"""Versioned upstream review bytes; synthetic producer/qualification metadata.

Includes real CPU preview staging scenarios; detector/qualification data remain synthetic.
No photographic quality qualification or executed test result is claimed.
"""
from hashlib import sha256
from io import BytesIO

import psycopg
import pytest
from retouch_operator_fixture import retouch_preview_operator_enabled

pytestmark = pytest.mark.usefixtures('retouch_preview_operator_enabled')
from retouch_apply_documents_fixture import prepared_retouch_review_fixtures
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('target_count',[1,2])
def test_retouch_review_catalog_contains_exact_versioned_objects(database,lens_objects,target_count):
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        reviews=prepared_retouch_review_fixtures(db,target_count=target_count,with_alpha=True,objects=lens_objects)
        documents=reviews[0][2]['review_documents']
        assert len(documents)==5+target_count
        for document in documents:
            row=db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE id=%s',
                (document['artifact_id'],)).fetchone()
            assert row[0]==bucket and row[2] not in ('','null',None)
            response=client.get_object(row[0],row[1],version_id=row[2])
            try:raw=response.read()
            finally:response.close();response.release_conn()
            assert raw==document['body'].encode()
            assert len(raw)==row[4] and sha256(raw).hexdigest()==row[3]==document['sha256']
        # A newer object at the same key cannot change the pinned review bytes.
        plan=next(document for document in documents if document['contract']=='SemanticPlan')
        row=db.execute('SELECT object_key,object_version FROM photo.artifacts WHERE id=%s',
            (plan['artifact_id'],)).fetchone()
        client.put_object(bucket,row[0],BytesIO(b'{}'),2,content_type='application/json')
        response=client.get_object(bucket,row[0],version_id=row[1])
        try:assert response.read()==plan['body'].encode()
        finally:response.close();response.release_conn()


@pytest.mark.parametrize('alpha',[False,True])
def test_retouch_landmark_inputs_and_result_are_version_pinned_objects(database,lens_objects,alpha):
    from retouch_apply_documents_fixture import retouch_context_producer_fixture
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        _,_,scope,_=retouch_context_producer_fixture(db,with_alpha=alpha,objects=lens_objects)
        refs=[scope[key] for key in ('landmark_boxes_ref','landmark_params_ref','landmark_ref')]
        for ref in refs:
            row=db.execute('SELECT bucket,object_key,object_version,bytes,sha256 FROM photo.artifacts WHERE id=%s',
                (ref.artifact_id,)).fetchone()
            assert row[0]==bucket and row[2] not in (None,'','null','metadata-only-v1')
            response=client.get_object(bucket,row[1],version_id=row[2])
            try:raw=response.read()
            finally:response.close();response.release_conn()
            assert len(raw)==row[3] and sha256(raw).hexdigest()==row[4]==ref.sha256
            if ref==scope['landmark_ref']:assert raw==scope['landmark_body']
        assert db.execute('SELECT bucket,object_version FROM photo.output_uploads WHERE artifact_id=%s',
            (scope['landmark_ref'].artifact_id,)).fetchone()==(bucket,db.execute(
                'SELECT object_version FROM photo.artifacts WHERE id=%s',(scope['landmark_ref'].artifact_id,)).fetchone()[0])



def test_retouch_admission_documents_have_real_pinned_storage_versions(database,lens_objects):
    from retouch_preview_admission_fixture import prepared_retouch_preview_fixture,activate_retouch_preview_fixture
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        prepared=prepared_retouch_preview_fixture(db,objects=lens_objects)
        for _,ref,raw in prepared['published']:
            row=db.execute('SELECT bucket,object_key,object_version,bytes,sha256 FROM photo.artifacts WHERE id=%s',
                (ref['artifact_id'],)).fetchone()
            assert row[0]==bucket and row[2] not in (None,'','null','metadata-only-v1')
            response=client.get_object(bucket,row[1],version_id=row[2])
            try:stored=response.read()
            finally:response.close();response.release_conn()
            assert stored==raw and len(stored)==row[3] and sha256(stored).hexdigest()==row[4]==ref['sha256']
        assert activate_retouch_preview_fixture(db,prepared)==prepared['task']


@pytest.mark.parametrize('alpha', [False, True])
def test_physical_retouch_claim_preserves_production_lease_and_admission(database, lens_objects, alpha):
    from retouch_physical_worker_fixture import claimed_physical_preview_fixture
    from photo_worker.contracts.base import canonical_bytes
    with psycopg.connect(**database) as db:
        prepared, worker_database, context = claimed_physical_preview_fixture(
            db, lens_objects, with_alpha=alpha)
        lease = context.lease
        config = db.execute('SELECT config_snapshot FROM photo.runs WHERE id=%s',
            (prepared['run'],)).fetchone()[0]
        raw = canonical_bytes(config['semantic_retouch_preview']['params'])
        assert worker_database.admit_semantic_retouch_preview(lease, raw) is True
        assert worker_database.claim(lease.command, lease.worker_id).outcome == 'busy'
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',
            (prepared['task'],))
        assert worker_database.admit_semantic_retouch_preview(lease, raw) is False


@pytest.mark.parametrize('alpha', [False, True])
def test_physical_retouch_task_artifacts_loads_typed_inputs_and_stages_versioned_bytes(
        database, lens_objects, tmp_path, alpha):
    from retouch_physical_worker_fixture import claimed_physical_preview_fixture
    from retouch_physical_gateway_fixture import physical_task_artifacts
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    with psycopg.connect(**database) as db:
        prepared, _, context = claimed_physical_preview_fixture(db, lens_objects, with_alpha=alpha)
        with physical_task_artifacts(db, lens_objects, context, tmp_path) as artifacts:
            params = artifacts.params(SemanticRetouchPreviewInput)
            assert params.target_id == prepared['target']
            assert context.lease.command.params in artifacts.declared_input_refs()
            # Transport payload only; never presented as a computed preview.
            raw = b'{"transport_fixture":true}'
            with artifacts.scratch.reserve(len(raw)) as reservation:
                path = reservation.path('transport', '.json')
                with artifacts.scratch.open_writer(path, reservation) as output:
                    output.write(raw)
                from photo_worker.runtime.gateway import GatewayError
                task = context.lease.command.task_id
                tenant = context.lease.command.tenant_id
                def ledger():
                    return (
                        db.execute('SELECT id FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id=%s ORDER BY id', (tenant, task)).fetchall(),
                        db.execute('SELECT id FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s ORDER BY id', (tenant, task)).fetchall(),
                        # Closed safe projections: no certificate/capability tokens,
                        # idempotency keys, buckets or object locators in diagnostics.
                        db.execute('SELECT id,state,bytes,sha256,media_type,object_version,verified_at,expires_at,width,height,icc_sha256 '
                            'FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id=%s ORDER BY id', (tenant, task)).fetchall(),
                        db.execute('SELECT id,artifact_id,attempt,fence,role,state,bytes,sha256,media_type,object_version,completed_at,run_reservation_id,returned_to_run_reservation '
                            'FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s ORDER BY id', (tenant, task)).fetchall(),
                        # This tenant is private to the case. Include every reservation
                        # in it so even an unexpected new/unlinked holder is detected.
                        db.execute('SELECT id,holder_type,holder_id,principal_type,principal_id,share_id,bytes,files,state,expires_at '
                            'FROM photo.quota_reservations WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall(),
                        db.execute('SELECT id,storage_bytes,reserved_bytes,source_files,reserved_files,active_runs,active_gpu_tasks '
                            'FROM photo.tenant_usage WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall(),
                        # This disposable tenant is owned by this fixture. Exact rows
                        # also detect any unexpected event emitted for this task.
                        db.execute('SELECT id,event_id,payload FROM photo.outbox WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall(),
                    )
                before = ledger()
                with pytest.raises(GatewayError, match='^OUTPUT_RESERVATION_REFUSED$'):
                    artifacts.stage(path, reservation, 'document', 'application/json')
                assert ledger() == before
                # Use an allowed JSON output role; this transport-only body is never committed as a result.
                ref = artifacts.stage(path, reservation, 'preview_evidence', 'application/json')
            row = db.execute('SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.sha256,u.state '
                'FROM photo.artifacts a JOIN photo.output_uploads u ON u.artifact_id=a.id '
                'WHERE a.id=%s', (ref.artifact_id,)).fetchone()
            assert row[0] == lens_objects[1] and row[2] not in (None, '', 'null')
            assert row[3:] == (len(raw), sha256(raw).hexdigest(), 'ready')
            response = lens_objects[0].get_object(row[0], row[1], version_id=row[2])
            try:assert response.read() == raw
            finally:response.close();response.release_conn()


@pytest.mark.parametrize('mutation', ['certificate', 'cancel'])
def test_physical_retouch_gateway_rechecks_authority_before_read(database, lens_objects, tmp_path, mutation):
    from retouch_physical_worker_fixture import claimed_physical_preview_fixture
    from retouch_physical_gateway_fixture import physical_task_artifacts
    from photo_worker.runtime.gateway import GatewayError
    with psycopg.connect(**database) as db:
        _, _, context = claimed_physical_preview_fixture(db, lens_objects)
        with physical_task_artifacts(db, lens_objects, context, tmp_path) as artifacts:
            if mutation == 'cancel':
                db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',
                    (context.lease.command.task_id,))
            else:
                db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s',
                    ('0'*64, context.lease.worker_id))
            with pytest.raises(GatewayError, match='LEASE_LOST'):
                artifacts.gateway.open_stream(context.lease.command.params)


@pytest.mark.parametrize('alpha', [False, True])
def test_physical_retouch_cpu_produces_full_versioned_output_closure(database, lens_objects, tmp_path, alpha):
    from retouch_physical_worker_fixture import execute_physical_preview_fixture
    from photo_worker.contracts.semantic_preview import RetouchPreviewPresentation, RetouchPreviewNumericalEvidence
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.semantic.preview_retouch_output_closure import ROLES
    with psycopg.connect(**database) as db:
        prepared, _, context, result = execute_physical_preview_fixture(
            db, lens_objects, tmp_path, with_alpha=alpha)
        rows = db.execute('SELECT u.role,a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes '
            'FROM photo.output_uploads u JOIN photo.artifacts a ON a.id=u.artifact_id '
            'WHERE u.task_id=%s AND u.attempt=%s AND u.fence=%s AND u.state=\'ready\'',
            (prepared['task'], context.lease.command.attempt, context.lease.fence)).fetchall()
        assert len(rows) == len(ROLES) and {r[0] for r in rows} == ROLES
        bodies, refs = {}, {}
        for role, aid, bucket, key, version, digest, size in rows:
            assert bucket == lens_objects[1] and version not in (None, '', 'null')
            response = lens_objects[0].get_object(bucket, key, version_id=version)
            try:raw = response.read()
            finally:response.close();response.release_conn()
            assert sha256(raw).hexdigest() == digest and len(raw) == size
            bodies[role] = raw
            refs[role] = dict(artifact_id=str(aid), sha256=digest)
        assert result.outputs[0].model_dump(mode='json') == refs['preview_presentation']
        assert len(result.outputs) == 1 and result.peak_device_bytes == 0
        assert result.bytes_written == sum(row[6] for row in rows)
        display = RetouchPreviewPresentation.model_validate_json(bodies['preview_presentation'])
        evidence = RetouchPreviewNumericalEvidence.model_validate_json(bodies['preview_evidence'])
        image = WorkingImage.model_validate_json(bodies['working_image'])
        original = WorkingImage.model_validate_json(prepared['scope']['source_body'])
        assert display.params == prepared['scope']['retouch_params']
        assert display.numerical_evidence.model_dump(mode='json') == refs['preview_evidence']
        assert display.working_image.model_dump(mode='json') == refs['working_image']
        assert display.pixels == image.pixels == evidence.pixels
        assert image.alpha == original.alpha
        # Verify physical pixel preservation independently of boolean report fields.
        from io import BytesIO
        import numpy as np
        source_row=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s',
            (prepared['args'][0],original.pixels.artifact_id,original.pixels.sha256)).fetchone()
        assert source_row is not None
        response=lens_objects[0].get_object(source_row[0],source_row[1],version_id=source_row[2])
        try:source_raw=response.read()
        finally:response.close();response.release_conn()
        assert sha256(source_raw).hexdigest()==original.pixels.sha256
        source_pixels=np.load(BytesIO(source_raw),allow_pickle=False)
        result_pixels=np.load(BytesIO(bodies['pixels']),allow_pickle=False)
        selected=np.load(BytesIO(prepared['scope']['selected_binary_body']),allow_pickle=False)
        assert selected[0,0]==0 and np.count_nonzero(selected)>0
        assert np.array_equal(result_pixels[selected==0],source_pixels[selected==0])
        assert np.array_equal(result_pixels[0,0],source_pixels[0,0])
        assert evidence.protected_pixels_unchanged and evidence.outside_mask_unchanged and evidence.alpha_unchanged
        assert context.snapshot_progress() == dict(done=1, total=1)
        # Staging is not success/adoption and cannot manufacture a result/receipt.
        assert db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',
            (prepared['task'],)).fetchone() == ('running', None)


@pytest.mark.parametrize('alpha', [False, True])
def test_physical_retouch_executor_commits_one_real_result_and_fences_redelivery(database, lens_objects, tmp_path, alpha):
    from retouch_physical_worker_fixture import completed_physical_preview_fixture
    from photo_worker.operations.semantic.preview_retouch_output_closure import ROLES
    with psycopg.connect(**database) as db:
        prepared, _, context, result = completed_physical_preview_fixture(
            db, lens_objects, tmp_path, with_alpha=alpha)
        state, canonical = db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',
            (prepared['task'],)).fetchone()
        assert state == 'succeeded' and canonical is not None
        attempt, fence, outputs, metrics = db.execute(
            'SELECT attempt,fencing_token,outputs,metrics FROM photo.task_results WHERE id=%s',
            (canonical,)).fetchone()
        assert attempt == context.lease.command.attempt and fence == context.lease.fence
        assert outputs == [ref.model_dump(mode='json') for ref in result.outputs]
        assert metrics['peak_device_bytes'] == 0 and metrics['peak_memory_bytes'] > 0
        assert metrics['duration_ms'] >= 0 and metrics['bytes_written'] == result.bytes_written
        assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',
            (prepared['task'],)).fetchone() == (1,)
        assert db.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',
            (prepared['task'],)).fetchone() == (1,)
        assert db.execute('SELECT count(*) FROM photo.output_uploads WHERE task_id=%s',
            (prepared['task'],)).fetchone() == (len(ROLES),)
        # Worker success does not itself forge the separate shown/Apply receipt.
        assert db.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
            (prepared['run'],)).fetchone() == (None,)
