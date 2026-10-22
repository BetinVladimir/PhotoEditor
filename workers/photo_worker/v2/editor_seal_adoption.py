"""Independent canonical seal reconstruction before publishing its receipt."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from uuid import UUID, uuid4
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.editor_effects import EditorEffectsSealParams,EditorExecutedBaseReceipt
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.editor.effects_seal import prepare_effects_seal
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.scratch import ScratchSpace
from .adoption import AdoptionInvalid,ArtifactRef
from .lens_bypass import _document,_ref
from .render_replay_reader import RenderReplayReader
from .render_replay_artifacts import RenderReplayArtifacts


@dataclass(frozen=True)
class EditorSealAdoptionInputs:
    params: ArtifactRef
    profile_jcs: bytes
    build_run_id: UUID


def load_editor_seal_adoption_inputs(connection,tenant,task,context):
    from photo_worker.contracts.base import parse_json
    from .render_adoption_inputs import _reference
    row=connection.execute('SELECT * FROM photo.editor_effect_seal_adoption_inputs(%s,%s,%s,%s,%s)',
        (tenant,task,context.attempt,context.fence,context.result_id)).fetchone()
    if not isinstance(row,(tuple,list)) or len(row)!=4 or not isinstance(row[2],dict):
        raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_SCOPE_STALE')
    try:
        build=row[3] if isinstance(row[3],UUID) else UUID(row[3]);profile=canonical_bytes(row[2])
        if build.int==0 or len(profile)>1048576:raise ValueError('scope')
        parse_json(profile)
        return EditorSealAdoptionInputs(_reference(row[0],row[1]),profile,build)
    except (TypeError,ValueError) as error:raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_SCOPE_INVALID') from error


@contextmanager
def _output_array(reader,ref,shape,role,scratch,active):
    active();evidence=reader.evidence(_ref(ref));limit=4*shape[0]*shape[1]+8192
    if (evidence.ref!=_ref(ref) or evidence.source!='output' or evidence.role!=role or evidence.media_type!='application/octet-stream'
            or evidence.bytes<1 or evidence.bytes>limit):raise AdoptionInvalid('EDITOR_SEAL_ARRAY_EVIDENCE')
    with scratch.reserve(evidence.bytes) as owner:
        path=owner.path('editor-seal-output','.npy');digest=sha256();size=0
        iterator=iter(reader.iter_bytes(_ref(ref),evidence.bytes))
        try:
            with scratch.open_writer(path,owner) as target:
                for chunk in iterator:
                    active()
                    if not isinstance(chunk,bytes) or not chunk or len(chunk)>65536 or size+len(chunk)>evidence.bytes:
                        raise AdoptionInvalid('EDITOR_SEAL_ARRAY_BYTES')
                    target.write(chunk);digest.update(chunk);size+=len(chunk);owner.validate()
            if size!=evidence.bytes or digest.hexdigest()!=ref.sha256:raise AdoptionInvalid('EDITOR_SEAL_ARRAY_HASH')
            with read_array(path,shape,'float32',active,unit_interval=True) as array:yield array
        finally:
            close=getattr(iterator,'close',None)
            if close is not None:close()


def verify_editor_effects_seal(reader,root,scoped,*,scratch_root,max_bytes,check_active):
    from photo_worker.contracts.base import parse_json
    if (not isinstance(scoped,EditorSealAdoptionInputs) or not isinstance(scratch_root,Path)
            or not scratch_root.is_absolute() or type(max_bytes) is not int or max_bytes<1):
        raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_CONTEXT_REQUIRED')
    params=_document(reader,scoped.params,EditorEffectsSealParams,'input',check_active=check_active)
    if params.build_run_id!=scoped.build_run_id:raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_BUILD')
    profile=parse_json(scoped.profile_jcs);estimate=profile.get('resource_estimate',{})
    budget=estimate.get('scratch_bytes')
    if type(budget) is not int or not 1<=budget<=max_bytes:raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_BUDGET')
    receipt=_document(reader,root,EditorExecutedBaseReceipt,'output',check_active=check_active)
    result=_document(reader,_ref(receipt.image_result),ImageResult,'output',check_active=check_active)
    report=_document(reader,_ref(result.report),ImageReport,'output',check_active=check_active)
    expected=EditorExecutedBaseReceipt(schema_version=1,build_run_id=params.build_run_id,original_recipe=params.original_recipe,
        original_source=params.original_source,processed_source=params.processed_source,executed_style=params.executed_style,
        compilation_sha=params.compilation_sha,trace_sha=params.trace_sha,quality=params.quality,image_result=receipt.image_result,
        generative_requested=params.generative_requested,inherited_review=params.inherited_review)
    if receipt!=expected or result.working_image!=params.processed_source.source:
        raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_RECEIPT')
    if params.generative_requested!=(result.generated_mask is not None):raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_GENERATED')
    try:
        with ScratchSpace(scratch_root,uuid4(),uuid4(),1,budget) as scratch:
            artifacts=RenderReplayArtifacts(RenderReplayReader(reader,check_active),scratch,check_active)
            with prepare_effects_seal(artifacts,params,profile,check_active) as prepared:
                if report!=prepared.report:raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_REPORT')
                shape=(prepared.image.canvas.height,prepared.image.canvas.width)
                changed=_document(reader,_ref(result.changed_mask),MaskRef,'output',check_active=check_active)
                if (changed.canvas!=prepared.image.canvas or changed.transform_chain!=prepared.image.transform_chain
                        or changed.selector!='manual' or changed.protected or changed.object_id is not None
                        or changed.confidence!=1 or changed.model_lock_id is not None):raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_CHANGED')
                with _output_array(reader,changed.artifact,shape,'changed_pixels',scratch,check_active) as physical:
                    for y in range(0,shape[0],256):
                        check_active()
                        if np.any(physical[y:y+256]!=0):raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_NOT_IDENTITY')
                if result.generated_mask is not None:
                    generated=_document(reader,_ref(result.generated_mask),MaskRef,'output',check_active=check_active)
                    if (generated.canvas!=prepared.image.canvas or generated.transform_chain!=prepared.image.transform_chain
                            or generated.selector!='generated' or generated.protected or generated.confidence!=1
                            or generated.model_lock_id is not None or generated.object_id is not None):raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_GENERATED')
                    if prepared.generated_path is not None:
                        with read_array(prepared.generated_path,shape,'float32',check_active,unit_interval=True) as expected_mask:
                            with _output_array(reader,generated.artifact,shape,'generated_pixels',scratch,check_active) as physical:
                                for y in range(0,shape[0],256):
                                    check_active()
                                    if not np.array_equal(physical[y:y+256],expected_mask[y:y+256]):raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_GENERATED')
                    elif generated!=prepared.generated:raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_GENERATED')
    except ValueError as error:
        if isinstance(error,AdoptionInvalid):raise
        raise AdoptionInvalid('EDITOR_SEAL_ADOPTION_PHYSICAL') from error
    check_active()
