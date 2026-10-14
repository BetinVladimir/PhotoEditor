"""Attempt-scoped donor glare admission and immutable input reader."""

from pathlib import Path

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.glare_donor_object_reader import GlareWorkerObjectReader


class GlareDonorAdmission:
    """Never infer donor consent or input locations from task documents alone."""

    def __init__(self, database, objects, bucket: str, *, runtime_sha: str,
                 model_lock_sha: str, material_scratch_root: Path,
                 material_max_bytes: int, alpha_mode: str):
        if (not callable(getattr(database, 'glare_donor_source_ready', None))
                or not callable(getattr(database, 'glare_donor_input_evidence', None))
                or not callable(getattr(objects, 'get_object', None))
                or not isinstance(bucket, str) or not bucket
                or not isinstance(material_scratch_root, Path)
                or not material_scratch_root.is_dir()
                or material_scratch_root.is_symlink()
                or type(material_max_bytes) is not int or not 1 <= material_max_bytes <= 2**53-1
                or alpha_mode not in ('require_float32', 'normalize_uint16')
                or any(not isinstance(value, str) or len(value) != 64
                       or any(char not in '0123456789abcdef' for char in value)
                       for value in (runtime_sha, model_lock_sha))):
            raise ValueError('GLARE_DONOR_INSTALLATION_INVALID')
        self.database = database
        self.objects = objects
        self.bucket = bucket
        self.runtime_sha = runtime_sha
        self.model_lock_sha = model_lock_sha
        self.material_scratch_root = material_scratch_root
        self.material_max_bytes = material_max_bytes
        self.alpha_mode = alpha_mode

    def _current(self, ctx, params, proof: bytes) -> bytes:
        ctx.check_active()
        command = ctx.lease.command
        profile = ctx.lease.execution_profile
        body = canonical_bytes(params)
        if (command.operation != 'portrait.glare'
                or command.operation_version != '1'
                or command.resource_class != 'gpu'
                or profile.get('backend') != 'cpu'
                or profile.get('renderer_version') != 'v2.2'
                or profile.get('restoration_runtime_sha') != self.runtime_sha
                or params.mode != 'donor'
                or params.model_lock_sha != self.model_lock_sha
                or canonical_hash(params) != command.params.sha256
                or len(body) > 65536
                or not isinstance(proof, bytes) or not 2 <= len(proof) <= 65536
                or self.database.glare_donor_source_ready(ctx.lease, body, proof) is not True):
            raise OperationFailure('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
        ctx.check_active()
        return body

    def admit(self, ctx, params, proof, source, donor, calibration) -> bool:
        if calibration.model_lock_sha != self.model_lock_sha:
            return False
        self._current(ctx, params, proof)
        return True

    def reader(self, ctx, params, proof):
        body = self._current(ctx, params, proof)
        return GlareWorkerObjectReader(self.database, ctx.lease, body, proof,
            self.objects, bucket=self.bucket, check_active=ctx.check_active)
