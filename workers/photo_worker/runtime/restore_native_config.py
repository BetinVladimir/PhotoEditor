"""Concrete pinned native renderer installation for isolated restore probes."""
from pathlib import Path
from uuid import UUID, uuid5
import psycopg

from photo_worker.v2.render_native_installation import NativeRenderInstallation
from photo_worker.v2.render_qualification_pin import RenderQualificationPin
from .restore_probe_config import _record, _read_pin, _pin
from .restore_render_barrier import RestoreBarrierRenderer
from .database import TaskDatabase


def install_restore_native_renderer(value, worker_config, *, probe_value,
                                     authority_database_url, expected_database,
                                     assert_isolation, check_active):
    _record(value, ('runtime', 'qualification', 'tenant_id', 'project_id', 'icc_lock_sha',
        'provider_peak', 'max_input_bytes', 'max_mask_bytes', 'max_tile_bytes', 'max_document_bytes'))
    pin = RenderQualificationPin.from_value(value['qualification'])
    if worker_config.backend != pin.backend or worker_config.resource_class != 'gpu':
        raise ValueError('RESTORE_NATIVE_WORKER_PROFILE')
    _read_pin(value['runtime'], 1 << 20, check_active)
    if value['runtime']['sha256'] != pin.runtime_lock_sha:
        raise ValueError('RESTORE_NATIVE_RUNTIME_PIN')
    tenant, project = UUID(value['tenant_id']), UUID(value['project_id'])
    if tenant.int == 0 or project.int == 0:
        raise ValueError('RESTORE_NATIVE_SCOPE')
    # SQL policy and trusted ready report are rechecked on every installation
    # use. Task/adopter admission independently binds the full calibration pin.
    def verify(current):
        if current != pin or check_active() is False:
            return False
        assert_isolation()
        with psycopg.connect(authority_database_url, autocommit=True,
                             connect_timeout=5, options="-c statement_timeout=5000 -c lock_timeout=1000") as connection:
            with connection.transaction():
                connection.execute("SET LOCAL statement_timeout='5s'")
                identity = connection.execute('SELECT current_database(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)').fetchone()
                if identity != (expected_database, True):
                    raise ValueError('RESTORE_NATIVE_DATABASE_AUTHORITY')
                connection.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
                row = connection.execute('SELECT * FROM photo.resolve_v26_render_qualification(%s,%s)',
                                         (tenant, project)).fetchone()
        assert_isolation()
        return row == (pin.model_lock_sha, pin.runtime_lock_sha, pin.backend,
                       pin.device_profile_sha, pin.dataset_sha) and check_active() is not False
    installation = NativeRenderInstallation(runtime_path=Path(value['runtime']['path']),
        qualification_pin=pin, icc_lock_sha=_pin(value['icc_lock_sha']),
        verify_qualification=verify, provider_peak=value['provider_peak'],
        max_input_bytes=value['max_input_bytes'], max_mask_bytes=value['max_mask_bytes'],
        max_tile_bytes=value['max_tile_bytes'], max_document_bytes=value['max_document_bytes'])
    installation.verify()
    running = probe_value['revoke']
    running_tenant = UUID(running['running_target']['fixture']['tenant_id'])
    child = UUID(running['child_run_id'])
    task = uuid5(uuid5(child, 'workflow'), 'render.compose:0')
    renderer = installation.worker(TaskDatabase(worker_config.database_url))
    barrier = RestoreBarrierRenderer(renderer, running_tenant, task,
        maximum_seconds=running['maximum_seconds'])
    return installation, barrier
