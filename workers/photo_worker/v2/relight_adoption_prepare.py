"""Qualified relight reconstruction outside the final SQL transaction."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.contracts.images import MaskRef,RelativeDepth
from .adoption import AdoptionInvalid,prepare_v2_adoption
from .relight_adoption import _document,_ref
from .relight_adoption_authority import qualified_completed_relight_authority
from .relight_adoption_manual import resolve_relight_adoption_manual
from .relight_adoption_source import load_relight_adoption_source
from .relight_replay import RelightReplayContext


@dataclass(frozen=True)
class RelightAdoptionInstallation:
    runtime_lock: object
    runtime_sha: str
    scratch_root: Path
    max_bytes: int
    max_scratch_bytes: int


@dataclass(frozen=True)
class PreparedRelightReconstruction:
    prepared: object
    evidence_jcs: bytes
    bodies: tuple


def prepare_relight_reconstruction(connection,reader,tenant_id,task_id,context,params_ref,*,installation,check_active):
    ctx=deepcopy(context)
    params=_document(reader,params_ref,ImageRelightParams,'input')
    raw=canonical_bytes(params)
    source=load_relight_adoption_source(connection,tenant_id,task_id,ctx,raw,check_active=check_active)
    if source['params_ref']!={'artifact_id':str(params_ref.artifact_id),'sha256':params_ref.sha256}:
        raise AdoptionInvalid('RELIGHT_ADOPTION_PARAMS_CHANGED')
    authority=qualified_completed_relight_authority(connection,reader,tenant_id,task_id,ctx,
        scratch_root=installation.scratch_root,max_bytes=installation.max_bytes,
        runtime_lock=installation.runtime_lock,expected_runtime_sha=installation.runtime_sha,check_active=check_active)
    depth=_document(reader,_ref(params.depth),RelativeDepth,'input')
    subject=_document(reader,_ref(params.subject),MaskRef,'input')
    guard=_document(reader,_ref(params.protected),MaskRef,'input')
    def original_root(mask,protected):
        if mask.selector!='manual':return None
        _,root,_=resolve_relight_adoption_manual(connection,reader,tenant_id,task_id,ctx,params,mask,
            protected=protected,check_active=check_active)
        return canonical_bytes(root).decode()
    bodies=(raw.decode(),canonical_bytes(depth).decode(),canonical_bytes(subject).decode(),
            canonical_bytes(guard).decode(),original_root(subject,False),original_root(guard,True))
    def evidence():
        check_active()
        row=connection.execute('SELECT photo.v28_relight_adoption_evidence('+','.join(['%s']*11)+')',
            (tenant_id,task_id,ctx.attempt,ctx.fence,ctx.result_id,*bodies)).fetchone()
        check_active()
        if row is None or row[0] is None:raise AdoptionInvalid('RELIGHT_ADOPTION_EVIDENCE_STALE')
        return canonical_bytes(row[0])
    frozen=evidence()
    replay=RelightReplayContext(installation.scratch_root,installation.max_bytes,
        installation.max_scratch_bytes,source['profile'],authority)
    prepared=prepare_v2_adoption(task_id=task_id,operation='image.relight',version='1',root=ctx.root,
        reader=reader,params_ref=params_ref,relight_context=replay,check_active=check_active)
    if evidence()!=frozen:raise AdoptionInvalid('RELIGHT_ADOPTION_EVIDENCE_CHANGED')
    return PreparedRelightReconstruction(prepared,frozen,bodies)
