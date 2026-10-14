"""Re-execute qualified relight from pinned inputs before result adoption.

Caller must provide current tenant/source/producer/runtime qualification authority
and repeat it in the adoption transaction. This module does not commit results.
"""
from dataclasses import dataclass
from uuid import uuid4
from pathlib import Path
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,RelativeDepth
from photo_worker.operations.session.contracts import Calibration
from photo_worker.operations.portrait.prepare_relight import prepare_relight
from photo_worker.operations.portrait.relight_adapter import _invalid_plane
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.scratch import ScratchSpace
from .adoption import AdoptionInvalid
from .array_evidence import ArrayEvidenceSession
from .relight_adoption import verify_relight_result_metadata,verify_relight_pixel_tiles,_document,_ref


@dataclass(frozen=True)
class RelightReplayContext:
    scratch_root: Path
    max_bytes: int
    max_scratch_bytes: int
    execution_profile: dict
    verify_authority: object


def verify_relight_replay(reader,params_ref,root_ref,*,scratch_root,max_bytes,max_scratch_bytes,
                          execution_profile,verify_authority,check_active):
    if not callable(verify_authority) or not callable(check_active):raise ValueError('RELIGHT_REPLAY_AUTHORITY_REQUIRED')
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
        raise ValueError('RELIGHT_REPLAY_SCRATCH_INVALID')
    if type(max_scratch_bytes) is not int or not 1<=max_scratch_bytes<=2**53-1:
        raise ValueError('RELIGHT_REPLAY_SCRATCH_INVALID')
    check_active()
    params,result=verify_relight_result_metadata(reader,params_ref,root_ref,check_active=check_active)
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    output=_document(reader,_ref(result.working_image),WorkingImage,'output')
    subject=_document(reader,_ref(params.subject),MaskRef,'input')
    guard=_document(reader,_ref(params.protected),MaskRef,'input')
    depth=_document(reader,_ref(params.depth),RelativeDepth,'input')
    changed=_document(reader,_ref(result.changed_mask),MaskRef,'output')
    calibration=_document(reader,_ref(params.calibration),Calibration,'input')
    if (not calibration.qualified or subject.protected or not guard.protected
            or any(doc.canvas!=source.canvas or doc.transform_chain!=source.transform_chain for doc in (subject,guard,depth))):
        raise AdoptionInvalid('RELIGHT_REPLAY_INPUT_INVALID')
    def identity():
        return canonical_hash([doc.model_dump(mode='json') for doc in (params,source,depth,subject,guard,calibration)]+[execution_profile])
    frozen=identity()
    def authority():
        check_active()
        if verify_authority(params,source,depth,subject,guard,calibration) is not True:
            raise AdoptionInvalid('RELIGHT_REPLAY_AUTHORITY_REVOKED')
        check_active()
        if identity()!=frozen:raise AdoptionInvalid('RELIGHT_REPLAY_AUTHORITY_CHANGED')
    authority()
    shape=(source.canvas.height,source.canvas.width)
    if shape[0]*shape[1]>200_000_000:raise AdoptionInvalid('RELIGHT_REPLAY_INPUT_INVALID')
    with ArrayEvidenceSession(reader,scratch_root,min(max_bytes,max_scratch_bytes),check_active) as arrays:
        before=arrays.array(_ref(source.pixels),(*shape,3),source='input',legacy_input_npy=True)
        after=arrays.array(_ref(output.pixels),(*shape,3),source='output',role='pixels')
        mask=arrays.array(_ref(subject.artifact),shape,source='input',unit=True,legacy_input_npy=True)
        protected=arrays.array(_ref(guard.artifact),shape,source='input',unit=True,legacy_input_npy=True)
        changes=arrays.array(_ref(changed.artifact),shape,source='output',role='mask_pixels',unit=True)
        values=arrays.array(_ref(depth.artifact),shape,source='input',unit=True,legacy_input_npy=True)
        invalid_values=None if depth.invalid_mask is None else arrays.array(_ref(depth.invalid_mask),shape,source='input',unit=True,legacy_input_npy=True)
        verify_relight_pixel_tiles(before,after,mask,protected,changes,invalid_values,strength=params.strength,check_active=check_active)
        remaining=max_scratch_bytes-arrays.used
        if remaining<1:raise AdoptionInvalid('RELIGHT_REPLAY_SCRATCH_EXCEEDED')
        with ScratchSpace(Path(scratch_root),uuid4(),uuid4(),1,remaining,min_free_bytes=0) as scratch:
            with _invalid_plane(invalid_values,shape,scratch,check_active) as invalid:
                with prepare_relight(params,before,values,invalid,mask,protected,source.color,scratch,execution_profile,
                        depth_sigma=calibration.depth_sigma,check_active=check_active) as files:
                    with read_array(files.rgb,(*shape,3),'float32',check_active) as expected:
                        with read_array(files.changed_mask,shape,'float32',check_active,unit_interval=True) as expected_changed:
                            for y in range(0,shape[0],256):
                                for x in range(0,shape[1],512):
                                    check_active();part=np.s_[y:y+256,x:x+512]
                                    if (not np.array_equal(expected[part].view(np.uint32),after[part].view(np.uint32))
                                            or not np.array_equal(expected_changed[part],changes[part])):
                                        raise AdoptionInvalid('RELIGHT_REPLAY_PIXEL_MISMATCH')
            authority()
    check_active()
    return params,result
