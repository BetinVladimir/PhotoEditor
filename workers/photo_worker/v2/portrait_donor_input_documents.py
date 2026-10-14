"""Discover and read original donor documents from a verified V2 input index."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.manual_mask import RasterizedMask,MaskRasterParams,ManualMask
from photo_worker.operations.session.contracts import FaceObservations


@dataclass(frozen=True)
class DonorInputDocuments:
    params: bytes
    source: bytes
    donor: bytes
    confirmation: bytes
    source_faces: bytes
    donor_faces: bytes
    target: bytes
    protected: bytes
    root_ref: ArtifactRef
    root: bytes
    producer_ref: ArtifactRef
    producer: bytes
    vectors: bytes | None


def load_donor_input_documents(artifacts,params_ref,*,check_active):
    """Original bytes only; SQL lineage/consent gates remain mandatory.

    Typed discovery refuses ambiguous original roots/Params. Canonical and SHA
    checks repeat independently of the TaskArtifacts reader implementation.
    """
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    pin=ArtifactRef.model_validate_json(canonical_bytes(params_ref))
    def read(ref,model,maximum=1<<20):
        check_active();artifacts.require_declared_input(ref)
        raw=artifacts.contract_bytes(ref,model)
        if not isinstance(raw,bytes) or not 2<=len(raw)<=maximum or sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('DONOR_DOCUMENT_INVALID')
        value=model.model_validate_json(raw)
        if canonical_bytes(value)!=raw:raise ValueError('DONOR_DOCUMENT_INVALID')
        check_active();return raw,value
    body,params=read(pin,PortraitOpenEyesParams,65536)
    roots=artifacts.declared_contract_refs('RasterizedMask',1)
    candidates=artifacts.declared_contract_refs('Params',2)
    original=[ref for ref in candidates if ref!=pin]
    if len(roots)!=1 or len(original)!=1 or pin not in candidates:
        raise ValueError('DONOR_MANUAL_ORIGIN_INVALID')
    root_pin=ArtifactRef.model_validate_json(canonical_bytes(roots[0]))
    producer_pin=ArtifactRef.model_validate_json(canonical_bytes(original[0]))
    root_raw,root=read(root_pin,RasterizedMask)
    producer_raw,producer=read(producer_pin,MaskRasterParams)
    if (root.image!=params.base.source or producer.image!=params.base.source
            or root_pin.artifact_id==producer_pin.artifact_id
            or root.vectors!=(producer.vectors if producer.vectors is not None else producer_pin)):
        raise ValueError('DONOR_MANUAL_ORIGIN_INVALID')
    source,_=read(params.base.source,WorkingImage)
    donor,_=read(params.donor.source,WorkingImage)
    proof,_=read(params.confirmation,DonorConfirmation,65536)
    sfaces,_=read(params.source_faces,FaceObservations)
    dfaces,_=read(params.donor_faces,FaceObservations)
    target,target_value=read(params.eyes_mask,MaskRef)
    guard,guard_value=read(params.protected,MaskRef)
    if canonical_bytes(root.target)!=canonical_bytes(target_value) or canonical_bytes(root.protected)!=canonical_bytes(guard_value):
        raise ValueError('DONOR_MANUAL_ORIGIN_INVALID')
    vectors=None
    if producer.vectors is not None:vectors,_=read(producer.vectors,ManualMask)
    check_active()
    return DonorInputDocuments(body,source,donor,proof,sfaces,dfaces,target,guard,
        root_pin,root_raw,producer_pin,producer_raw,vectors)
