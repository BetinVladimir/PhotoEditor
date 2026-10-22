from __future__ import annotations
from dataclasses import dataclass
from datetime import datetime
from uuid import UUID
import psycopg
from time import sleep
from psycopg.types.json import Jsonb
from photo_worker.contracts.wire import TaskCommand, TaskError, TaskMetrics


@dataclass(frozen=True)
class Lease:
    command: TaskCommand
    worker_id: UUID
    fence: int
    until: datetime
    deadline: datetime
    execution_profile: dict
    input_digest: str


@dataclass(frozen=True)
class Claim:
    outcome: str
    lease: Lease | None


@dataclass(frozen=True)
class Heartbeat:
    accepted: bool
    until: datetime | None
    cancel_requested: bool


class TaskDatabase:
    """Each call has its own bounded connection/transaction, including heartbeat.

    The login must have photo_worker membership and an enabled registration bound
    to session_user. No tenant-wide table grants or caller-controlled SQL exist.
    """
    def media_renderer_policy_ready(self) -> bool:
        row=self._query('SELECT photo.media_renderer_policy_ready()',())
        return row is not None and len(row)==1 and row[0] is True

    def editor_internal_dispatch_installed(self, operation: str) -> bool:
        if operation not in ('editor.effects_seal','editor.layers_precompose'):
            return False
        row=self._query('SELECT photo.editor_internal_dispatch_installed(%s)',(operation,))
        return row is not None and len(row)==1 and row[0] is True

    def __init__(self, dsn: str):
        self._dsn = dsn

    def _connect(self):
        connection = psycopg.connect(self._dsn, connect_timeout=5,
                              options='-c role=photo_worker -c statement_timeout=5000 -c lock_timeout=3000')
        try:
            self._require_role(connection)
            return connection
        except BaseException:
            connection.close()
            raise

    def _worker_function_set_ready(self, signatures) -> bool:
        for signature in signatures:
            row = self._query('''SELECT pg_get_userbyid(p.proowner),p.prosecdef,
                (SELECT setting FROM unnest(p.proconfig) AS settings(setting)
                 WHERE split_part(setting,'=',1)='search_path'),
                has_function_privilege('photo_worker',p.oid,'EXECUTE'),
                has_function_privilege('photo_api',p.oid,'EXECUTE'),
                EXISTS (SELECT 1 FROM unnest(ARRAY['photo_gateway','photo_scheduler',
                    'photo_portal','photo_semantic_admission']) AS roles(role_name)
                    WHERE has_function_privilege(role_name,p.oid,'EXECUTE'))
                FROM pg_proc p WHERE p.oid=to_regprocedure(%s)''', (signature,))
            if (row is None or len(row) != 6 or row[0] != 'photo_fn_owner'
                    or row[1] is not True
                    or row[2] not in ('search_path=pg_catalog, photo', 'search_path=pg_catalog,photo')
                    or row[3] is not True or row[4] is not False or row[5] is not False):
                return False
        return True

    def task_result_fence_ready(self) -> bool:
        row=self._query("""SELECT count(*)=1 AND bool_and(
          p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
          AND position('-- 02535:' in p.prosrc)>0
          AND position('OUTPUT_NOT_VERIFIED' in p.prosrc)<position('-- 02535:' in p.prosrc)
          AND position('-- 02537:' in p.prosrc)>position('-- 02535:' in p.prosrc)
          AND position('-- 02537:' in p.prosrc)<position(' INSERT INTO photo.task_results(' in p.prosrc))
          AND (SELECT count(*)=9 AND bool_and(
            heartbeat.prosecdef AND pg_get_userbyid(heartbeat.proowner)='photo_fn_owner'
            AND heartbeat.prokind='f' AND heartbeat.provolatile='v' AND heartbeat.proparallel='u'
            AND NOT heartbeat.proleakproof AND NOT heartbeat.proisstrict AND heartbeat.proretset
            AND heartbeat.prorettype='pg_catalog.record'::regtype
            AND heartbeat.pronargs=5 AND heartbeat.pronargdefaults=0
            AND heartbeat.provariadic=0 AND heartbeat.prosupport=0
            AND heartbeat.procost=100 AND heartbeat.prorows=1000
            AND heartbeat.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
            AND heartbeat.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
            AND heartbeat.proargtypes=ARRAY['uuid'::regtype::oid,'int4'::regtype::oid,'int8'::regtype::oid,'uuid'::regtype::oid,'jsonb'::regtype::oid]::oidvector
            AND heartbeat.proallargtypes=ARRAY['uuid'::regtype::oid,'int4'::regtype::oid,'int8'::regtype::oid,'uuid'::regtype::oid,'jsonb'::regtype::oid,'bool'::regtype::oid,'timestamptz'::regtype::oid,'bool'::regtype::oid]
            AND heartbeat.proargmodes=ARRAY['i','i','i','i','i','t','t','t']::"char"[]
            AND heartbeat.proargnames=ARRAY['p_task','p_attempt','p_fence','p_worker','p_progress','accepted','lease_until','cancel_requested']::text[]
            AND (CASE WHEN expected.body_sha IS NOT NULL THEN
              (encode(sha256(convert_to(heartbeat.prosrc,'UTF8')),'hex')=expected.body_sha
               OR (expected.name='heartbeat_task'
                AND encode(sha256(convert_to(heartbeat.prosrc,'UTF8')),'hex')='ad68c2cc04acd441dafe096f07dd9404c7b1ed10e972a66c64c1b020d950597f'
                AND (SELECT count(*)=2 AND bool_and(
                  helper.prosecdef AND pg_get_userbyid(helper.proowner)='photo_fn_owner'
                  AND helper.prokind='f' AND helper.provolatile='v' AND helper.proparallel='u'
                  AND NOT helper.proleakproof AND NOT helper.proisstrict AND NOT helper.proretset
                  AND helper.prorettype=pin.result_type::regtype
                  AND helper.pronargs=1 AND helper.pronargdefaults=0
                  AND helper.proargdefaults IS NULL AND helper.proallargtypes IS NULL AND helper.proargmodes IS NULL
                  AND helper.proargtypes=ARRAY['uuid'::regtype::oid]::oidvector
                  AND helper.proargnames=ARRAY['p_task']::text[]
                  AND helper.provariadic=0 AND helper.prosupport=0 AND helper.procost=100 AND helper.prorows=0
                  AND helper.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
                  AND helper.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
                  AND encode(sha256(convert_to(helper.prosrc,'UTF8')),'hex')=pin.body_sha
                  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(helper.proacl,acldefault('f',helper.proowner))) a
                    WHERE a.privilege_type='EXECUTE' AND (a.grantee<>helper.proowner OR a.is_grantable))
                  AND NOT EXISTS(SELECT FROM unnest(ARRAY['photo_api','photo_auth','photo_worker','photo_gateway','photo_scheduler','photo_maintenance','photo_portal','photo_semantic_admission']) role(name)
                    WHERE has_function_privilege(role.name,helper.oid,'EXECUTE')))
                  FROM (VALUES
                    ('photo.authored_mask_lock_execution_parent(uuid)','pg_catalog.void','4c0183f52fd1ea6013a8c4a2bb54cf028d64921c5ebbc9fa3d871f674e48f79c'),
                    ('photo.authored_native_replay_parent_lock(uuid)','pg_catalog.bool','54198fcd79caf5fbc10476a547a86a516f1ab945e28a9ead648ece4ce494357d')) pin(signature,result_type,body_sha)
                  JOIN pg_proc helper ON helper.oid=to_regprocedure(pin.signature))))
            ELSE
              position('UPDATE photo.tasks SET lease_until=until_time,progress=p_progress' in heartbeat.prosrc)>0
              AND position('t:=photo.lock_task(p_task);' in heartbeat.prosrc)>0
              AND position('-- 02537:' in heartbeat.prosrc)>position('t:=photo.lock_task(p_task);' in heartbeat.prosrc)
              AND position('-- 02537:' in heartbeat.prosrc)<position(' until_time:=' in heartbeat.prosrc)
            END)
            AND has_function_privilege(heartbeat.proowner,heartbeat.oid,'EXECUTE')
            AND (expected.name<>'heartbeat_task' OR has_function_privilege('photo_worker',heartbeat.oid,'EXECUTE'))
            AND NOT EXISTS(SELECT FROM aclexplode(coalesce(heartbeat.proacl,acldefault('f',heartbeat.proowner))) a
              WHERE a.privilege_type='EXECUTE' AND (a.is_grantable OR
                a.grantee<>heartbeat.proowner AND (expected.name<>'heartbeat_task' OR a.grantee<>'photo_worker'::regrole)))
            AND NOT EXISTS(SELECT FROM unnest(ARRAY['photo_api','photo_auth','photo_worker','photo_gateway','photo_scheduler','photo_maintenance','photo_portal','photo_semantic_admission']) role(name)
              WHERE (expected.name<>'heartbeat_task' OR role.name<>'photo_worker')
              AND has_function_privilege(role.name,heartbeat.oid,'EXECUTE')))
            FROM (VALUES
              ('heartbeat_task_pre_shown_media','d4fc1a2c94e61554c9298661bdf37fe81a9d8b67b49712bd73791434bd085096'),
              ('heartbeat_task_before_removal_preview','7c1eb6f1693023d7d93f6dbc4943fe2074d1330f168434d2870e667dab5952ea'),
              ('heartbeat_task_before_shown_removal','305d94125e0157d5b3f72bf5aa994b7ca81748cd44d2dc12a42617947cfacfd0'),
              ('heartbeat_task_before_background_cleanup_preview','f39291862dea6965073806d51cdec231145bc91860309980a3bda008e1c503f8'),
              ('heartbeat_task_before_shown_background_cleanup','ceca9ab3fc9edb0c7f77fb5a0b11148c8f293b04cbbbc4fc4a4fbc2ba4231065'),
              ('heartbeat_task_before_retouch_preview','09d4200eabebe93c5c101a3a8907d60b69b4c3a6f939b5cf957c51aec503a0b2'),
              ('heartbeat_task_before_shown_retouch','a39cbed8435b2224b6b8ef96e4d1f398f4efecb960414413d3f5a96b3af04713'),
              ('heartbeat_task','a42d7c40c0c9cd5de0910ae9109a224f0e3939972c0fd163e139c4de06c619b8'),
              ('heartbeat_task_pre_v23_series_device',NULL)) expected(name,body_sha)
            JOIN pg_proc heartbeat ON heartbeat.oid=to_regprocedure('photo.'||expected.name||'(uuid,integer,bigint,uuid,jsonb)'))
          AND EXISTS(SELECT 1 FROM pg_proc failure
           WHERE failure.oid=to_regprocedure('photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb)')
           AND failure.prosecdef AND pg_get_userbyid(failure.proowner)='photo_fn_owner'
           AND position('-- 02543: final failure authority' in failure.prosrc)>position('IF old_error=p_error AND old_metrics=p_metrics' in failure.prosrc)
           AND position('-- 02543: no authority/helper wait' in failure.prosrc)>position('-- 02543: final failure authority' in failure.prosrc)
           AND position('OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()' in failure.prosrc)>position('-- 02543: no authority/helper wait' in failure.prosrc)
           AND position('OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()' in failure.prosrc)<position(' UPDATE photo.tasks SET state=CASE WHEN cancelled' in failure.prosrc)
           AND has_function_privilege('photo_worker',failure.oid,'EXECUTE')
           AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(failure.proacl,acldefault('f',failure.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
          AND EXISTS(SELECT 1 FROM pg_proc helper WHERE helper.oid=to_regprocedure('photo.execution_source_live(uuid)')
           AND helper.prosecdef AND pg_get_userbyid(helper.proowner)='photo_fn_owner'
           AND position('AND photo.semantic_scene_task_live(p_task) IS TRUE' in helper.prosrc)>0
           AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(helper.proacl,acldefault('f',helper.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
           AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_gateway','photo_scheduler','photo_maintenance','photo_portal','photo_semantic_admission']) role(name)
            WHERE has_function_privilege(role.name,helper.oid,'EXECUTE')))
          AND (SELECT count(*)=5 AND bool_and(scene.prosecdef AND pg_get_userbyid(scene.proowner)='photo_fn_owner'
            AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(scene.proacl,acldefault('f',scene.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
            AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_gateway','photo_scheduler','photo_maintenance','photo_portal','photo_semantic_admission']) role(name)
             WHERE has_function_privilege(role.name,scene.oid,'EXECUTE')))
            FROM pg_proc scene WHERE scene.oid=ANY(ARRAY[
             to_regprocedure('photo.semantic_scene_source_pins_live(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb)'),
             to_regprocedure('photo.semantic_scene_qualification_live(uuid,uuid,uuid,uuid,jsonb,jsonb)'),
             to_regprocedure('photo.semantic_scene_params_live(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb)'),
             to_regprocedure('photo.semantic_scene_scope_live(uuid,uuid,uuid)'),
             to_regprocedure('photo.semantic_scene_task_live(uuid)')]))
          AND EXISTS(SELECT 1 FROM pg_proc scene WHERE scene.oid=to_regprocedure('photo.semantic_scene_task_live(uuid)')
            AND position('op.v2_enabled' in scene.prosrc)>0 AND position('photo.semantic_scene_scope_live' in scene.prosrc)>0)
          AND EXISTS(SELECT 1 FROM pg_proc scene WHERE scene.oid=to_regprocedure('photo.semantic_scene_scope_live(uuid,uuid,uuid)')
            AND position('photo.semantic_scene_source_pins_live' in scene.prosrc)>0
            AND position('photo.semantic_scene_qualification_live' in scene.prosrc)>0
            AND position('photo.semantic_scene_params_live' in scene.prosrc)>0
            AND position('FOR UPDATE' in upper(scene.prosrc))=0 AND position('FOR SHARE' in upper(scene.prosrc))=0)
          FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
          WHERE n.nspname='photo' AND position(
          ' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)'
          in p.prosrc)>0""", ())
        return row is not None and len(row)==1 and row[0] is True

    def removal_preview_runtime_ready(self) -> bool:
        """Require the removal preview admission, qualification and provenance ABI."""
        return self._worker_function_set_ready((
            'photo.admit_semantic_removal_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_removal_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.semantic_removal_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
        ))

    def shown_removal_runtime_ready(self) -> bool:
        """Require the separate exact shown removal copy SQL authority."""
        return self._worker_function_set_ready((
            'photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_shown_removal_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        ))

    def retouch_preview_runtime_ready(self) -> bool:
        """Require every worker SQL gate used by the retouch preview pipeline."""
        return self._worker_function_set_ready((
            'photo.admit_semantic_retouch_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text)',
            'photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text)',
            'photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
        ))

    def shown_retouch_runtime_ready(self) -> bool:
        """Require the separate Apply-copy admission and qualification SQL ABI."""
        return self._worker_function_set_ready((
            'photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        ))

    def local_portrait_runtime_ready(self) -> bool:
        """Require the live worker-only lease and manual-origin SQL ABI."""
        return self._worker_function_set_ready((
            'photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v28_worker_manual_pair_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text,uuid,text)',
        ))

    def restoration_runtime_ready(self) -> bool:
        """Require both V28 restoration worker gates before registration."""
        return self._worker_function_set_ready((
            'photo.v28_restoration_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v28_restoration_qualification_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        ))

    def glare_donor_runtime_ready(self) -> bool:
        """Require the separate worker-only donor consent gate for glare."""
        return self._worker_function_set_ready((
            'photo.v28_glare_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)',
            'photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)',
            'photo.v28_glare_donor_input_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,uuid,text)',
        ))

    def heartbeat_cpu_worker(self,worker_id:UUID,resource_class:str,operations:frozenset[str],ram_bytes:int,scratch_bytes:int)->bool:
        row=self._query('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,%s,%s)',(worker_id,resource_class,sorted(operations),ram_bytes,scratch_bytes))
        return row is not None and row[0] is True

    def heartbeat_policy_worker(self, worker_id, resource_class, backend, operations, ram_bytes, vram_bytes, scratch_bytes, policy_sha):
        import re
        if not isinstance(policy_sha,str) or re.fullmatch(r'[0-9a-f]{64}',policy_sha) is None:
            raise ValueError('CAPACITY_POLICY_SHA_INVALID')
        row=self._query('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,%s,%s,%s,%s,%s,%s)',
            (worker_id,resource_class,backend,sorted(operations),ram_bytes,vram_bytes,scratch_bytes,policy_sha))
        return row is not None and row[0] is True

    def heartbeat_policy_worker_with_residency(self, worker_id, resource_class, backend,
            operations, ram_bytes, vram_bytes, scratch_bytes, policy_sha, residents):
        from dataclasses import asdict
        from photo_worker.models.pool import ResidentModel
        import re
        if (not isinstance(worker_id, UUID) or worker_id.int == 0
                or not isinstance(policy_sha, str) or re.fullmatch(r'[0-9a-f]{64}', policy_sha) is None
                or not isinstance(residents, tuple) or len(residents) > 16
                or any(not isinstance(item, ResidentModel) for item in residents)):
            raise ValueError('MODEL_RESIDENCY_INVALID')
        payload = Jsonb([asdict(item) for item in residents])
        args = (worker_id, resource_class, backend, sorted(operations),
                ram_bytes, vram_bytes, scratch_bytes, policy_sha)
        for attempt in range(3):
            try:
                with self._connect() as conn:
                    capacity = conn.execute('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()
                    if capacity is None or capacity[0] is not True:
                        conn.rollback()
                        return False
                    residency = conn.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',
                        (worker_id, policy_sha, payload)).fetchone()
                    if residency is None or residency[0] is not True:
                        conn.rollback()
                        return False
                return True
            except (psycopg.errors.SerializationFailure, psycopg.errors.DeadlockDetected):
                if attempt == 2:
                    raise
                sleep(.01 * (attempt + 1))

    def report_model_residency(self, worker_id, policy_sha, residents):
        from dataclasses import asdict
        from photo_worker.models.pool import ResidentModel
        import re
        if (not isinstance(worker_id, UUID) or worker_id.int == 0
                or not isinstance(policy_sha, str) or re.fullmatch(r'[0-9a-f]{64}', policy_sha) is None
                or not isinstance(residents, tuple) or len(residents) > 16
                or any(not isinstance(item, ResidentModel) for item in residents)):
            raise ValueError('MODEL_RESIDENCY_INVALID')
        row = self._query('SELECT photo.report_worker_model_residency(%s,%s,%s)',
                          (worker_id, policy_sha, Jsonb([asdict(item) for item in residents])))
        return row is not None and row[0] is True

    def capacity_policy_timing_ready(self, worker_id, policy_sha, offline_seconds, lease_seconds):
        import re
        if (not isinstance(worker_id, UUID) or worker_id.int == 0
                or not isinstance(policy_sha, str) or re.fullmatch(r'[0-9a-f]{64}', policy_sha) is None
                or any(type(value) is not int or not 1 <= value <= 2**53 - 1
                       for value in (offline_seconds, lease_seconds))):
            raise ValueError('CAPACITY_POLICY_TIMING_INVALID')
        row = self._query('SELECT photo.capacity_policy_timing_ready(%s,%s,%s,%s)',
                          (worker_id, policy_sha, offline_seconds, lease_seconds))
        return row is not None and row[0] is True

    def signer_runtime_ready(self, worker_id: UUID, sdk_sha: str, certificate_sha: str,
                             trust_sha: str, qualification_sha: str) -> bool:
        """Read provisioned qualification; this never registers or renews it."""
        import re
        if (not isinstance(worker_id, UUID) or worker_id.int == 0
                or any(not isinstance(value, str) or re.fullmatch(r'[0-9a-f]{64}', value) is None
                       for value in (sdk_sha, certificate_sha, trust_sha, qualification_sha))):
            raise ValueError('SIGNER_QUALIFICATION_PINS')
        row = self._query('SELECT photo.signer_runtime_ready(%s,%s,%s,%s,%s)',
                          (worker_id, sdk_sha, certificate_sha, trust_sha, qualification_sha))
        return row is not None and row[0] is True

    def series_runtime_ready(self, worker_id: UUID, runtime_sha: str,
                             device_sha: str) -> bool:
        """Read the provisioned worker binding before serving pinned Apply."""
        import re
        if (not isinstance(worker_id, UUID) or worker_id.int == 0 or
                any(not isinstance(value, str) or
                    re.fullmatch(r'[0-9a-f]{64}', value) is None
                    for value in (runtime_sha, device_sha))):
            raise ValueError('SERIES_RUNTIME_PINS')
        row = self._query(
            'SELECT photo.v23_series_worker_runtime_ready(%s,%s,%s)',
            (worker_id, runtime_sha, device_sha))
        return row is not None and row[0] is True

    def heartbeat_accelerator_worker(self,worker_id:UUID,backend:str,operations:frozenset[str],ram_bytes:int,vram_bytes:int,scratch_bytes:int)->bool:
        row=self._query('SELECT photo.heartbeat_accelerator_worker(%s,%s,%s,%s,%s,%s)',(worker_id,backend,sorted(operations),ram_bytes,vram_bytes,scratch_bytes))
        return row is not None and row[0] is True

    def accelerator_operations(self,worker_id:UUID,backend:str)->frozenset[str]:
        with self._connect() as conn:
            rows=conn.execute("SELECT operation FROM photo.worker_runtime_operations(%s,'gpu',%s)",(worker_id,backend)).fetchall()
        if len(rows)>128:raise ValueError('Worker operation limit exceeded')
        return frozenset(row[0] for row in rows)

    def runtime_operations(self,worker_id:UUID,resource_class:str)->frozenset[str]:
        with self._connect() as conn:
            rows=conn.execute('SELECT operation FROM photo.worker_runtime_operations(%s,%s)',(worker_id,resource_class)).fetchall()
        if len(rows)>128:raise ValueError('Worker operation limit exceeded')
        return frozenset(row[0] for row in rows)

    def ping(self):
        with self._connect() as conn:
            self._require_role(conn)

    @staticmethod
    def _require_role(conn):
        from .application_role import require_application_role
        try:
            require_application_role(conn, 'photo_worker')
        except ValueError as error:
            raise ValueError('WORKER_DATABASE_ROLE_ISOLATION_INVALID') from error

    def _query(self, query: str, params: tuple):
        for attempt in range(3):
            try:
                with self._connect() as conn:
                    row = conn.execute(query,params).fetchone()
                return row
            except (psycopg.errors.SerializationFailure,psycopg.errors.DeadlockDetected):
                if attempt == 2:
                    raise
                sleep(.01*(attempt+1))

    def local_portrait_lease_ready(self, lease: Lease, params: bytes) -> bool:
        """Attempt gate only; immutable source/mask authority is separate."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_local_portrait_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def relight_lease_ready(self, lease: Lease, params: bytes) -> bool:
        """Live attempt gate; producer and qualification authority remain separate."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_relight_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def negative_source_ready(self, lease: Lease, params: bytes) -> bool:
        """Accepted snapshot source scope; original bytes/ICC remain separate."""
        command = lease.command
        row = self._query(
            'SELECT photo.v29_worker_negative_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def negative_lease_ready(self, lease: Lease, params: bytes) -> bool:
        """Attempt gate only; immutable source/qualification authority is separate."""
        command = lease.command
        row = self._query(
            'SELECT photo.v29_negative_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def donor_portrait_lease_ready(self, lease: Lease, params: bytes) -> bool:
        """Attempt gate only; immutable source/mask authority is separate."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_donor_portrait_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def restoration_lease_ready(self, lease: Lease, params: bytes) -> bool:
        """V28 glare/reflection attempt gate; independent qualification remains required."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_restoration_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def restoration_qualification_ready(self, lease: Lease, params: bytes) -> bool:
        """Current independent qualified-policy pin for glare or reflection."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_restoration_qualification_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def glare_donor_source_ready(self, lease: Lease, params: bytes,
                                 proof: bytes) -> bool:
        """Current server-issued donor lineage and consent for donor-mode glare."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_glare_donor_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             proof.decode('utf-8')))
        return row is not None and row[0] is True

    def glare_generative_source_ready(self, lease: Lease, params: bytes,
                                       proof: bytes) -> bool:
        """Current issued source-only identity/consent for generative glare."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_glare_generative_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             proof.decode('utf-8')))
        return row is not None and row[0] is True

    def glare_donor_input_evidence(self, lease: Lease, params: bytes,
                                   proof: bytes, ref):
        """Exact versioned object locator for one declared glare input."""
        command = lease.command
        row = self._query(
            'SELECT photo.v28_glare_donor_input_evidence(' + ','.join(['%s']*13) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             proof.decode('utf-8'), ref.artifact_id, ref.sha256))
        return None if row is None else row[0]

    def negative_input_evidence(self, lease: Lease, params: bytes, ref):
        command = lease.command
        row = self._query(
            'SELECT photo.v29_worker_negative_input_evidence(' + ','.join(['%s']*12) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             ref.artifact_id, ref.sha256))
        return None if row is None else row[0]

    def donor_input_evidence(self, lease: Lease, params: bytes, confirmation: bytes, ref):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_donor_input_evidence(' + ','.join(['%s']*13) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             confirmation.decode('utf-8'), ref.artifact_id, ref.sha256))
        return None if row is None else row[0]

    def donor_source_ready(self, lease: Lease, params: bytes, confirmation: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_donor_source_ready(' + ','.join(['%s']*11) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             confirmation.decode('utf-8')))
        return row is not None and row[0] is True

    def negative_qualification_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v29_worker_negative_qualification_evidence(' + ','.join(['%s']*10) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def perspective_capabilities_ready(self) -> bool:
        row = self._query("SELECT to_regprocedure('photo.v29_worker_perspective_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)') IS NOT NULL", ())
        return row is not None and row[0] is True

    def perspective_qualification_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v29_worker_perspective_qualification_evidence(' + ','.join(['%s']*10) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def multi_capabilities_ready(self) -> bool:
        row = self._query("SELECT to_regprocedure('photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)') IS NOT NULL", ())
        return row is not None and row[0] is True

    def multi_qualification_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v29_worker_multi_qualification_evidence(' + ','.join(['%s']*10) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None


    def relight_manual_origin(self, lease: Lease, params: bytes, mask: bytes, root: bytes | None, protected: bool):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_relight_manual_origin(' + ','.join(['%s']*13) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             mask.decode('utf-8'), None if root is None else root.decode('utf-8'), protected))
        return row[0] if row is not None else None

    def relight_person_origin(self, lease: Lease, params: bytes, mask: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_relight_person_origin(' + ','.join(['%s']*11) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             mask.decode('utf-8')))
        return row[0] if row is not None else None

    def relight_depth_origin(self, lease: Lease, params: bytes, depth: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_relight_depth_origin(' + ','.join(['%s']*11) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             depth.decode('utf-8')))
        return row[0] if row is not None else None

    def relight_source_ready(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_relight_source_ready(' + ','.join(['%s']*10) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def relight_qualification_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_relight_qualification_evidence(' + ','.join(['%s']*10) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def donor_qualification_evidence(self, lease: Lease, params: bytes, confirmation: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v28_worker_donor_qualification_evidence(' + ','.join(['%s']*11) + ')',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             confirmation.decode('utf-8')))
        return row[0] if row is not None else None

    def local_portrait_manual_origin(self, lease: Lease, params: bytes, target, guard, vectors=None):
        command = lease.command
        return self._query(
            'SELECT * FROM photo.v28_worker_manual_pair_origin(' + ','.join(['%s']*16) + ')',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,
             params.decode('utf-8'),lease.input_digest,target.artifact_id,target.sha256,
             guard.artifact_id,guard.sha256,
             None if vectors is None else vectors.artifact_id,None if vectors is None else vectors.sha256))

    def donor_manual_origin(self, lease: Lease, params: bytes, target, guard, vectors=None):
        command = lease.command
        return self._query(
            'SELECT * FROM photo.v28_worker_donor_manual_origin(' + ','.join(['%s']*16) + ')',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,
             params.decode('utf-8'),lease.input_digest,target.artifact_id,target.sha256,
             guard.artifact_id,guard.sha256,
             None if vectors is None else vectors.artifact_id,None if vectors is None else vectors.sha256))

    def interchange_parse_context(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_interchange_parse_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else row[0]

    def interchange_export_context(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_interchange_export_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else row[0]

    def interchange_export_snapshot(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_export_snapshot_authority(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else row[0]

    def interchange_export_descriptor_authority(self, lease: Lease, params: bytes, evidence: dict):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_export_descriptor_authority(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest, Jsonb(evidence)))
        return row is not None and row[0] is True

    def interchange_import_context(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_interchange_import_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else row[0]

    def interchange_import_person_mask_ready(self) -> bool:
        return self._worker_function_set_ready((
            'photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text)',
        ))

    def interchange_import_person_mask_live(self, lease: Lease, params: bytes,
            workflow_id: UUID, project_id: UUID, target_version_id: UUID,
            prepared_mask_id: UUID, mask_sha: str):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_import_worker_person_mask_live(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             workflow_id, project_id, target_version_id, prepared_mask_id, mask_sha))
        return None if row is None else row[0]

    def delivery_lease_ready(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.v30_delivery_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def delivery_source_ready(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.v30_delivery_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def delivery_signed_source_scope(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v30_delivery_signed_source_scope(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else row[0]

    def admit_lens_user_composition(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v25_lens_user_composition(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_render_recipe(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v26_render_recipe(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def render_qualification(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v26_render_worker_qualification(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def semantic_qualification(self, lease: Lease):
        """Lease-scoped metadata; caller still verifies report bytes and sources."""
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_qualification(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def semantic_assembly_qualification(self, lease: Lease):
        """Active CPU assembly lease; does not prove report bytes/provenance."""
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_assembly_worker_qualification(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def semantic_assembly_snapshot(self, lease: Lease):
        command=lease.command
        row=self._query(
            "SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.ordinal),'[]'::jsonb) FROM photo.v27_semantic_assembly_worker_snapshot(%s,%s,%s,%s,%s,%s,%s,%s,%s) s",
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else []

    def semantic_assembly_producer(self, lease: Lease, candidate):
        command=lease.command
        row=self._query(
            "SELECT coalesce(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM photo.v27_semantic_assembly_worker_producer(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) p",
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest,candidate.artifact_id,candidate.sha256))
        values=row[0] if row is not None else []
        return values[0] if len(values)==1 else None

    def semantic_assembly_report_evidence(self, lease: Lease):
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_assembly_worker_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def semantic_protection_pins(self, lease: Lease):
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_protection_pins(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def semantic_protection_origin(self, lease: Lease, binding, root, child, *, target: bool) -> bool:
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,
             lease.input_digest,binding.asset_version_id,binding.source.artifact_id,binding.source.sha256,
             target,root.artifact_id,root.sha256,child.artifact_id,child.sha256))
        return row is not None and row[0] is True

    def semantic_source_ready(self, lease: Lease, binding, *, target: bool) -> bool:
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,
             lease.input_digest,binding.asset_version_id,binding.source.artifact_id,
             binding.source.sha256,target))
        return row is not None and row[0] is True

    def semantic_scene(self, lease: Lease):
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_scene(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def semantic_report_evidence(self, lease: Lease):
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

    def render_executed_base_receipt(self, lease: Lease, params: bytes):
        command = lease.command
        return self._query(
            'SELECT * FROM photo.editor_effect_render_worker_receipt(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))

    def render_cache_context(self, lease: Lease, params: bytes):
        command=lease.command
        return self._query(
            'SELECT * FROM photo.v26_render_worker_cache_context(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,params.decode('utf-8'),lease.input_digest))

    def lookup_render_cache(self, lease: Lease, params: bytes, cache_key: str, cache_body: str):
        command=lease.command
        return self._query(
            'SELECT * FROM photo.lookup_v26_render_worker_cache(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,params.decode('utf-8'),lease.input_digest,cache_key,cache_body))

    def render_cache_artifacts(self, lease: Lease, params: bytes, cache_key: str, cache_body: str):
        command=lease.command
        with self._connect() as conn:
            rows=conn.execute(
                'SELECT * FROM photo.v26_render_worker_cache_artifacts(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (command.tenant_id,command.run_id,command.task_id,command.attempt,
                 lease.fence,lease.worker_id,command.params.artifact_id,
                 command.params.sha256,params.decode('utf-8'),lease.input_digest,cache_key,cache_body)).fetchall()
        if len(rows)>30001:raise ValueError('Render cache manifest limit exceeded')
        return rows

    def quarantine_render_cache(self, lease: Lease, params: bytes, cache_key: str, cache_body: str, result_id: UUID):
        command=lease.command
        row=self._query(
            'SELECT photo.quarantine_v26_render_worker_cache(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,command.params.sha256,
             params.decode('utf-8'),lease.input_digest,cache_key,cache_body,result_id))
        return row is not None and row[0] is True

    def local_exposure_protection_origin(self, lease: Lease, params: bytes, root, binary) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_local_exposure_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id, command.params.sha256,
             params.decode('utf-8'), lease.input_digest, root.artifact_id, root.sha256,
             binary.artifact_id, binary.sha256))
        return row is not None and row[0] is True

    def exposure_preview_protection_origin(self, lease: Lease, params: bytes, root, binary) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_exposure_preview_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id, command.params.sha256,
             params.decode('utf-8'), lease.input_digest, root.artifact_id, root.sha256,
             binary.artifact_id, binary.sha256))
        return row is not None and row[0] is True

    def exposure_protection_pin(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT * FROM photo.v27_exposure_worker_protection_pin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return None if row is None else dict(artifact_id=str(row[0]), sha256=row[1])

    def exposure_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_exposure_worker_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_local_exposure(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_local_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def exposure_preview_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_exposure_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def shown_exposure_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_shown_exposure_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_exposure_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_exposure_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_semantic_shown_exposure(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_shown_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def shown_removal_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_shown_removal_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_shown_removal(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_shown_removal(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def shown_background_cleanup_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_shown_background_cleanup_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_shown_background_cleanup(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_shown_background_cleanup(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def shown_retouch_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_shown_retouch_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_shown_retouch(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_shown_retouch(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def removal_preview_protection_origin(self, lease: Lease, params: bytes, root, binary) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_removal_preview_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id, command.params.sha256,
             params.decode('utf-8'), lease.input_digest, root.artifact_id, root.sha256,
             binary.artifact_id, binary.sha256))
        return row is not None and row[0] is True

    def removal_preview_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_removal_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_removal_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_removal_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def background_cleanup_preview_protection_origin(self, lease: Lease, params: bytes, root, binary) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_background_cleanup_preview_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id, command.params.sha256,
             params.decode('utf-8'), lease.input_digest, root.artifact_id, root.sha256,
             binary.artifact_id, binary.sha256))
        return row is not None and row[0] is True

    def retouch_preview_protection_origin(self, lease: Lease, params: bytes, root, binary) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_retouch_preview_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id, command.params.sha256,
             params.decode('utf-8'), lease.input_digest, root.artifact_id, root.sha256,
             binary.artifact_id, binary.sha256))
        return row is not None and row[0] is True


    def retouch_preview_landmark_origin(self, lease: Lease, params: bytes, landmark) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.semantic_retouch_preview_worker_landmark_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             landmark.artifact_id, landmark.sha256))
        return row is not None and row[0] is True

    def retouch_preview_report_evidence(self, lease: Lease, params: bytes, context_ref, context: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_retouch_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest,
             context_ref.artifact_id, context_ref.sha256, context.decode('utf-8')))
        return row[0] if row is not None else None

    def admit_semantic_retouch_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_retouch_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def background_cleanup_preview_report_evidence(self, lease: Lease, params: bytes):
        command = lease.command
        row = self._query(
            'SELECT photo.v27_background_cleanup_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row[0] if row is not None else None

    def admit_semantic_background_cleanup_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_editor_layers_precompose(self, lease: Lease, params: bytes) -> bool:
        command=lease.command
        row=self._query('SELECT photo.editor_layers_precompose_worker_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,lease.fence,lease.worker_id,
             command.params.artifact_id,command.params.sha256,params.decode('utf-8'),lease.input_digest))
        return row is not None and row[0] is True

    def admit_editor_effects_seal(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.editor_effect_seal_worker_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_region_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v26_region_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def claim(self, command: TaskCommand, worker_id: UUID) -> Claim:
        row = self._query('SELECT * FROM photo.claim_task(%s,%s,%s,%s)',
                               (command.task_id, command.attempt, worker_id,
                                Jsonb(command.model_dump(mode='json'))))
        outcome, fence, until, deadline, context = row
        if outcome != 'claimed':
            return Claim(outcome, None)
        if context['command'] != command.model_dump(mode='json'):
            raise ValueError('Claimed context does not match command')
        return Claim(outcome, Lease(command, worker_id, fence, until, deadline,
                                    context['execution_profile'], context['input_digest']))

    def heartbeat(self, lease: Lease, progress: dict) -> Heartbeat:
        row = self._query('SELECT * FROM photo.heartbeat_task(%s,%s,%s,%s,%s)',
                               (lease.command.task_id, lease.command.attempt, lease.fence,
                                lease.worker_id, Jsonb(progress)))
        return Heartbeat(*row)

    def commit_success(self, lease: Lease, manifest: bytes) -> tuple[str, UUID | None, str | None]:
        return self._query('SELECT * FROM photo.commit_task_success(%s,%s,%s,%s,%s)',
                                (lease.command.task_id, lease.command.attempt, lease.fence,
                                 lease.worker_id, manifest.decode('utf-8')))

    def commit_failure(self, lease: Lease, error: TaskError, metrics: TaskMetrics) -> str:
        return self._query('SELECT photo.commit_task_failure(%s,%s,%s,%s,%s,%s)',
                                (lease.command.task_id, lease.command.attempt, lease.fence,
                                 lease.worker_id, Jsonb(error.model_dump(mode='json')),
                                 Jsonb(metrics.model_dump(mode='json'))))[0]
