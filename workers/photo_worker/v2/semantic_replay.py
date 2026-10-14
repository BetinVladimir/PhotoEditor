"""Bounded source-mask warp and protection replay for semantic adoption."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
from .array_evidence import ArrayEvidenceSession
from .adoption import ArtifactRef,AdoptionInvalid



def semantic_mask_replay_resources(source_shape,target_shape):
    for shape in (source_shape,target_shape):
        if (not isinstance(shape,tuple) or len(shape)!=2 or any(type(n) is not int or n<8 for n in shape)
                or shape[0]*shape[1]>200000000):raise AdoptionInvalid('SEMANTIC_REPLAY_DIMENSIONS_INVALID')
    retained=8*source_shape[0]*source_shape[1]+12*target_shape[0]*target_shape[1]
    return dict(ram_bytes=retained+64*1024*1024,scratch_bytes=retained+5*8192)


def verify_semantic_mask_replay(reader,source_mask,target_mask,source_protection,target_protection,transform,*,
        scratch_root,max_bytes,max_ram_bytes,check_active=lambda:None):
    """Caller supplies authenticated pinned documents from the admitted intent.

    Exact replay uses the same qualified native warp; it does not independently
    prove scene planarity, model provenance or human approval. All input maps,
    published target and private replay output coexist within admitted scratch.
    """
    def owned(value,model):return model.model_validate_json(canonical_bytes(value))
    source_mask,target_mask,source_protection,target_protection=[owned(value,MaskRef) for value in
        (source_mask,target_mask,source_protection,target_protection)]
    transform=owned(transform,ResolvedTransform);check_active()
    if (source_mask.protected or target_mask.protected or not source_protection.protected or not target_protection.protected
            or source_protection.canvas!=source_mask.canvas or target_protection.canvas!=target_mask.canvas
            or source_protection.transform_chain!=source_mask.transform_chain
            or target_protection.transform_chain!=target_mask.transform_chain
            or transform.kind!='homography' or transform.from_canvas_hash!=canonical_hash(source_mask.canvas)
            or transform.to_canvas_hash!=canonical_hash(target_mask.canvas)):
        raise AdoptionInvalid('SEMANTIC_REPLAY_BINDING_INVALID')
    costs=semantic_mask_replay_resources((source_mask.canvas.height,source_mask.canvas.width),
        (target_mask.canvas.height,target_mask.canvas.width))
    if any(type(value) is not int or not 1<=value<=2**53-1 for value in (max_bytes,max_ram_bytes)):
        raise AdoptionInvalid('SEMANTIC_REPLAY_BUDGET_INVALID')
    if max_ram_bytes<costs['ram_bytes']:raise AdoptionInvalid('SEMANTIC_REPLAY_RAM_EXCEEDED')
    if max_bytes<costs['scratch_bytes']:raise AdoptionInvalid('ADOPTION_SCRATCH_EXCEEDED')
    def ref(value):return ArtifactRef(value.artifact_id,value.sha256)
    # Canonical input protection/manual mask producers publish NPY bytes using
    # application/octet-stream. Normalize only these authenticated input refs;
    # the shared array parser still validates exact SHA, NPY shape/dtype/range.
    from dataclasses import replace
    input_refs={ref(mask.artifact) for mask in (source_mask,source_protection,target_protection)}
    class MaskReader:
        def evidence(self,value):
            evidence=reader.evidence(value)
            if value in input_refs and evidence.source=='input' and evidence.media_type=='application/octet-stream':
                return replace(evidence,media_type='application/x-npy')
            return evidence
        def iter_bytes(self,value,max_bytes):return reader.iter_bytes(value,max_bytes)
    with ArrayEvidenceSession(MaskReader(),scratch_root,max_bytes,check_active) as arrays:
        def mapped(mask,source):
            return arrays.array(ref(mask.artifact),(mask.canvas.height,mask.canvas.width),source=source,
                role='mask_pixels' if source=='output' else None,unit=True)
        try:
            selected=mapped(source_mask,'input');source_guard=mapped(source_protection,'input')
            target_guard=mapped(target_protection,'input');published=mapped(target_mask,'output')
            expected=arrays.allocate_mask(published.shape)
            warp_planar_mask_into(selected,np.asarray(transform.matrix).reshape(3,3),expected,
                source_protected=source_guard,target_protected=target_guard,check_active=check_active)
        except ValueError as error:
            if not isinstance(error,AdoptionInvalid) and (str(error).startswith(('ARRAY_','SEMANTIC_'))
                    or str(error)=='UNSAFE_PLANAR_TRANSFORM'):
                raise AdoptionInvalid('SEMANTIC_REPLAY_INVALID') from error
            raise
        count=0
        for y in range(0,published.shape[0],256):
            for x in range(0,published.shape[1],256):
                check_active();part=np.s_[y:y+256,x:x+256]
                if not np.array_equal(published[part],expected[part]):raise AdoptionInvalid('SEMANTIC_REPLAY_CHANGED')
                count+=int(np.count_nonzero(expected[part]>0))
        if count==0:raise AdoptionInvalid('SEMANTIC_REPLAY_EMPTY')
        check_active();return count
