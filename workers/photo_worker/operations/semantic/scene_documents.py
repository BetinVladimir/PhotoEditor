"""Strict embedded scene binding, shared by worker and independent adopter."""
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.semantic_scene import SemanticSceneEvidence
from photo_worker.contracts.semantic import SceneLabels,SceneParseParams
from photo_worker.contracts.depth import DepthParams
from photo_worker.contracts.images import RelativeDepth


def bind_scene_documents(raw,params,intent,images,load_document,*,policy_evidence_shas):
    if (not isinstance(images,(tuple,list)) or len(images)!=2 or not callable(load_document)
            or not isinstance(policy_evidence_shas,(list,tuple,set,frozenset))
            or any(not isinstance(v,str) or len(v)!=64 for v in policy_evidence_shas)):
        raise ValueError('SEMANTIC_SCENE_BINDING_INVALID')
    evidence=SemanticSceneEvidence.model_validate_json(canonical_bytes(raw))
    if (evidence.reference.binding!=params.reference or evidence.target.binding!=params.target
            or evidence.intent!=params.intent or evidence.calibration!=params.calibration
            or evidence.model_lock_sha!=params.model_lock_sha or intent.reference!=params.reference
            or evidence.policy_sha not in policy_evidence_shas):
        raise ValueError('SEMANTIC_SCENE_BINDING_INVALID')
    labels=[];depth=[]
    for source,image in zip((evidence.reference,evidence.target),images):
        if canonical_hash(image.canvas)!=source.binding.canvas_hash:
            raise ValueError('SEMANTIC_SCENE_CANVAS_INVALID')
        parsed=load_document(source.labels,SceneLabels)
        z=load_document(source.depth,RelativeDepth)
        lp=load_document(source.labels_params,SceneParseParams)
        dp=load_document(source.depth_params,DepthParams)
        if (parsed.labels!=source.labels_pixels or z.artifact!=source.depth_pixels or z.invalid_mask!=source.depth_invalid
                or lp.image!=source.binding.source or dp.image!=source.binding.source
                or lp.model_lock_id!=evidence.model_lock_sha or dp.model_lock_id!=evidence.model_lock_sha
                or parsed.image!=source.binding.source or parsed.canvas!=image.canvas
                or parsed.transform_chain!=image.transform_chain
                or parsed.model_lock_id!=evidence.model_lock_sha
                or parsed.backend!='cuda' or parsed.warnings
                or z.canvas!=image.canvas or z.transform_chain!=image.transform_chain):
            raise ValueError('SEMANTIC_SCENE_SOURCE_INVALID')
        # Depth has no embedded source identity. Caller MUST additionally prove
        # exact depth Params.image via canonical producer SQL, never infer it
        # from canvas equality. Scene/depth model pins are validated by SQL too.
        labels.append(parsed);depth.append(z)
    return evidence,tuple(labels),tuple(depth)


def require_scene_pin(evidence,pin):
    if any(getattr(evidence,key)!=getattr(pin,key) for key in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')):
        raise ValueError('SEMANTIC_SCENE_RUNTIME_CHANGED')
