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
    def __init__(self, dsn: str):
        self._dsn = dsn

    def _connect(self):
        return psycopg.connect(self._dsn, connect_timeout=5,
                              options='-c role=photo_worker -c statement_timeout=5000 -c lock_timeout=3000')

    def _worker_function_set_ready(self, signatures) -> bool:
        for signature in signatures:
            row = self._query('''SELECT pg_get_userbyid(p.proowner),p.prosecdef,
                coalesce(array_to_string(p.proconfig,','),''),
                has_function_privilege('photo_worker',p.oid,'EXECUTE'),
                has_function_privilege('photo_api',p.oid,'EXECUTE'),
                has_function_privilege('photo_gateway',p.oid,'EXECUTE')
                FROM pg_proc p WHERE p.oid=to_regprocedure(%s)''', (signature,))
            if (row is None or len(row) != 6 or row[0] != 'photo_fn_owner'
                    or row[1] is not True or 'search_path=pg_catalog' not in row[2]
                    or 'photo' not in row[2]
                    or row[3] is not True or row[4] is not False or row[5] is not False):
                return False
        return True

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
            conn.execute('SELECT 1').fetchone()

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

    def semantic_report_evidence(self, lease: Lease):
        command=lease.command
        row=self._query(
            'SELECT photo.v27_semantic_worker_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id,command.run_id,command.task_id,command.attempt,
             lease.fence,lease.worker_id,command.params.artifact_id,
             command.params.sha256,lease.input_digest))
        return row[0] if row is not None else None

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
