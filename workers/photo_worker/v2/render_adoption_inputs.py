"""Read immutable task-generation-scoped replay inputs from the primary DB."""
from dataclasses import dataclass
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid, ArtifactRef
from .render_qualification_pin import RenderQualificationPin


@dataclass(frozen=True)
class RenderAdoptionInputs:
    recipe: ArtifactRef
    execution_profile_jcs: bytes
    parent_roots: tuple[ArtifactRef, ...]
    qualification_pin: RenderQualificationPin | None = None
    cache_identity: str | None = None
    executed_base_receipt: ArtifactRef | None = None

    def execution_profile(self):
        # Return an independent object: caller changes cannot alter the snapshot.
        return parse_json(self.execution_profile_jcs)


def _reference(identifier, digest):
    try:
        value=identifier if isinstance(identifier,UUID) else UUID(identifier)
    except (ValueError,TypeError,AttributeError) as error:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID') from error
    if (value.int==0 or not isinstance(digest,str) or len(digest)!=64
            or any(c not in '0123456789abcdef' for c in digest)):
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    return ArtifactRef(value,digest)


def load_render_adoption_inputs(connection, tenant_id, task_id, context):
    """No object IO or writes; later commit repeats live authority independently.

    All parents are declared ImageResult roots in authoritative ordinal order.
    This establishes declaration scope, not provider completeness/qualification.
    Source binding must be decoded from hash-verified recipe bytes afterwards.
    """
    if context.operation!='render.compose' or context.version!='1':
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    row=connection.execute('SELECT * FROM photo.v26_render_adoption_inputs(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
    if row is None:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
    from dataclasses import replace
    scoped = decode_render_adoption_inputs(row)
    receipt = connection.execute('SELECT * FROM photo.editor_effect_render_adoption_receipt(%s,%s,%s,%s,%s)',
        (tenant_id, task_id, context.attempt, context.fence, context.result_id)).fetchone()
    return replace(scoped, executed_base_receipt=decode_executed_base_reference(receipt))


def decode_render_adoption_inputs(row):
    if not isinstance(row,(tuple,list)) or len(row)!=4:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    recipe=_reference(row[0],row[1]);profile=row[2];parents=row[3]
    if (not isinstance(profile,dict) or not isinstance(parents,list) or len(parents)>33):
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    try:
        profile_jcs=canonical_bytes(profile)
    except (TypeError,ValueError,OverflowError) as error:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID') from error
    if len(profile_jcs)>1048576:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    refs=[];seen=set()
    for parent in parents:
        if not isinstance(parent,dict) or set(parent)!={'artifact_id','sha256'}:
            raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
        ref=_reference(parent['artifact_id'],parent['sha256'])
        if ref.artifact_id in seen:
            raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
        seen.add(ref.artifact_id);refs.append(ref)
    return RenderAdoptionInputs(recipe,profile_jcs,tuple(refs))


def resolve_render_source_parent(artifacts, scoped, source_ref, *, check_active=lambda: None):
    """Read only primary-declared ImageResult roots; never take provider claims."""
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    from photo_worker.operations.optical.contracts import ImageResult
    if not isinstance(scoped,RenderAdoptionInputs):
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_INVALID')
    matched=None
    for reference in scoped.parent_roots:
        check_active()
        parent=artifacts.contract(ContractRef(artifact_id=reference.artifact_id,sha256=reference.sha256),ImageResult)
        check_active()
        if parent.working_image==source_ref:
            if matched is not None:
                raise AdoptionInvalid('RENDER_REPLAY_SOURCE_PARENT_AMBIGUOUS')
            matched=parent
    return matched


def decode_executed_base_reference(row):
    if row is None:
        return None
    if not isinstance(row, (tuple, list)) or len(row) != 2:
        raise AdoptionInvalid('RENDER_EXECUTED_BASE_AUTHORITY_INVALID')
    return _reference(row[0], row[1])
