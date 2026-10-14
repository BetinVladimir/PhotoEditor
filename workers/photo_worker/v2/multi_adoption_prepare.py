"""Qualified completed-task reconstruction outside the final SQL transaction."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.multi_installation import MultiImageRuntimeOptions
from photo_worker.operations.optical.multi_admission import PinnedMultiImageAdmission,MultiImageEvidence
from .multi_adoption_evidence import load_multi_adoption_evidence,multi_adoption_checkpoint
from .multi_replay import verify_admitted_multi_replay
from .adoption import AdoptionInvalid,ArtifactRef
from .lens_bypass import _document,_ref


@dataclass(frozen=True)
class MultiImageAdoptionInstallation:
    options:MultiImageRuntimeOptions
    scratch_root:Path
    max_bytes:int
    ram_max_bytes:int
    verify_color:object


def validate_multi_adoption_installation(item):
    if (not isinstance(item,MultiImageAdoptionInstallation) or not isinstance(item.options,MultiImageRuntimeOptions) or
        not callable(item.verify_color) or any(type(value) is not int or not 1<=value<=2**53-1 for value in (item.max_bytes,item.ram_max_bytes)) or
        not isinstance(item.scratch_root,Path) or item.scratch_root.is_symlink() or not item.scratch_root.is_dir()):
        raise ValueError('MULTI_ADOPTION_INSTALLATION_INVALID')
    from photo_worker.operations.optical.multi_runtime_lock import verify_installed_runtime
    verify_installed_runtime(item.options.runtime_lock,item.options.runtime_sha)
    PinnedMultiImageAdmission(operation=item.options.operation,runtime_lock=item.options.runtime_lock,
        runtime_sha=item.options.runtime_sha,pin=item.options.pin,policy=item.options.policy,
        max_pixels=item.options.max_pixels,resolve_evidence=lambda *args:None,verify_color=item.verify_color)
    return deepcopy(item)


@dataclass(frozen=True)
class PreparedMultiReconstruction:
    evidence_jcs:bytes
    params_raw:bytes
    params_ref:object
    result_ref:object


def prepare_multi_reconstruction(connection,reader,tenant_id,task_id,context,params_ref,*,installation,check_active):
    owned=validate_multi_adoption_installation(installation)
    ctx=deepcopy(context);ref=deepcopy(params_ref)
    model=OpticalPanoramaParams if owned.options.operation=='optical.panorama' else OpticalFocusStackParams
    check_active();params=_document(reader,ref,model,'input');raw=canonical_bytes(params)
    if sha256(raw).hexdigest()!=ref.sha256:raise AdoptionInvalid('MULTI_PARAMS_NONCANONICAL')
    evidence=load_multi_adoption_evidence(connection,tenant_id,task_id,ctx,raw,operation=owned.options.operation,check_active=check_active)
    if evidence['params_ref']!={'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}:
        raise AdoptionInvalid('MULTI_ADOPTION_PARAMS_CHANGED')
    checkpoint=multi_adoption_checkpoint(connection,tenant_id,task_id,ctx,raw,evidence,operation=owned.options.operation,check_active=check_active)
    sources=tuple(_document(reader,_ref(binding.source),WorkingImage,'input') for binding in params.frames)
    locations={item['contract']:item for item in evidence['qualification_objects']}
    def original(contract,maximum):
        location=locations[contract];artifact=ArtifactRef.parse({'artifact_id':location['artifact_id'],'sha256':location['sha256']})
        check_active();metadata=reader.evidence(artifact)
        if metadata.ref!=artifact or metadata.source!='input' or metadata.media_type!='application/json' or metadata.bytes!=location['bytes']:
            raise AdoptionInvalid('MULTI_QUALIFICATION_OBJECT_CHANGED')
        body=reader.read_json(artifact,maximum)
        if not isinstance(body,bytes) or len(body)!=location['bytes'] or sha256(body).hexdigest()!=artifact.sha256:
            raise AdoptionInvalid('MULTI_QUALIFICATION_BYTES_CHANGED')
        check_active();return body
    report=original('Report',8<<20);calibration=original('Calibration',1<<20)
    def resolve(*args):
        current=checkpoint()
        return MultiImageEvidence(current['profile']['multi_qualification'],owned.options.policy,
            report,calibration,owned.options.operation,raw)
    admission=PinnedMultiImageAdmission(operation=owned.options.operation,runtime_lock=owned.options.runtime_lock,runtime_sha=owned.options.runtime_sha,
        pin=owned.options.pin,policy=owned.options.policy,max_pixels=owned.options.max_pixels,resolve_evidence=resolve,verify_color=owned.verify_color)
    authority_context=SimpleNamespace(check_active=check_active,lease=SimpleNamespace(execution_profile=evidence['profile']))
    verify_admitted_multi_replay(reader,ref,ctx.root,ctx=authority_context,admission=admission,
        scratch_root=owned.scratch_root,max_bytes=owned.max_bytes,ram_max_bytes=owned.ram_max_bytes)
    checkpoint();check_active()
    return PreparedMultiReconstruction(canonical_bytes(evidence),raw,ref,deepcopy(ctx.root))
