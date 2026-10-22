"""Real SQL/S3 admission and replay; synthetic qualification, no GPU/quality proof.
Authored for the deferred testing phase; not executed during coding.
"""
from physical_go_child_evidence import run_evidenced_go
from pathlib import Path
from io import BytesIO
from hashlib import sha256
import json
import os
import subprocess
import numpy as np
import pytest
import psycopg
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from test_v25_lens_objectstore import lens_objects
from test_v27_semantic_final_producer_gate import fixture
from semantic_qualification_fixture import frozen_qualification

ROOT = Path(__file__).resolve().parents[2]


def _analysis_diagnostic(database, tenant, project, operation):
    """Read-only facts, not execution-phase or runtime-authority proof."""
    with psycopg.connect(**database) as diagnostic:
        diagnostic.execute('SET TRANSACTION READ ONLY')
        diagnostic.execute("SET LOCAL statement_timeout='3000ms'")
        registry = diagnostic.execute("""
            SELECT version,resource_class,v2_enabled,backends ? 'mps'
            FROM photo.operation_registry WHERE operation=%s ORDER BY version,resource_class
        """, (operation,)).fetchall()
        rows = diagnostic.execute("""
            SELECT r.status,r.graph_sealed,r.cancel_requested,
                r.v28_analysis_descriptor_jcs IS NOT NULL,
                r.v28_analysis_queued_receipt IS NOT NULL,
                (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id),
                (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.purpose='task_params' AND d.state='ready'),
                (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.purpose='v2_input_chunk_0' AND d.state='ready'),
                (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.purpose='v2_input_manifest' AND d.state='ready'),
                (SELECT count(*) FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.expires_at>clock_timestamp())
            FROM photo.runs r WHERE r.tenant_id=%s
              AND r.config_snapshot->>'project_id'=%s
              AND r.config_snapshot->'v28_analysis_plan'->>'operation'=%s
            ORDER BY r.id
        """,(tenant,str(project),operation)).fetchall()
        diagnostic.rollback()
    keys=('run_status','graph_sealed','cancel_requested','descriptor_present','queued_receipt_present','tasks','ready_params','ready_chunk0','ready_manifest','live_run_reservations')
    return dict(operation=operation, registry=[dict(version=v,resource_class=c,v2_enabled=e,mps_supported=b) for v,c,e,b in registry], persisted_states=[dict(zip(keys,row)) for row in rows])

@pytest.mark.parametrize('operation', ['depth.predict', 'mask.person'])
def test_analysis_admission_and_stored_recovery_without_current_provider(database, lens_objects, tmp_path, operation, record_property):
    client, bucket = lens_objects
    record_property('qualification_evidence', 'synthetic isolated imported report; no GPU execution or image quality acceptance')
    pixels = BytesIO()
    np.save(pixels, np.full((5, 5, 3), .5, dtype='<f4'), allow_pickle=False)
    pixels_raw = pixels.getvalue()
    def source_builder(artifact, original_sha):
        image = json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        image['canvas'].update(width=5, height=5, source_sha256=original_sha, space_id='oriented')
        pixel = artifact(sha256(pixels_raw).hexdigest(), role='working_rgb', media='application/octet-stream', body=pixels_raw)
        image['pixels'] = {'artifact_id': str(pixel), 'sha256': sha256(pixels_raw).hexdigest()}
        return canonical_bytes(image)
    with psycopg.connect(**database) as setup:
        tenant, *_, scope = fixture(setup, snapshot_authority=True, source_builder=source_builder, qualification_objects=(client, bucket))
        setup.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tenant,))
        pin = frozen_qualification(setup, tenant, scope['project'], scope['actor'], objects=(client, bucket), operation=operation, backend='mps')
        # This isolated synthetic qualification tests admission/replay, not model quality.
        # Production catalog defaults remain disabled; opt in this fixture explicitly.
        activated = setup.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation=%s AND version='1' AND resource_class='gpu' AND backends ? 'mps' RETURNING operation", (operation,)).fetchall()
        assert activated == [(operation,)]
    source = json.loads(scope['source_body'])
    resources = dict(pixel_count=25, ram_bytes=1<<30, scratch_bytes=64<<20, output_bytes=1<<20, vram_bytes=256<<20)
    capacity = dict(pixel_count=200000000, ram_bytes=16<<30, scratch_bytes=8<<30, output_bytes=1<<30, vram_bytes=4<<30)
    seed = dict(tenant=str(tenant), project=str(scope['project']), actor=str(scope['actor']), session=str(scope['session']), snapshot=str(scope['snapshot']),
                operation=operation, base=dict(asset_version_id=str(scope['version']), source=dict(artifact_id=str(scope['source']), sha256=scope['source_sha']), canvas_hash=canonical_hash(source['canvas'])),
                runtime=dict(backend='mps', model_lock_sha=pin['model_lock_sha'], runtime_lock_sha=pin['runtime_lock_sha'], device_profile_sha=pin['device_profile_sha'], qualification=pin['report'], resource_estimate=resources),
                capacity=capacity, dataset_sha=pin['dataset_sha'], issuer=pin['issuer_id'])
    path = tmp_path/'analysis-recovery.json'
    path.write_text(json.dumps(seed))
    credentials = client._provider.retrieve()
    env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database), PHOTO_TEST_RELIGHT_ANALYSIS_RECOVERY_SEED=str(path),
               PHOTO_TEST_MINIO_URL='http://'+client._base_url.host, PHOTO_TEST_MINIO_BUCKET=bucket,
               PHOTO_TEST_MINIO_ACCESS=credentials.access_key, PHOTO_TEST_MINIO_SECRET=credentials.secret_key, GOCACHE=str(ROOT/'work/go-cache'))
    from uuid import uuid4
    evidence_root=Path(env.get('PHOTO_TEST_RETAINED_CHILD_EVIDENCE_ROOT',str(ROOT/'work/retained-physical-go-evidence'))).resolve()
    assert evidence_root.is_relative_to(ROOT) and evidence_root!=ROOT
    evidence=evidence_root/('analysis-status-'+uuid4().hex)
    evidence.mkdir(parents=True,mode=0o700)
    facts={'scope':'owner read-only persisted facts; not exact executed phase or API authority proof'}
    def capture(label):
        try:
            facts[label]=_analysis_diagnostic(database,tenant,scope['project'],operation)
        except Exception as error:
            facts[label]={'diagnostic_error_type':type(error).__name__}
        target=evidence/'status.json'
        target.touch(mode=0o600,exist_ok=True)
        target.write_text(json.dumps(facts,sort_keys=True,indent=2)+'\n')
    capture('before_child')
    original_error=None
    try:
        result = run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightAnalysisStoredRecovery$','-count=1','-timeout=90s','-v'], cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
        facts['child_returncode']=result.returncode
        assert result.returncode == 0, result.stdout+result.stderr
    except BaseException as error:
        original_error=error
        facts['child_exception_type']=type(error).__name__
        raise
    finally:
        try:
            capture('after_child')
        except BaseException:
            if original_error is None:
                raise
    record_property('analysis_status_diagnostic',str((evidence/'status.json').relative_to(ROOT)))

    startup = run_evidenced_go(['go','test','./cmd/photo','-run','^TestIntegrationRelightReceiptOnlyStartup$','-count=1','-timeout=60s','-v'], cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert startup.returncode == 0, startup.stdout+startup.stderr

    http = run_evidenced_go(['go','test','./internal/httpapi','-run','^TestIntegrationRelightReplayHTTP$','-count=1','-timeout=60s','-v'], cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert http.returncode == 0, http.stdout+http.stderr
