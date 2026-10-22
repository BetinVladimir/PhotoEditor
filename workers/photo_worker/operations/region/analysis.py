"""Exact declared native analysis selection for full Region providers.

Catalog/adoption authority must install these typed inputs; this loader never
discovers arbitrary artifacts or treats geometry alone as provenance proof.
"""
from dataclasses import dataclass
from photo_worker.contracts.face import FaceBoxes, FaceLandmarks
from photo_worker.contracts.images import MaskRef, RelativeDepth
from photo_worker.contracts.base import canonical_hash


@dataclass(frozen=True)
class RegionNativeAnalysis:
    detection: object
    landmarks: object
    faces: tuple
    skin: tuple
    subject: object = None
    depth: object = None


def load_region_native_analysis(artifacts, source_ref, source, qualification, *,
                               require_faces=False, require_subject=False,
                               require_depth=False, check_active=lambda:None):
    """Select exactly one admitted collection; refuse ambiguity/stale source.

    Binary loading remains in the owned provider context. All masks retain
    source canvas, transform chain, model lock and confidence fences.
    """
    check_active()
    canvas=canonical_hash(source.canvas)
    def same_geometry(value):
        return canonical_hash(value.canvas)==canvas and value.transform_chain==source.transform_chain
    boxes=[];landmarks=[];masks=[];depths=[]
    if require_faces:
        for ref in artifacts.declared_contract_refs('FaceBoxes',maximum=64):
            check_active();doc=artifacts.contract(ref,FaceBoxes)
            if doc.image==source_ref and doc.model_lock_id==qualification.model_lock_sha and doc.backend==qualification.backend:
                boxes.append((ref,doc))
        for ref in artifacts.declared_contract_refs('FaceLandmarks',maximum=64):
            check_active();doc=artifacts.contract(ref,FaceLandmarks)
            if doc.image==source_ref and doc.model_lock_id==qualification.model_lock_sha and doc.backend==qualification.backend:
                landmarks.append((ref,doc))
        if len(boxes)!=1 or len(landmarks)!=1 or landmarks[0][1].faces!=boxes[0][0]:
            raise ValueError('REGION_FACE_ANALYSIS_UNAVAILABLE')
        detection=boxes[0][1];collection=landmarks[0][1]
        ids={face.object_id for face in detection.faces}
        actual={face.object_id for face in collection.landmarks}
        if (len(detection.faces)>64 or len(ids)!=len(detection.faces)
                or len(actual)!=len(collection.landmarks) or actual!=ids
                or detection.accepted_count!=len(ids)
                or any(not same_geometry(face) for face in collection.landmarks)
                or 'faces_truncated' in detection.warnings
                or collection.landmarks and collection.mean_confidence<.75):
            raise ValueError('REGION_FACE_ANALYSIS_UNAVAILABLE')
    if require_faces or require_subject:
        for ref in artifacts.declared_contract_refs('MaskRef',maximum=128):
            check_active();mask=artifacts.contract(ref,MaskRef)
            if (same_geometry(mask) and mask.model_lock_id==qualification.model_lock_sha
                    and not mask.protected and mask.confidence>=.75):
                masks.append((ref,mask))
    skin=[]
    if require_faces:
        for face in collection.landmarks:
            selected=[pair for pair in masks if pair[1].selector=='skin' and pair[1].object_id==face.object_id]
            if len(selected)!=1:raise ValueError('REGION_FACE_SKIN_UNAVAILABLE')
            skin.append(selected[0])
    subject=None
    if require_subject:
        selected=[pair for pair in masks if pair[1].selector in ('subject','person') and pair[1].object_id is None]
        if len(selected)!=1:raise ValueError('REGION_SUBJECT_ANALYSIS_UNAVAILABLE')
        subject=selected[0]
    depth=None
    if require_depth:
        for ref in artifacts.declared_contract_refs('RelativeDepth',maximum=64):
            check_active();doc=artifacts.contract(ref,RelativeDepth)
            if same_geometry(doc) and doc.invalid_mask is None:depths.append((ref,doc))
        if len(depths)!=1:raise ValueError('REGION_DEPTH_ANALYSIS_UNAVAILABLE')
        depth=depths[0]
    check_active()
    return RegionNativeAnalysis(None if not require_faces else boxes[0],
        None if not require_faces else landmarks[0],
        () if not require_faces else tuple(collection.landmarks),tuple(skin),subject,depth)
