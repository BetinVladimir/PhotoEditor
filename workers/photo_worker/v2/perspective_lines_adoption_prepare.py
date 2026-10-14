"""Prepare owned detector replay evidence outside the final write transaction."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.perspective_line_detection import PerspectiveLineDetectionParams
from photo_worker.operations.optical.perspective_lines_installation import PerspectiveLinesInstallation, execution_policy
from .adoption import AdoptionInvalid, require_adoption_active
from .perspective_lines_adoption_evidence import load_perspective_lines_adoption_evidence, perspective_lines_adoption_checkpoint
from .perspective_lines_replay import verify_pinned_perspective_lines_replay


@dataclass(frozen=True)
class PreparedPerspectiveLinesAdoption:
    evidence_jcs: bytes
    params_bytes: bytes
    result_bytes: bytes
    lines_bytes: bytes | None
    lines_sha256: str | None


def prepare_perspective_lines_adoption(connection,reader,tenant_id,task_id,context,params_ref,*,
        installation,scratch_root,max_bytes,ram_max_bytes,check_active):
    if not isinstance(installation,PerspectiveLinesInstallation) or not callable(check_active):
        raise ValueError('PERSPECTIVE_LINES_ADOPTION_INSTALLATION_INVALID')
    supplied_check_active=check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    owned=deepcopy(installation)
    ctx=deepcopy(context)
    ref=deepcopy(params_ref)
    check_active()
    raw=reader.read_json(ref,65536)
    params=PerspectiveLineDetectionParams.model_validate_json(raw)
    evidence=load_perspective_lines_adoption_evidence(connection,tenant_id,task_id,ctx,raw,check_active=check_active)
    if (evidence['params_ref']!={'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}
            or evidence['policy_jcs']!=canonical_bytes(execution_policy(owned.working_icc_sha,owned.runtime_sha)).decode()):
        raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_INSTALLATION_MISMATCH')
    checkpoint=perspective_lines_adoption_checkpoint(connection,tenant_id,task_id,ctx,raw,evidence,check_active=check_active)
    checkpoint()
    verified=verify_pinned_perspective_lines_replay(reader,ref,ctx.root,expected_base=params.base,
        working_icc_sha=owned.working_icc_sha,runtime_lock=owned.runtime_lock,runtime_sha=owned.runtime_sha,
        scratch_root=scratch_root,max_bytes=max_bytes,ram_max_bytes=ram_max_bytes,check_active=check_active)
    checkpoint()
    check_active()
    return PreparedPerspectiveLinesAdoption(canonical_bytes(evidence),bytes(raw),verified.result_bytes,
        verified.lines_bytes,verified.lines_sha256)


@dataclass(frozen=True)
class PerspectiveLinesAdoptionInstallation:
    runtime: PerspectiveLinesInstallation
    scratch_root: Path
    max_bytes: int
    ram_max_bytes: int


def validate_perspective_lines_adoption_installation(installation):
    if (not isinstance(installation,PerspectiveLinesAdoptionInstallation)
            or not isinstance(installation.runtime,PerspectiveLinesInstallation)
            or not isinstance(installation.scratch_root,Path) or installation.scratch_root.is_symlink()
            or not installation.scratch_root.is_dir() or not installation.scratch_root.is_absolute()
            or any(type(value) is not int or not 1<=value<=2**53-1
                for value in (installation.max_bytes,installation.ram_max_bytes))):
        raise ValueError('PERSPECTIVE_LINES_ADOPTION_INSTALLATION_INVALID')
    owned=deepcopy(installation)
    execution_policy(owned.runtime.working_icc_sha,owned.runtime.runtime_sha)
    from photo_worker.operations.optical.perspective_lines_runtime_lock import verify_installed_runtime
    verify_installed_runtime(owned.runtime.runtime_lock,owned.runtime.runtime_sha)
    return owned
